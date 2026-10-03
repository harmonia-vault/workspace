package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/mobileworkflow"
)

type acceptanceRecoveryEnvelopeEvidence struct {
	Profile            string `json:"profile"`
	EnvironmentChanges []struct {
		Sequence      uint64                           `json:"sequence"`
		Change        cryptox.SignedEnvironmentChange  `json:"change"`
		Origin        *cryptox.SignedEnvironmentOrigin `json:"origin"`
		Authorization cryptox.SignedGrantWire          `json:"authorization"`
	} `json:"environmentChanges"`
	RecoveryRotations []struct {
		Sequence  uint64                           `json:"sequence"`
		Proposal  cryptox.RecoveryRotationProposal `json:"proposal"`
		Proof     cryptox.RecoveryRotationProof    `json:"proof"`
		Signature string                           `json:"signature"`
	} `json:"recoveryRotations"`
}

func editRecoveryEnvelopeEvidence(f *mobileManagerFixture, edit func(map[string]json.RawMessage, *acceptanceRecoveryEnvelopeEvidence) error) {
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.StatusCode != 200 || r.Request.Method != "GET" || !strings.HasSuffix(r.Request.URL.Path, "/recovery-vault") {
			return nil
		}
		if r.Request.URL.Query().Get("envelopeEvidence") != "recovery-envelope-v1" {
			return errors.New("missing explicit envelope-evidence capability")
		}
		data, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		_ = r.Body.Close()
		var body map[string]json.RawMessage
		if err = json.Unmarshal(data, &body); err != nil {
			return err
		}
		var evidence acceptanceRecoveryEnvelopeEvidence
		if err = json.Unmarshal(body["envelopeEvidence"], &evidence); err != nil {
			return err
		}
		if err = edit(body, &evidence); err != nil {
			return err
		}
		if _, erased := body["envelopeEvidence"]; erased {
			body["envelopeEvidence"], err = json.Marshal(evidence)
			if err != nil {
				return err
			}
		}
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
		r.Body = io.NopCloser(bytes.NewReader(data))
		r.ContentLength = int64(len(data))
		r.Header.Set("Content-Length", strconv.Itoa(len(data)))
		return nil
	}})
}

func assertRecoveryEnvelopeEvidenceRejected(t *testing.T, f *mobileManagerFixture, code string) {
	t.Helper()
	a := newMobileManagerActor(t, f)
	ctx := context.Background()
	if err := a.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	if _, err := a.workflow.BeginRecoveryWithOrigins(ctx, code); !errors.Is(err, mobileworkflow.ErrRecoveryEvidence) {
		t.Fatal("uncommitted recovery envelope evidence accepted", err)
	}
	if info, err := a.workflow.RecoveryInfo(); err != nil || info.State != "none" || info.TrustedDevice {
		t.Fatal("partial recovery context became observable", info, err)
	}
	if _, err := a.workflow.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
		t.Fatal("partial plaintext cache became observable", err)
	}
}

func TestMobileRecoveryEnvelopeRealSignedChangeAndRotationEvidenceCannotBeOmittedOrRewritten(t *testing.T) {
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	view := environmentValue(f.root.CreateEnvironment(ctx, "空封套来源", "recover-envelope-origin-empty"))
	empty := ""
	for _, e := range view.Environments {
		if e.ID != f.initial {
			empty = e.ID
		}
	}
	if empty == "" {
		t.Fatal(view)
	}
	for _, mode := range []string{"missing-evidence", "missing-change", "signed-packet-bytes", "signed-packet-rehash", "origin-signature", "historical-authority", "accepted-head", "duplicate-packet"} {
		t.Run(mode, func(t *testing.T) {
			editRecoveryEnvelopeEvidence(f, func(body map[string]json.RawMessage, e *acceptanceRecoveryEnvelopeEvidence) error {
				if len(e.EnvironmentChanges) != 1 || e.EnvironmentChanges[0].Change.Change.EnvironmentID != empty {
					return errors.New("expected exact accepted empty-environment source")
				}
				switch mode {
				case "missing-evidence":
					delete(body, "envelopeEvidence")
				case "missing-change":
					e.EnvironmentChanges = e.EnvironmentChanges[:0]
				case "signed-packet-bytes":
					e.EnvironmentChanges[0].Change.Change.RecoveryEnvelope = cryptox.EncodeBase64(bytes.Repeat([]byte{91}, 80))
				case "signed-packet-rehash":
					source := &e.EnvironmentChanges[0]
					source.Change.Change.RecoveryEnvelope = cryptox.EncodeBase64(bytes.Repeat([]byte{91}, 80))
					source.Origin.Origin.ChangeHash = environmentValue(cryptox.EnvironmentChangeReferenceHash(source.Change))
				case "origin-signature":
					e.EnvironmentChanges[0].Origin.Signature = cryptox.EncodeBase64(make([]byte, 64))
				case "historical-authority":
					e.EnvironmentChanges[0].Authorization.Grant.Role = "rw"
				case "accepted-head":
					e.EnvironmentChanges[0].Sequence++
				case "duplicate-packet":
					e.EnvironmentChanges = append(e.EnvironmentChanges, e.EnvironmentChanges[0])
				}
				return nil
			})
			assertRecoveryEnvelopeEvidenceRejected(t, f, f.code)
		})
	}
	f.responseHook.Store(nil)
	recoverer := newMobileManagerActor(t, f)
	if err := recoverer.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverer.workflow.BeginRecoveryWithOrigins(ctx, f.code); err != nil {
		t.Fatal("legitimate empty environment was rejected", err)
	}
	found := false
	for _, e := range environmentValue(recoverer.workflow.RecoveryView()).Environments {
		if e.ID == empty {
			found = true
			if len(e.Variables) != 0 {
				t.Fatal(e)
			}
		}
	}
	if !found {
		t.Fatal("empty environment missing")
	}
	newCode := environmentValue(recoverer.workflow.BeginRecoveryRotation(ctx, "recover-empty-full-manifest"))
	if result, err := recoverer.workflow.CompleteRecoveryRotation(ctx, newCode); err != nil || result.TrustedDevice || result.RecoveryGeneration != "2" {
		t.Fatal(result, err)
	}
	for _, mode := range []string{"missing-rotation", "rewrite-envelope-and-full-hash", "rewrite-current-recovery-public", "rewrite-current-root"} {
		t.Run(mode, func(t *testing.T) {
			editRecoveryEnvelopeEvidence(f, func(body map[string]json.RawMessage, e *acceptanceRecoveryEnvelopeEvidence) error {
				if len(e.RecoveryRotations) != 1 {
					return errors.New("expected exact completed current-generation manifest")
				}
				r := &e.RecoveryRotations[0]
				switch mode {
				case "missing-rotation":
					e.RecoveryRotations = e.RecoveryRotations[:0]
				case "rewrite-envelope-and-full-hash":
					packet := environmentValue(cryptox.WrapEnvironmentKey(bytes.Repeat([]byte{91}, 32), cryptox.EnvelopeContext{AccountID: r.Proof.AccountID, AccountGeneration: r.Proof.AccountGeneration, EnvironmentID: empty, KeyVersion: "1", RecipientType: "recovery", RecipientID: r.Proof.AccountID, RecipientGeneration: r.Proposal.NewRecoveryGeneration, RecipientPublicKey: r.Proposal.NewRecoveryReceivingPublicKey}))
					for i := range r.Proposal.Envelopes {
						if r.Proposal.Envelopes[i].EnvironmentID == empty {
							r.Proposal.Envelopes[i].Envelope = cryptox.EncodeBase64(packet)
						}
					}
					r.Proof.EnvelopesHash = environmentValue(cryptox.RecoveryEnvelopesHash(r.Proposal.Envelopes))
					body["environments"] = environmentValue(json.Marshal(r.Proposal.Envelopes))
				case "rewrite-current-recovery-public":
					r.Proof.NewRecoverySigningPublicKey = cryptox.EncodeBase64(bytes.Repeat([]byte{91}, 32))
				case "rewrite-current-root":
					r.Proposal.NewTrustRoot.RootDeviceID = "other-root"
				}
				return nil
			})
			assertRecoveryEnvelopeEvidenceRejected(t, f, newCode)
		})
	}
	f.responseHook.Store(nil)
	fresh := newMobileManagerActor(t, f)
	if err := fresh.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.workflow.BeginRecoveryWithOrigins(ctx, newCode); err != nil {
		t.Fatal("current code and exact full signed manifest failed", err)
	}
	fresh.reopen(t)
	if v, err := fresh.workflow.RecoveryView(); err != nil || v.Info.TrustedDevice || len(v.Environments) != 2 {
		t.Fatal(v, err)
	}
}

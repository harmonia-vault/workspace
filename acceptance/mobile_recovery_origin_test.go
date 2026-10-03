package acceptance

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/pairing"
	"github.com/harmonia-vault/core-go/syncclient"
)

// Reuse the exact AES-authenticated accepted receipt as native source, never a
// synthetic trusted bool or the service's public device directory.
func recoveryOriginBoot(t *testing.T, f *mobileManagerFixture, a *mobileManagerActor) (syncclient.DeviceSession, localstate.State) {
	t.Helper()
	var native struct {
		AccountID  string           `json:"accountId"`
		Generation string           `json:"accountGeneration"`
		DeviceID   string           `json:"deviceId"`
		Cloud      localstate.State `json:"cloud"`
		Enrollment struct {
			Receipt syncclient.EnrollmentReceiptV3 `json:"receipt"`
		} `json:"enrollmentV3"`
	}
	if err := json.Unmarshal(a.load(), &native); err != nil {
		t.Fatal(err)
	}
	engine := environmentValue(localstate.New(&originMemoryStore{state: native.Cloud}))
	gen := environmentValue(strconv.ParseUint(native.Generation, 10, 64))
	verifier := environmentValue(syncclient.NewPinnedVerifierV3(syncclient.IssuerOriginPinnedTrust{AccountID: native.AccountID, AccountGeneration: gen, DeviceID: native.DeviceID, DeviceSigningPublicKey: a.config.SigningKey.Public().(ed25519.PublicKey), ReceivingPrivateKey: a.config.ReceivingPrivateKey, Receipt: native.Enrollment.Receipt}))
	defer verifier.Close()
	c := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: native.AccountID, AccountGeneration: gen, DeviceID: native.DeviceID, Engine: engine, Verifier: verifier}))
	if _, err := c.BootDevice(context.Background(), a.config.SigningKey); err != nil {
		t.Fatal(err)
	}
	// BootDevice intentionally keeps bearer private. A precise signed device
	// challenge below obtains the independent test-only session from real service.
	var challenge syncclient.DeviceChallenge
	if status := callJSON(t, f.proxy.Client(), f.proxy.URL, "/v1/accounts/"+native.AccountID+"/boot-challenges", "", "", "", map[string]string{"deviceId": native.DeviceID, "accountGeneration": native.Generation}, &challenge); status != 200 {
		t.Fatal(status)
	}
	pub := environmentValue(ecdh.X25519().NewPrivateKey(a.config.ReceivingPrivateKey)).PublicKey().Bytes()
	proof := environmentValue(cryptox.NewDeviceBootProof(native.AccountID, native.Generation, native.DeviceID, a.config.SigningKey.Public().(ed25519.PublicKey), pub, challenge.ChallengeID, challenge.Nonce, strconv.FormatInt(challenge.ExpiresAt, 10)))
	sig := environmentValue(cryptox.SignDeviceBootProof(proof, a.config.SigningKey))
	var session syncclient.DeviceSession
	if status := callJSON(t, f.proxy.Client(), f.proxy.URL, "/v1/accounts/"+native.AccountID+"/boot-sessions", "", "", "", map[string]string{"deviceId": native.DeviceID, "accountGeneration": native.Generation, "challengeId": challenge.ChallengeID, "signature": sig}, &session); status != 200 {
		t.Fatal(status)
	}
	return session, native.Cloud
}
func recoveryOriginRevokeDevice(t *testing.T, f *mobileManagerFixture, b, recipient *mobileManagerActor, id string) {
	t.Helper()
	ctx := context.Background()
	bv := environmentValue(b.workflow.Pull(ctx))
	target := environmentValue(recipient.workflow.View()).DeviceID
	session, state := recoveryOriginBoot(t, f, b)
	var native struct {
		AccountID, DeviceID string
		Generation          string `json:"accountGeneration"`
	}
	if err := json.Unmarshal(b.load(), &native); err != nil {
		t.Fatal(err)
	}
	base := "/v1/accounts/" + native.AccountID + "/device-revocations"
	var r cryptox.DeviceRevocation
	if status := callJSON(t, f.proxy.Client(), f.proxy.URL, base, session.Token, bv.DeviceID, native.Generation, map[string]string{"idempotencyKey": id, "subjectDeviceId": target}, &r); status != 200 {
		t.Fatal(status)
	}
	hash := sha256.Sum256([]byte(session.Token))
	rootPub := recipient.config.SigningKey.Public().(ed25519.PublicKey)
	x := environmentValue(ecdh.X25519().NewPrivateKey(recipient.config.ReceivingPrivateKey)).PublicKey().Bytes()
	if r.SubjectDeviceID != target || r.DeviceID != bv.DeviceID || r.SubjectSigningPublicKey != cryptox.EncodeBase64(rootPub) || r.SubjectReceivingPublicKey != cryptox.EncodeBase64(x) || r.SessionHash != hex.EncodeToString(hash[:]) || len(r.Authorities) != len(state.Cloud.Environments) {
		t.Fatal("challenge did not bind accepted devices and all local authorities")
	}
	for _, a := range r.Authorities {
		e := state.Cloud.Environments[a.EnvironmentID]
		if e.Role != localstate.Admin || a.KeyVersion != strconv.FormatUint(e.KeyVersion, 10) || a.GrantGeneration != strconv.FormatUint(e.GrantGeneration, 10) {
			t.Fatal("challenge authority changed")
		}
	}
	signed := environmentValue(cryptox.SignDeviceRevocation(r, b.config.SigningKey))
	if status := callJSON(t, f.proxy.Client(), f.proxy.URL, base+"/complete", session.Token, bv.DeviceID, native.Generation, signed, &struct {
		Sequence uint64 `json:"sequence"`
		Replayed bool   `json:"replayed"`
	}{}); status != 200 {
		t.Fatal(status)
	}
	if _, err := recipient.workflow.Pull(ctx); !errors.Is(err, syncclient.ErrTrustInvalidated) {
		t.Fatal("real root revocation did not invalidate", err)
	}
}
func recoveryOriginPopulated(t *testing.T) (*mobileManagerFixture, *mobileManagerActor, *mobileManagerActor, string) {
	t.Helper()
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	b := newMobileManagerActor(t, f)
	expires := strconv.FormatInt(time.Now().Unix()+600, 10)
	if _, err := f.enroll(t, f.root, b, "recover-origin-b", []mobileworkflow.ApprovalSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: expires}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.root.RetryApprovalV3(ctx, "recover-origin-b"); err != nil {
		t.Fatal(err)
	}
	view := environmentValue(b.workflow.CreateEnvironment(ctx, "恢复来源Y", "recover-origin-y"))
	y := ""
	for _, e := range view.Environments {
		if e.ID != f.initial {
			y = e.ID
		}
	}
	if y == "" {
		t.Fatal(view)
	}
	c := newMobileManagerActor(t, f)
	if _, err := f.enroll(t, b.workflow, c, "recover-origin-c", []mobileworkflow.ApprovalSelection{{EnvironmentID: y, Role: "rw", ExpiresAt: expires}}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.workflow.RetryApprovalV3(ctx, "recover-origin-c"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.workflow.SetVariable(ctx, y, "SYNTHETIC_C_WRITE", "synthetic-c-shared-value", "recover-c-y-put"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.workflow.RotateEnvironment(ctx, f.initial, "recover-origin-x-rotate"); err != nil {
		t.Fatal(err)
	}
	return f, b, c, y
}
func recoveryOriginAssertValues(t *testing.T, w *mobileworkflow.Workflow, x, y string) {
	t.Helper()
	v := environmentValue(w.RecoveryView())
	if v.Info.TrustedDevice || len(v.Environments) != 2 {
		t.Fatal(v.Info)
	}
	found := map[string]mobileworkflow.RecoveredEnvironment{}
	for _, e := range v.Environments {
		found[e.ID] = e
	}
	if found[x].KeyVersion != "2" || found[x].Variables["SYNTHETIC_X"] != "synthetic-x-value" || found[y].Variables["SYNTHETIC_C_WRITE"] != "synthetic-c-shared-value" {
		t.Fatal("recovery did not authenticate/decrypt exact current versions")
	}
	if _, err := w.View(); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
		t.Fatal("recovery became management", err)
	}
}
func TestMobileRecoveryOriginRealCurrentVersionsRevokedActorsAndFullRotation(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("requires real native SPAKE2")
	}
	f, b, c, y := recoveryOriginPopulated(t)
	ctx := context.Background()
	recoveryOriginRevokeDevice(t, f, b, f.rootActor, "origin-recovery-revoke-root")
	recoveryOriginRevokeDevice(t, f, b, c, "origin-recovery-revoke-c")
	// Only Y is held by C; global B self revocation removes the last X Admin.
	if result, err := b.workflow.RevokeSelf(ctx, "recover-origin-b-self-revoke"); err != nil || !result.Completed {
		t.Fatal(result, err)
	}
	recovered := newMobileManagerActor(t, f)
	if err := recovered.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	info, err := recovered.workflow.BeginRecoveryWithOrigins(ctx, f.code)
	if err != nil {
		t.Fatal("full origins after root/B revocation", err)
	}
	if info.TrustedDevice || !info.RotationRequired {
		t.Fatal(info)
	}
	recoveryOriginAssertValues(t, recovered.workflow, f.initial, y)
	recovered.reopen(t)
	recoveryOriginAssertValues(t, recovered.workflow, f.initial, y)
	// Even authenticated local material cannot drop the required origin graph and
	// claim a compatible initial-only recovery to expose this newer vault.
	var state map[string]json.RawMessage
	if err = json.Unmarshal(recovered.load(), &state); err != nil {
		t.Fatal(err)
	}
	var record map[string]json.RawMessage
	_ = json.Unmarshal(state["recovery"], &record)
	var vault map[string]json.RawMessage
	_ = json.Unmarshal(record["vault"], &vault)
	vault["issuerEvidence"] = json.RawMessage("null")
	record["vault"] = environmentValue(json.Marshal(vault))
	state["recovery"] = environmentValue(json.Marshal(record))
	badConfig := recovered.config
	badConfig.ProtectedState = environmentValue(json.Marshal(state))
	if bad, err := mobileworkflow.New(badConfig); err == nil {
		bad.Close()
		t.Fatal("originsRequired context restored without graph")
	}
	newCode := environmentValue(recovered.workflow.BeginRecoveryRotation(ctx, "recover-origin-new-code"))
	if _, err := recovered.workflow.CompleteRecoveryRotation(ctx, newCode[:51]); err == nil {
		t.Fatal("partial full reentry accepted")
	}
	done, err := recovered.workflow.CompleteRecoveryRotation(ctx, newCode)
	if err != nil {
		t.Fatal("full origin all-envelope rotation", err)
	}
	if done.TrustedDevice || done.RotationRequired || done.RecoveryGeneration != "2" {
		t.Fatal(done)
	}
	recovered.reopen(t)
	recoveryOriginAssertValues(t, recovered.workflow, f.initial, y)
	fresh := newMobileManagerActor(t, f)
	if err := fresh.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.workflow.BeginRecoveryWithOrigins(ctx, f.code); err == nil {
		t.Fatal("old recovery code remained valid")
	}
	if _, err := fresh.workflow.BeginRecoveryWithOrigins(ctx, newCode); err != nil {
		t.Fatal("new current recovery root lost original genesis", err)
	}
	recoveryOriginAssertValues(t, fresh.workflow, f.initial, y)
}
func TestMobileRecoveryOriginMissingTamperedGraphFailsClosedWithoutFallback(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("requires real native SPAKE2")
	}
	f, _, _, _ := recoveryOriginPopulated(t)
	for _, mode := range []string{"missing", "origin-signature", "genesis-parent", "wrong-root", "target-version"} {
		t.Run(mode, func(t *testing.T) {
			f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
				if r.StatusCode != 200 || r.Request.Method != "GET" || !strings.HasSuffix(r.Request.URL.Path, "/recovery-vault") {
					return nil
				}
				data, err := io.ReadAll(r.Body)
				if err != nil {
					return err
				}
				_ = r.Body.Close()
				var b map[string]json.RawMessage
				if err = json.Unmarshal(data, &b); err != nil {
					return err
				}
				var p cryptox.IssuerProofV2
				if err = json.Unmarshal(b["issuerEvidence"], &p); err != nil {
					return err
				}
				switch mode {
				case "missing":
					b["issuerEvidence"] = json.RawMessage("null")
				case "origin-signature":
					if len(p.Origins) == 0 {
						return errors.New("synthetic full origin missing")
					}
					p.Origins[0].Signature = cryptox.EncodeBase64(make([]byte, 64))
				case "genesis-parent":
					found := false
					for i := range p.Authorities {
						if p.Authorities[i].ParentHash != "" {
							p.Authorities[i].ParentHash = ""
							found = true
							break
						}
					}
					if !found {
						return errors.New("synthetic graph has no non-genesis edge")
					}
				case "wrong-root":
					p.TrustRoot.RootSigningPublicKey = cryptox.EncodeBase64(bytes.Repeat([]byte{11}, 32))
				case "target-version":
					p.Targets = p.Targets[:1]
				}
				if mode != "missing" {
					b["issuerEvidence"], err = json.Marshal(p)
					if err != nil {
						return err
					}
				}
				data, err = json.Marshal(b)
				if err != nil {
					return err
				}
				r.Body = io.NopCloser(bytes.NewReader(data))
				r.ContentLength = int64(len(data))
				r.Header.Set("Content-Length", strconv.Itoa(len(data)))
				return nil
			}})
			a := newMobileManagerActor(t, f)
			if err := a.workflow.Login(context.Background(), f.email, f.password); err != nil {
				t.Fatal(err)
			}
			if _, err := a.workflow.BeginRecoveryWithOrigins(context.Background(), f.code); !errors.Is(err, mobileworkflow.ErrRecoveryEvidence) {
				t.Fatal("unproven origin recovery did not fail closed", err)
			}
			info := environmentValue(a.workflow.RecoveryInfo())
			if info.State != "none" || info.TrustedDevice {
				t.Fatal("partial source failure persisted recovery", info)
			}
		})
	}
	f.responseHook.Store(nil)
}

func TestMobileRecoveryProcessSessionActualHTTPSDetachResumeSameBindingAndScrub(t *testing.T) {
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	a := newMobileManagerActor(t, f)
	if err := a.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	owner, info, err := a.workflow.BeginRecoveryWithOriginsSession(ctx, f.code)
	if err != nil || info.TrustedDevice {
		t.Fatal(info, err)
	}
	defer owner.Close()
	before := environmentValue(owner.Binding())
	if before.ExpiresAt > time.Now().Unix()+300 || before.SessionHash == "" || before.TransitionID != "" {
		t.Fatal("process owner bounds changed")
	}
	seed := environmentValue(cryptox.DecodeRecoveryCode(f.code))
	keys := environmentValue(cryptox.DeriveRecoveryKeys(seed, before.AccountID, before.AccountGeneration, before.RecoveryGeneration))
	sealed := a.load()
	for _, secret := range [][]byte{seed, keys.SigningPrivate, keys.ReceivingPrivate, []byte(f.code), []byte(cryptox.EncodeBase64(seed)), []byte(cryptox.EncodeBase64(keys.SigningPrivate))} {
		if bytes.Contains(sealed, secret) {
			t.Fatal("recovery code or derived private material persisted")
		}
	}
	clear(seed)
	clear(keys.SigningPrivate)
	clear(keys.ReceivingPrivate)
	a.reopen(t)
	if _, err = owner.Binding(); err != nil {
		t.Fatal("native per-operation close cleared registry owner", err)
	}
	if err = a.workflow.AttachRecoverySession(owner); err != nil {
		t.Fatal("new native operation could not attach exact context", err)
	}
	newCode := environmentValue(a.workflow.BeginRecoveryRotation(ctx, "process-owner-original-rotation"))
	bound := environmentValue(owner.Binding())
	if bound.SessionHash != before.SessionHash || bound.TransitionID != "process-owner-original-rotation" || bound.TransitionHash == "" || bound.ExpiresAt > before.ExpiresAt {
		t.Fatal("original session or rotation binding changed")
	}
	owner.Cancel()
	a.reopen(t)
	if err = a.workflow.AttachRecoverySession(owner); !errors.Is(err, mobileworkflow.ErrRecoverySession) {
		t.Fatal("cancelled owner revived", err)
	}
	resumed, err := a.workflow.ResumeRecoverySession(ctx, f.code)
	if err != nil {
		t.Fatal("explicit interrupted old-code reentry could not resume original bearer", err)
	}
	defer resumed.Close()
	resumedBinding := environmentValue(resumed.Binding())
	if resumedBinding.SessionHash != bound.SessionHash || resumedBinding.TransitionID != bound.TransitionID || resumedBinding.TransitionHash != bound.TransitionHash {
		t.Fatal("owner restart changed original token/nonce/id")
	}
	done, err := a.workflow.CompleteRecoveryRotation(ctx, newCode)
	if err != nil || done.TrustedDevice || done.RotationRequired {
		t.Fatal(done, err)
	}
	if _, err = resumed.Binding(); !errors.Is(err, mobileworkflow.ErrRecoverySession) {
		t.Fatal("completed proof retained old signer", err)
	}
	a.reopen(t)
	if _, err = a.workflow.View(); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
		t.Fatal("process signer completed management without authority transition", err)
	}
}

// HPKE recipient public keys are public. Decryption alone cannot authenticate
// an envelope's sender or prove it contains the manager's environment key.
func TestMobileRecoveryOriginEmptyEnvironmentRejectsPubliclyForgedEnvelope(t *testing.T) {
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	view := environmentValue(f.root.CreateEnvironment(ctx, "空恢复环境", "recover-empty-env"))
	empty := ""
	for _, e := range view.Environments {
		if e.ID != f.initial {
			empty = e.ID
		}
	}
	if empty == "" {
		t.Fatal(view)
	}
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.StatusCode != 200 || r.Request.Method != "GET" || !strings.HasSuffix(r.Request.URL.Path, "/recovery-vault") {
			return nil
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
		var account, generation, recGeneration, recPublic string
		_ = json.Unmarshal(body["accountId"], &account)
		_ = json.Unmarshal(body["accountGeneration"], &generation)
		_ = json.Unmarshal(body["recoveryGeneration"], &recGeneration)
		_ = json.Unmarshal(body["recoveryReceivingPublicKey"], &recPublic)
		var envelopes []cryptox.RecoveryEnvelope
		if err = json.Unmarshal(body["environments"], &envelopes); err != nil {
			return err
		}
		attackerKey := bytes.Repeat([]byte{91}, 32)
		defer clear(attackerKey)
		for i, e := range envelopes {
			if e.EnvironmentID == empty {
				packet, err := cryptox.WrapEnvironmentKey(attackerKey, cryptox.EnvelopeContext{AccountID: account, AccountGeneration: generation, EnvironmentID: empty, KeyVersion: e.KeyVersion, RecipientType: "recovery", RecipientID: account, RecipientGeneration: recGeneration, RecipientPublicKey: recPublic})
				if err != nil {
					return err
				}
				envelopes[i].Envelope = cryptox.EncodeBase64(packet)
			}
		}
		body["environments"], err = json.Marshal(envelopes)
		if err != nil {
			return err
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
	a := newMobileManagerActor(t, f)
	if err := a.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	if _, err := a.workflow.BeginRecoveryWithOrigins(ctx, f.code); !errors.Is(err, mobileworkflow.ErrRecoveryEvidence) {
		t.Fatal("publicly forged empty environment recovery envelope was trusted", err)
	}
}

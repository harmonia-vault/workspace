package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"
)

func b3Rotated(t *testing.T) (*b2Mobile, string) {
	t.Helper()
	b := b2New(t)
	code, e := b.begin(t, "preparation-transition-challenge")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.seal(t, code); e != nil {
		t.Fatal(e)
	}
	info, e := b.retry(t, "preparation-transition-submit")
	if e != nil || info.RotationRequired || info.TrustedDevice || info.ExpiresAt != b.opened.ExpiresAt {
		t.Fatal("same-session applied transition prerequisite", e)
	}
	return b, code
}
func b3Selection(t *testing.T, b *b2Mobile, role string) mobileworkflow.DAGRecoveredIntent {
	t.Helper()
	b.routes.set("choices")
	w := b.open(t)
	defer w.Close()
	c, e := w.DAGRecoveredEnrollmentChoices(context.Background(), b.registry, b.scope)
	if e != nil || c.TrustedDevice || len(c.Environments) == 0 {
		t.Fatal("choices", e)
	}
	rights := make([]cryptox.RecoveredDeviceRight, 0, len(c.Environments))
	for _, env := range c.Environments {
		rights = append(rights, cryptox.RecoveredDeviceRight{EnvironmentID: env.EnvironmentID, KeyVersion: env.KeyVersion, Role: role, ExpiresAt: strconv.FormatInt(time.Now().Unix()+3600, 10)})
	}
	return mobileworkflow.DAGRecoveredIntent{ExpectedSequence: c.Sequence, RecoveryHeadHash: c.RecoveryHeadHash, SelectedRights: rights}
}
func b3Seal(t *testing.T, b *b2Mobile, in mobileworkflow.DAGRecoveredIntent, phase string) (mobileworkflow.DAGRecoveredInfo, error) {
	t.Helper()
	b.routes.set(phase)
	w := b.open(t)
	defer w.Close()
	return w.SealDAGRecoveredDevice(context.Background(), b.registry, b.scope, in)
}
func b3Retry(t *testing.T, b *b2Mobile, phase string) (mobileworkflow.DAGRecoveredInfo, error) {
	t.Helper()
	b.routes.set(phase)
	w := b.open(t)
	defer w.Close()
	return w.RetryDAGRecoveredDevice(context.Background(), b.registry, b.scope)
}
func b3Preparation(t *testing.T, b *b2Mobile) syncclient.DAGRecoveredPreparation {
	t.Helper()
	raw := b.slot.read()
	defer clear(raw)
	var s struct {
		Preparation *struct {
			Record json.RawMessage `json:"record"`
		} `json:"recoveryDAGRecoveredPreparation"`
	}
	if json.Unmarshal(raw, &s) != nil || s.Preparation == nil {
		t.Fatal("missing original recovered preparation")
	}
	p, e := syncclient.DecodeDAGRecoveredPreparation(s.Preparation.Record)
	if e != nil {
		t.Fatal(e)
	}
	return p
}
func b3AssertRoutes(t *testing.T, b *b2Mobile, challenges, submits int) {
	t.Helper()
	b.routes.mu.Lock()
	defer b.routes.mu.Unlock()
	allowed := map[string]bool{"GET protocol-info": true, "POST recovery-challenges": true, "POST recovery-sessions": true, "GET recovery-vault-v2": true, "POST recovery-authority-challenges-v2": true, "GET recovery-authority-transitions-v2/original": true, "POST recovery-authority-transitions-v2": true, "POST recovered-device-challenges-v2": true, "GET recovered-devices-v2/original": true, "POST recovered-devices-v2": true}
	gotChallenge, gotSubmit := 0, 0
	for phase, rows := range b.routes.counts {
		for route, n := range rows {
			if !allowed[route] {
				t.Fatal("unexpected route (B3a cannot Boot/Pull)", phase, route)
			}
			if route == "POST recovered-device-challenges-v2" {
				gotChallenge += n
			}
			if route == "POST recovered-devices-v2" {
				gotSubmit += n
			}
		}
	}
	if gotChallenge != challenges || gotSubmit != submits {
		t.Fatal("original route counts", gotChallenge, gotSubmit)
	}
	raw, _ := json.Marshal(b.routes.counts)
	t.Log("B3A_ROUTE_COUNTS", string(raw))
}
func b3Count(b *b2Mobile) int {
	b.routes.mu.Lock()
	defer b.routes.mu.Unlock()
	n := 0
	for _, rs := range b.routes.counts {
		for _, v := range rs {
			n += v
		}
	}
	return n
}
func TestMobileDAGRecoveredHTTPS(t *testing.T) {
	t.Run("explicit-rights-original-seal-confirmed-still-restricted", func(t *testing.T) {
		b, _ := b3Rotated(t)
		in := b3Selection(t, b, "admin")
		pending, e := b3Seal(t, b, in, "recovered-intent-challenge-seal")
		if e != nil || pending.OperationID == "" || pending.TrustedDevice || pending.OriginalConfirmed {
			t.Fatal("seal", e)
		}
		before, count := b.slot.read(), b3Count(b)
		again, e := b3Seal(t, b, in, "same-sealed-call")
		if e != nil || again != pending || !bytes.Equal(before, b.slot.read()) || b3Count(b) != count {
			t.Fatal("same sealed tuple was replaced or contacted network", e)
		}
		confirmed, e := b3Retry(t, b, "original-query-submit-confirm")
		if e != nil || confirmed.OperationID != pending.OperationID || confirmed.Acceptance != "accepted" || !confirmed.OriginalConfirmed || confirmed.TrustedDevice || confirmed.State != "accepted-not-device-applied" {
			t.Fatal("confirm must remain restricted", e)
		}
		w := b.open(t)
		defer w.Close()
		if _, e = w.View(); !errors.Is(e, mobileworkflow.ErrRecoveryRestricted) {
			t.Fatal("registration exposed vault before B3b", e)
		}
		b.routes.set("owner-info-after-original")
		owner, e := w.RecoveryDAGOwnerInfo(context.Background(), b.registry, b.scope)
		if e != nil || owner.TrustedDevice || owner.RotationRequired || owner.ExpiresAt != b.opened.ExpiresAt {
			t.Fatal("owner identity/deadline changed", e)
		}
		cold, e := w.DAGRecoveredDeviceInfo()
		if e != nil || cold != confirmed {
			t.Fatal("durable confirmed metadata", e)
		}
		repeat, e := b3Retry(t, b, "same-original-accepted-query")
		if e != nil || repeat != confirmed {
			t.Fatal("same original retry", e)
		}
		b3AssertRoutes(t, b, 1, 1)
	})
	t.Run("accepted-loss-CAS-failure-cold-current-code-original-query", func(t *testing.T) {
		b, code := b3Rotated(t)
		in := b3Selection(t, b, "rw")
		pending, e := b3Seal(t, b, in, "recovered-seal")
		if e != nil {
			t.Fatal(e)
		}
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovered-devices-v2") && r.StatusCode == 200 {
				b2LoseResponse(r)
			}
			return nil
		}})
		if _, e = b3Retry(t, b, "submit-response-lost"); !errors.Is(e, syncclient.ErrEnrollmentPending) {
			t.Fatal("uncertainty lost owner", e)
		}
		before := b.slot.read()
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "GET" && strings.Contains(r.Request.URL.Path, "/recovered-devices-v2/") && r.StatusCode == 200 {
				b.slot.mu.Lock()
				b.slot.fail = true
				b.slot.mu.Unlock()
			}
			return nil
		}})
		if _, e = b3Retry(t, b, "accepted-query-CAS-fails"); !errors.Is(e, mobileworkflow.ErrDAGPersistence) || !bytes.Equal(before, b.slot.read()) {
			t.Fatal("failed CAS claimed success", e)
		}
		b.f.responseHook.Store(nil)
		b.slot.mu.Lock()
		b.slot.fail = false
		b.slot.mu.Unlock()
		w := b.open(t)
		b.routes.set("retired-owner-check")
		if _, e = w.RecoveryDAGOwnerInfo(context.Background(), b.registry, b.scope); !errors.Is(e, mobileworkflow.ErrDAGOwnerMissing) {
			t.Fatal("failed owner survived", e)
		}
		w.Close()
		w = b.open(t)
		defer w.Close()
		b.routes.set("cold-fresh-session-original-query")
		out, e := w.QueryRecoveryDAGOriginal(context.Background(), []byte(code))
		if e != nil || out.Observation != "accepted" || out.Pending.OperationID != pending.OperationID || !out.Pending.OriginalApplied || out.TrustedDevice || !out.RotationRequired || out.Confirmation != "original-verified-and-saved" {
			t.Fatal("cold original query", e)
		}
		if _, e = w.View(); !errors.Is(e, mobileworkflow.ErrRecoveryRestricted) {
			t.Fatal("cold query granted trust", e)
		}
		b3AssertRoutes(t, b, 1, 1)
	})
	t.Run("lost-challenge-same-ID-cold-unaccepted-original-preserved", func(t *testing.T) {
		b, code := b3Rotated(t)
		in := b3Selection(t, b, "ro")
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovered-device-challenges-v2") && r.StatusCode == 200 {
				b2LoseResponse(r)
			}
			return nil
		}})
		if _, e := b3Seal(t, b, in, "challenge-response-lost"); !errors.Is(e, syncclient.ErrDAGPreparationPending) {
			t.Fatal("lost challenge", e)
		}
		intent := b3Preparation(t, b)
		if intent.Phase != "intent" {
			t.Fatal("missing durable intent")
		}
		before, count := b.slot.read(), b3Count(b)
		cold := b.open(t)
		info, e := cold.DAGRecoveredDeviceInfo()
		if e != nil || !info.NeedsOriginalOwner || info.OperationID != intent.OperationID || info.TrustedDevice {
			t.Fatal("cold interrupted metadata", e)
		}
		other := environmentValue(mobileworkflow.NewDAGRecoveryRegistry(b.scope))
		defer other.Close()
		if _, e = cold.OpenDAGRecoveryOwner(context.Background(), other, b.scope, []byte(code)); !errors.Is(e, mobileworkflow.ErrDAGPreparationInterrupted) {
			t.Fatal("cold prep replaced owner", e)
		}
		if _, e = cold.QueryRecoveryDAGOriginal(context.Background(), []byte(code)); !errors.Is(e, mobileworkflow.ErrDAGPreparationInterrupted) {
			t.Fatal("prep became sealed query", e)
		}
		cold.Close()
		if b3Count(b) != count || !bytes.Equal(before, b.slot.read()) {
			t.Fatal("cold prep sent HTTP/saved")
		}
		b.f.responseHook.Store(nil)
		pending, e := b3Seal(t, b, in, "same-original-challenge-retry")
		if e != nil || pending.OperationID != intent.OperationID || pending.TrustedDevice {
			t.Fatal("same original retry", e)
		}
		b.registry.Close()
		before = b.slot.read()
		cold = b.open(t)
		defer cold.Close()
		b.routes.set("cold-unaccepted-query")
		out, e := cold.QueryRecoveryDAGOriginal(context.Background(), []byte(code))
		if e != nil || out.Observation != "not-accepted-at-query" || out.Pending.OperationID != intent.OperationID || out.Pending.OriginalApplied || out.TrustedDevice || !out.RotationRequired || !bytes.Equal(before, b.slot.read()) {
			t.Fatal("not-accepted became closure", e)
		}
		b3AssertRoutes(t, b, 2, 0)
	})
}

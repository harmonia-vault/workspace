package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
)

type b2Mobile struct {
	f        *mobileManagerFixture
	routes   *s2aRoutes
	config   mobileworkflow.Config
	slot     *s2aNativeState
	registry *mobileworkflow.DAGRecoveryRegistry
	scope    mobileworkflow.DAGOwnerScope
	opened   syncclient.DAGRecoveryInfo
}

func b2New(t *testing.T) *b2Mobile {
	t.Helper()
	f := newMobileManagerFixture(t)
	a := newMobileManagerActor(t, f)
	if err := a.workflow.Login(context.Background(), f.email, f.password); err != nil {
		t.Fatal(err)
	}
	raw := environmentValue(a.workflow.ExportProtectedState())
	if err := a.config.SaveProtectedState(raw); err != nil {
		clear(raw)
		t.Fatal(err)
	}
	clear(raw)
	a.workflow.Close()
	slot := &s2aNativeState{save: a.config.SaveProtectedState, load: a.load}
	routes := &s2aRoutes{phase: "owner-open", counts: map[string]map[string]int{}}
	c := a.config
	c.HTTPClient = routes.client(f.proxy.Client())
	c.SaveProtectedState = func([]byte) error { return errors.New("B2 requires atomic whole-state CAS") }
	c.SaveProtectedStateCAS = slot.cas
	c.CheckProtectedState = slot.check
	scope := mobileworkflow.DAGOwnerScope{Namespace: "synthetic-go-native", Slot: "b2-transition", PlatformEpoch: 31}
	registry := environmentValue(mobileworkflow.NewDAGRecoveryRegistry(scope))
	t.Cleanup(registry.Close)
	b := &b2Mobile{f: f, routes: routes, config: c, slot: slot, registry: registry, scope: scope}
	w := b.open(t)
	code := []byte(f.code)
	var err error
	b.opened, err = w.OpenDAGRecoveryOwner(context.Background(), registry, scope, code)
	w.Close()
	if err != nil || !b.opened.RotationRequired || b.opened.TrustedDevice || !bytes.Equal(code, make([]byte, len(code))) {
		t.Fatal("restricted owner open", err)
	}
	return b
}
func (b *b2Mobile) open(t *testing.T) *mobileworkflow.Workflow {
	t.Helper()
	return s2aOpen(t, b.config, b.slot)
}
func (b *b2Mobile) begin(t *testing.T, phase string) (string, error) {
	t.Helper()
	b.routes.set(phase)
	w := b.open(t)
	defer w.Close()
	return w.BeginDAGRecoveryTransition(context.Background(), b.registry, b.scope)
}
func (b *b2Mobile) seal(t *testing.T, code string) (mobileworkflow.RecoveryDAGPendingInfo, error) {
	t.Helper()
	b.routes.set("seal")
	w := b.open(t)
	defer w.Close()
	in := []byte(code)
	out, err := w.SealDAGRecoveryTransition(context.Background(), b.registry, b.scope, in)
	if !bytes.Equal(in, make([]byte, len(in))) {
		t.Fatal("new code input not erased")
	}
	return out, err
}
func (b *b2Mobile) retry(t *testing.T, phase string) (syncclient.DAGRecoveryInfo, error) {
	t.Helper()
	b.routes.set(phase)
	w := b.open(t)
	defer w.Close()
	return w.RetryDAGRecoveryTransition(context.Background(), b.registry, b.scope)
}
func (b *b2Mobile) preparation(t *testing.T) syncclient.DAGTransitionPreparation {
	t.Helper()
	raw := b.slot.read()
	defer clear(raw)
	var s struct {
		Preparation *struct {
			Record json.RawMessage `json:"record"`
		} `json:"recoveryDAGPreparation"`
	}
	if json.Unmarshal(raw, &s) != nil || s.Preparation == nil {
		t.Fatal("missing durable preparation")
	}
	p, err := syncclient.DecodeDAGTransitionPreparation(s.Preparation.Record)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func (b *b2Mobile) assertRoutes(t *testing.T, wantChallenges, wantSubmits int) {
	t.Helper()
	b.routes.mu.Lock()
	defer b.routes.mu.Unlock()
	challenges, submits := 0, 0
	allowed := map[string]bool{"GET protocol-info": true, "POST recovery-challenges": true, "POST recovery-sessions": true, "GET recovery-vault-v2": true, "POST recovery-authority-challenges-v2": true, "GET recovery-authority-transitions-v2/original": true, "POST recovery-authority-transitions-v2": true}
	for phase, rows := range b.routes.counts {
		for route, n := range rows {
			if !allowed[route] {
				t.Fatal("unexpected B2 route", phase, route)
			}
			if route == "POST recovery-authority-challenges-v2" {
				challenges += n
			}
			if route == "POST recovery-authority-transitions-v2" {
				submits += n
			}
		}
	}
	if challenges != wantChallenges || submits != wantSubmits {
		t.Fatal("wrong original business counts", challenges, submits)
	}
	raw, _ := json.Marshal(b.routes.counts)
	t.Log("B2_ROUTE_COUNTS", string(raw))
}
func b2LoseResponse(response *http.Response) {
	_ = response.Body.Close()
	response.StatusCode = 504
	response.Body = io.NopCloser(strings.NewReader(`{"error":"synthetic_response_loss"}`))
	response.ContentLength = -1
	response.Header.Del("Content-Length")
}

func TestMobileDAGTransitionHTTPS(t *testing.T) {
	t.Run("prepared-reentry-sealed-applied-across-workflows", func(t *testing.T) {
		b := b2New(t)
		code, err := b.begin(t, "prepare")
		if err != nil || code == "" {
			t.Fatal("prepare", err)
		}
		preparation := b.preparation(t)
		if preparation.Phase != "prepared" {
			t.Fatal("code escaped before prepared persistence")
		}
		before := b.slot.read()
		if repeat, err := b.begin(t, "already-prepared"); !errors.Is(err, mobileworkflow.ErrDAGCodeAlreadyPrepared) || repeat != "" || !bytes.Equal(before, b.slot.read()) {
			t.Fatal("second display regenerated code", err)
		}
		if _, err = b.seal(t, b.f.code); !errors.Is(err, syncclient.ErrDAGNewCodeMismatch) || !bytes.Equal(before, b.slot.read()) {
			t.Fatal("wrong complete code changed preparation/owner", err)
		}
		pending, err := b.seal(t, code)
		if err != nil || pending.OperationID != preparation.OperationID || pending.OriginalApplied || pending.TrustedDevice {
			t.Fatal("single CAS seal", err)
		}
		final, err := b.retry(t, "query-and-submit")
		if err != nil || final.RotationRequired || final.TrustedDevice || final.ExpiresAt != b.opened.ExpiresAt {
			t.Fatal("verified durable controlled rotation", err)
		}
		w := b.open(t)
		current, err := w.RecoveryDAGPendingInfo()
		if err != nil || !current.OriginalApplied || current.AcceptedSequence != preparation.ExpectedSequence+1 || current.OperationID != preparation.OperationID {
			t.Fatal("applied original", err)
		}
		b.routes.set("owner-info-after-rotation")
		info, err := w.RecoveryDAGOwnerInfo(context.Background(), b.registry, b.scope)
		w.Close()
		if err != nil || info != final {
			t.Fatal("controlled binding cannot reattach", err)
		}
		repeated, err := b.retry(t, "same-applied-retry")
		if err != nil || repeated != final {
			t.Fatal("same original replay", err)
		}
		b.assertRoutes(t, 1, 1)
	})
	t.Run("accepted-response-loss-save-failure-cold-original-query", func(t *testing.T) {
		b := b2New(t)
		code, err := b.begin(t, "prepare")
		if err != nil {
			t.Fatal(err)
		}
		pending, err := b.seal(t, code)
		if err != nil {
			t.Fatal(err)
		}
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovery-authority-transitions-v2") && r.StatusCode == 200 {
				b2LoseResponse(r)
			}
			return nil
		}})
		_, err = b.retry(t, "submit-response-lost")
		if !errors.Is(err, syncclient.ErrEnrollmentPending) {
			t.Fatal("uncertain original owner lost", err)
		}
		before := b.slot.read()
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "GET" && strings.Contains(r.Request.URL.Path, "/recovery-authority-transitions-v2/") && r.StatusCode == 200 {
				b.slot.mu.Lock()
				b.slot.fail = true
				b.slot.mu.Unlock()
			}
			return nil
		}})
		_, err = b.retry(t, "accepted-query-save-fails")
		if !errors.Is(err, mobileworkflow.ErrDAGPersistence) || !bytes.Equal(before, b.slot.read()) {
			t.Fatal("accepted receipt hid failed CAS", err)
		}
		b.f.responseHook.Store(nil)
		b.slot.mu.Lock()
		b.slot.fail = false
		b.slot.mu.Unlock()
		w := b.open(t)
		b.routes.set("retired-owner-check")
		if _, err = w.RecoveryDAGOwnerInfo(context.Background(), b.registry, b.scope); !errors.Is(err, mobileworkflow.ErrDAGOwnerMissing) {
			t.Fatal("failed owner survived", err)
		}
		w.Close()
		w = b.open(t)
		b.routes.set("cold-current-code-original-query")
		result, err := w.QueryRecoveryDAGOriginal(context.Background(), []byte(code))
		w.Close()
		if err != nil || result.Observation != "accepted" || result.Confirmation != "original-verified-and-saved" || !result.RotationRequired || result.Pending.OperationID != pending.OperationID || !result.Pending.OriginalApplied || result.TrustedDevice {
			t.Fatal("cold query trusted a new session or replaced original", err)
		}
		b.assertRoutes(t, 1, 1)
	})
	t.Run("lost-challenge-same-id-retry-cold-prepared-interrupted", func(t *testing.T) {
		b := b2New(t)
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovery-authority-challenges-v2") && r.StatusCode == 200 {
				b2LoseResponse(r)
			}
			return nil
		}})
		code, err := b.begin(t, "challenge-response-lost")
		if !errors.Is(err, syncclient.ErrDAGPreparationPending) || code != "" {
			t.Fatal("lost challenge did not preserve intent", err)
		}
		intent := b.preparation(t)
		if intent.Phase != "intent" {
			t.Fatal("unexpected original stage")
		}
		b.f.responseHook.Store(nil)
		code, err = b.begin(t, "same-intent-challenge-retry")
		if err != nil || code == "" {
			t.Fatal("same original retry", err)
		}
		prepared := b.preparation(t)
		if !syncclient.DAGPreparationSameIntent(intent, prepared) {
			t.Fatal("retry rewrote original intent")
		}
		before := b.slot.read()
		b.registry.Clear()
		w := b.open(t)
		b.routes.set("cold-prepared-blocked")
		if _, err = w.OpenDAGRecoveryOwner(context.Background(), b.registry, b.scope, []byte(b.f.code)); !errors.Is(err, mobileworkflow.ErrDAGPreparationInterrupted) {
			t.Fatal("cold prepared became new original owner", err)
		}
		if _, err = w.QueryRecoveryDAGOriginal(context.Background(), []byte(b.f.code)); !errors.Is(err, mobileworkflow.ErrDAGPreparationInterrupted) {
			t.Fatal("prepare presented as queryable signed packet", err)
		}
		meta, err := w.RecoveryDAGPreparationInfo()
		w.Close()
		if err != nil || meta.OperationID != intent.OperationID || meta.Phase != "prepared" || !meta.NeedsOriginalOwner || !bytes.Equal(before, b.slot.read()) {
			t.Fatal("cold preparation erased or guessed expired", err)
		}
		b.assertRoutes(t, 2, 0)
	})
}

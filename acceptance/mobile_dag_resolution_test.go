package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// 原生AES测试适配器+真实HTTPS/SQLite；每步重新构造Workflow，不手造原包或接受材料。
func resolutionSealedMobile(t *testing.T) (*b2Mobile, string, mobileworkflow.RecoveryDAGPendingInfo) {
	t.Helper()
	f := newMobileManagerFixture(t)
	a := newMobileManagerActor(t, f)
	initial := environmentValue(a.workflow.ExportProtectedState())
	if e := a.config.SaveProtectedState(initial); e != nil {
		t.Fatal(e)
	}
	clear(initial)
	a.workflow.Close()
	slot := &s2aNativeState{save: a.config.SaveProtectedState, load: a.load}
	routes := &s2aRoutes{phase: "scope-login", counts: map[string]map[string]int{}}
	c := a.config
	c.HTTPClient = routes.client(f.proxy.Client())
	c.SaveProtectedState = func([]byte) error { return errors.New("closure requires native whole-state CAS") }
	c.SaveProtectedStateCAS = slot.cas
	c.CheckProtectedState = slot.check
	scope := mobileworkflow.DAGOwnerScope{Namespace: "synthetic-closure", Slot: "transition", PlatformEpoch: 1}
	r := environmentValue(mobileworkflow.NewDAGRecoveryRegistry(scope))
	t.Cleanup(r.Close)
	b := &b2Mobile{f: f, config: c, slot: slot, routes: routes, registry: r, scope: scope}
	w := b.open(t)
	if out, e := w.LoginDAGAccountScope(context.Background(), f.email, f.password); e != nil || out.TrustedDevice {
		t.Fatal("JIT account scope", e)
	}
	w.Close()
	w = b.open(t)
	var e error
	b.opened, e = w.OpenDAGRecoveryOwner(context.Background(), r, scope, []byte(f.code))
	w.Close()
	if e != nil {
		t.Fatal(e)
	}
	code, e := b.begin(t, "original-prepare")
	if e != nil {
		t.Fatal(e)
	}
	sealed, e := b.seal(t, code)
	if e != nil {
		t.Fatal(e)
	}
	return b, code, sealed
}
func resolutionOriginal(t *testing.T, b *b2Mobile) json.RawMessage {
	t.Helper()
	var v struct {
		Original *struct {
			Journal json.RawMessage `json:"journal"`
		} `json:"recoveryDAG"`
	}
	if json.Unmarshal(b.slot.read(), &v) != nil || v.Original == nil {
		t.Fatal("original missing")
	}
	return bytes.Clone(v.Original.Journal)
}
func resolutionClose(t *testing.T, b *b2Mobile, code, hash string) (mobileworkflow.RecoveryDAGResolutionResult, error) {
	t.Helper()
	w := b.open(t)
	defer w.Close()
	input := []byte(code)
	out, e := w.CloseDAGOperationOriginal(context.Background(), b.registry, b.scope, input, hash)
	if !bytes.Equal(input, make([]byte, len(input))) {
		t.Fatal("code retained")
	}
	return out, e
}
func resolutionQuery(t *testing.T, b *b2Mobile, code string) (mobileworkflow.RecoveryDAGResolutionResult, error) {
	t.Helper()
	w := b.open(t)
	defer w.Close()
	return w.QueryDAGOperationResolution(context.Background(), b.registry, b.scope, []byte(code))
}
func resolutionAssertMetadata(t *testing.T, out mobileworkflow.RecoveryDAGResolutionResult) {
	t.Helper()
	if out.TrustedDevice || !out.RotationRequired {
		t.Fatal("closure promoted trust")
	}
}
func TestMobileDAGOperationResolutionHTTPS(t *testing.T) {
	t.Run("closed-unknown-CAS-ack-loss-explicit-new-original", func(t *testing.T) {
		b, _, sealed := resolutionSealedMobile(t)
		w := b.open(t)
		info, e := w.RecoveryDAGResolutionInfo()
		w.Close()
		if e != nil || info.OperationID != sealed.OperationID {
			t.Fatal("immutable target", e)
		}
		original := resolutionOriginal(t, b)
		out, e := resolutionQuery(t, b, b.f.code)
		if e != nil || out.Observation != "pending" || out.LocalState != "pending" || !bytes.Equal(original, resolutionOriginal(t, b)) {
			t.Fatal("pending cleared original", e)
		}
		resolutionAssertMetadata(t, out)
		// CAS写前屏障失败：不得取得fresh证明或发送closure POST。
		var auths, posts atomic.Int32
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if strings.HasSuffix(r.Request.URL.Path, "/recovery-sessions") {
				auths.Add(1)
			}
			if strings.HasSuffix(r.Request.URL.Path, "/recovery-operation-resolutions-v1") {
				posts.Add(1)
			}
			return nil
		}})
		b.slot.mu.Lock()
		b.slot.fail = true
		b.slot.mu.Unlock()
		out, e = resolutionClose(t, b, b.f.code, info.TargetHash)
		if !errors.Is(e, mobileworkflow.ErrDAGPersistence) || auths.Load() != 0 || posts.Load() != 0 || out.LocalState == "closed" {
			t.Fatal("native barrier failed open", e)
		}
		b.slot.mu.Lock()
		b.slot.fail = false
		b.slot.mu.Unlock()
		// 取消只是本地未知，不能把服务器关闭观察提前提交为本机完成。
		ctx, cancel := context.WithCancel(context.Background())
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if strings.HasSuffix(r.Request.URL.Path, "/recovery-operation-resolutions-v1") && r.StatusCode == 200 {
				cancel()
			}
			return nil
		}})
		w = b.open(t)
		out, e = w.CloseDAGOperationOriginal(ctx, b.registry, b.scope, []byte(b.f.code), info.TargetHash)
		w.Close()
		cancel()
		if e == nil || out.LocalState == "closed" || !bytes.Equal(original, resolutionOriginal(t, b)) {
			t.Fatal("canceled response cleared original", e)
		}
		// server已原子close，但200被代理丢掉。当次unknown，原包保留；冷同ID查询才完成CAS。
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if strings.HasSuffix(r.Request.URL.Path, "/recovery-operation-resolutions-v1") && r.StatusCode == 200 {
				b2LoseResponse(r)
			}
			return nil
		}})
		out, e = resolutionClose(t, b, b.f.code, info.TargetHash)
		if e == nil || out.LocalState == "closed" || !bytes.Equal(original, resolutionOriginal(t, b)) {
			t.Fatal("lost server response cleared original", e)
		}
		b.f.responseHook.Store(nil)
		// native实际CAS已写入，但回应丢失：当次失败；真实冷读才能得知closed。
		c := b.config
		c.ProtectedState = b.slot.read()
		c.SaveProtectedStateCAS = func(expected string, next []byte) error {
			if e := b.slot.cas(expected, next); e != nil {
				return e
			}
			var v struct {
				Resolution *struct {
					Pending json.RawMessage   `json:"pending"`
					Closed  []json.RawMessage `json:"closed"`
				} `json:"recoveryDAGResolution"`
			}
			_ = json.Unmarshal(next, &v)
			if v.Resolution != nil && len(v.Resolution.Closed) > 0 {
				return errors.New("synthetic CAS acknowledgement lost")
			}
			return nil
		}
		w = environmentValue(mobileworkflow.New(c))
		out, e = w.QueryDAGOperationResolution(context.Background(), b.registry, b.scope, []byte(b.f.code))
		w.Close()
		if !errors.Is(e, mobileworkflow.ErrDAGPersistence) || out.LocalState == "closed" {
			t.Fatal("lost native acknowledgement falsely completed", e)
		}
		w = b.open(t)
		closed, e := w.RecoveryDAGResolutionInfo()
		w.Close()
		if e != nil || closed.LocalState != "closed" || closed.OperationID != sealed.OperationID {
			t.Fatal("cold actual closed state", e)
		}
		out, e = resolutionQuery(t, b, b.f.code)
		if e != nil || out.LocalState != "closed" || out.Sequence != closed.Sequence {
			t.Fatal("same closed query", e)
		}
		resolutionAssertMetadata(t, out)
		w = b.open(t)
		if _, e = w.OpenDAGRecoveryOwner(context.Background(), b.registry, b.scope, []byte(b.f.code)); !errors.Is(e, mobileworkflow.ErrDAGResolutionPending) {
			t.Fatal("implicit reopening", e)
		}
		reopened, e := w.BeginDAGRecoveryAfterClosure(context.Background(), b.registry, b.scope, []byte(b.f.code))
		w.Close()
		if e != nil || reopened.Sequence < closed.Sequence || reopened.TrustedDevice {
			t.Fatal("explicit full verified owner", e)
		}
		next, e := b.begin(t, "explicit-new-prepare")
		if e != nil {
			t.Fatal(e)
		}
		newsealed, e := b.seal(t, next)
		if e != nil || newsealed.OperationID == sealed.OperationID {
			t.Fatal("retired ID reused", e)
		}
		finished, e := b.retry(t, "explicit-new-submit")
		if e != nil || finished.TrustedDevice {
			t.Fatal("new original did not complete", e)
		}
		w = b.open(t)
		pending, e := w.RecoveryDAGPendingInfo()
		w.Close()
		if e != nil || !pending.OriginalApplied || pending.OperationID != newsealed.OperationID {
			t.Fatal("new exact history", e)
		}
		var protected map[string]json.RawMessage
		_ = json.Unmarshal(b.slot.read(), &protected)
		if len(protected["recoveryDAGResolution"]) == 0 {
			t.Fatal("new operation erased closed receipt")
		}
	})
	t.Run("accepted-lost-response-fresh-code-confirms-original", func(t *testing.T) {
		b, code, sealed := resolutionSealedMobile(t)
		w := b.open(t)
		info, e := w.RecoveryDAGResolutionInfo()
		w.Close()
		if e != nil {
			t.Fatal(e)
		}
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovery-authority-transitions-v2") && r.StatusCode == 200 {
				b2LoseResponse(r)
			}
			return nil
		}})
		_, e = b.retry(t, "accepted-response-loss")
		if e == nil {
			t.Fatal("response loss not observed")
		}
		b.f.responseHook.Store(nil)
		before := resolutionOriginal(t, b)
		invalid, e := resolutionQuery(t, b, b.f.code)
		if e == nil || invalid.LocalState == "closed" || !bytes.Equal(before, resolutionOriginal(t, b)) {
			t.Fatal("old code authorized current terminal resolution", e)
		}
		out, e := resolutionQuery(t, b, code)
		if e != nil || out.Observation != "accepted" || out.LocalState != "accepted-original-confirmed" || out.OperationID != sealed.OperationID {
			t.Fatal("fresh accepted original confirmation", e)
		}
		resolutionAssertMetadata(t, out)
		after := resolutionOriginal(t, b)
		var a, z struct {
			Operation syncclient.ProtectedDAGOperation `json:"operation"`
		}
		_ = json.Unmarshal(before, &a)
		_ = json.Unmarshal(after, &z)
		if !z.Operation.Applied || z.Operation.AcceptedSequence != out.Sequence || z.Operation.ContentHash != a.Operation.ContentHash || z.Operation.OperationID != a.Operation.OperationID {
			t.Fatal("accepted did not confirm immutable packet")
		}
		// 关闭同一accepted原ID只重复原收据，不能生成closed或新rotation。
		repeated, e := resolutionClose(t, b, code, info.TargetHash)
		if e != nil || repeated.Observation != "accepted" || repeated.Sequence != out.Sequence || repeated.LocalState != "accepted-original-confirmed" {
			t.Fatal("accepted overwritten", e)
		}
	})
	t.Run("auth-only-closed-query-after-dag-transition", func(t *testing.T) {
		b, _, _ := resolutionSealedMobile(t)
		w := b.open(t)
		info, e := w.RecoveryDAGResolutionInfo()
		w.Close()
		if e != nil {
			t.Fatal(e)
		}
		closed, e := resolutionClose(t, b, b.f.code, info.TargetHash)
		if e != nil || closed.LocalState != "closed" {
			t.Fatal("close before independent legacy rotation", e)
		}
		var scopeAccount struct {
			AccountID string `json:"accountId"`
		}
		originMust(t, json.Unmarshal(b.f.rootActor.load(), &scopeAccount))
		_, current := recoverDAGActor(t, context.Background(), b.f, scopeAccount.AccountID, b.f.code, "independent-dag-after-close", false)

		var vaultReads atomic.Int32
		b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if strings.HasSuffix(r.Request.URL.Path, "/recovery-vault-v2") {
				vaultReads.Add(1)
			}
			return nil
		}})
		out, e := resolutionQuery(t, b, current)
		if e != nil || out.LocalState != "closed" || out.Sequence != closed.Sequence || vaultReads.Load() != 0 {
			t.Fatal("auth-only terminal depended on missing DAG chain", e)
		}
		w = b.open(t)
		reopened, e := w.BeginDAGRecoveryAfterClosure(context.Background(), b.registry, b.scope, []byte(current))
		w.Close()
		if e != nil || reopened.TrustedDevice || vaultReads.Load() != 1 {
			t.Fatal("fresh DAG recovery after closed history failed", e)
		}
		b.f.responseHook.Store(nil)
	})

}

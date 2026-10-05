package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 独立B3b只替换初始账号scope保存入口；原B1/B2/B3a helper与断言不变。
func b3bRotated(t *testing.T) *b2Mobile {
	t.Helper()
	f := newMobileManagerFixture(t)
	a := newMobileManagerActor(t, f)
	// 原生初始空槽保存，只含未登录device/endpoint；不是手写登录scope或接受材料。
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
	c.SaveProtectedState = func([]byte) error { return errors.New("B3b must use native whole-state CAS") }
	c.SaveProtectedStateCAS = slot.cas
	c.CheckProtectedState = slot.check
	scope := mobileworkflow.DAGOwnerScope{Namespace: "synthetic-b3b", Slot: "recovered-device", PlatformEpoch: 1}
	r := environmentValue(mobileworkflow.NewDAGRecoveryRegistry(scope))
	t.Cleanup(r.Close)
	b := &b2Mobile{f: f, routes: routes, config: c, slot: slot, registry: r, scope: scope}
	w := b.open(t)
	info, e := w.LoginDAGAccountScope(context.Background(), f.email, f.password)
	w.Close()
	if e != nil || info.TrustedDevice {
		t.Fatal("JIT persisted scope", e)
	}
	w = b.open(t)
	b.opened, e = w.OpenDAGRecoveryOwner(context.Background(), r, scope, []byte(f.code))
	w.Close()
	if e != nil {
		t.Fatal("B1 cold account scope", e)
	}
	code, e := b.begin(t, "transition-challenge")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.seal(t, code); e != nil {
		t.Fatal(e)
	}
	if _, e = b.retry(t, "transition-submit"); e != nil {
		t.Fatal(e)
	}
	return b
}
func b3bOriginalBytes(t *testing.T, raw []byte) json.RawMessage {
	t.Helper()
	var s struct {
		Pending *struct {
			Journal json.RawMessage `json:"journal"`
		} `json:"recoveryDAG"`
		Source *struct {
			Original struct {
				Journal json.RawMessage `json:"journal"`
			} `json:"original"`
		} `json:"recoveredDAGDevice"`
	}
	if json.Unmarshal(raw, &s) != nil {
		t.Fatal("state")
	}
	if s.Pending != nil {
		return s.Pending.Journal
	}
	if s.Source != nil {
		return s.Source.Original.Journal
	}
	t.Fatal("original missing")
	return nil
}
func TestMobileDAGRecoveredApplyHTTPS(t *testing.T) {
	b := b3bRotated(t)
	in := b3Selection(t, b, "admin")
	sealed, e := b3Seal(t, b, in, "device-seal")
	if e != nil {
		t.Fatal(e)
	}
	original, e := b3Retry(t, b, "device-confirm")
	if e != nil || !original.OriginalConfirmed || original.TrustedDevice {
		t.Fatal("B3a prerequisite", e)
	}
	before := b.slot.read()
	journal := bytes.Clone(b3bOriginalBytes(t, before))
	var op struct {
		Operation syncclient.ProtectedDAGOperation `json:"operation"`
	}
	if json.Unmarshal(journal, &op) != nil {
		t.Fatal("original journal")
	}
	var boots, pulls atomic.Int32
	b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if strings.HasSuffix(r.Request.URL.Path, "/boot-sessions") && r.StatusCode == 200 {
			boots.Add(1)
		}
		if strings.HasSuffix(r.Request.URL.Path, "/pull") && r.StatusCode == 200 {
			pulls.Add(1)
			_ = r.Body.Close()
			r.StatusCode = 504
			r.Body = io.NopCloser(strings.NewReader(`{"error":"synthetic_response_lost"}`))
			r.ContentLength = -1
			r.Header.Del("Content-Length")
		}
		return nil
	}})
	w := b.open(t)
	out, e := w.ApplyDAGRecoveredDevice(context.Background(), b.registry, b.scope)
	if !errors.Is(e, syncclient.ErrAcceptedNotApplied) || out.TrustedDevice || !bytes.Equal(before, b.slot.read()) || boots.Load() != 1 || pulls.Load() != 1 {
		t.Fatal("Boot success/Pull unknown promoted", e)
	}
	w.Close()
	b.f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if strings.HasSuffix(r.Request.URL.Path, "/boot-sessions") && r.StatusCode == 200 {
			boots.Add(1)
		}
		if strings.HasSuffix(r.Request.URL.Path, "/pull") && r.StatusCode == 200 {
			pulls.Add(1)
			b.slot.mu.Lock()
			b.slot.fail = true
			b.slot.mu.Unlock()
		}
		return nil
	}})
	w = b.open(t)
	out, e = w.ApplyDAGRecoveredDevice(context.Background(), nil, mobileworkflow.DAGOwnerScope{})
	if !errors.Is(e, mobileworkflow.ErrDAGPersistence) || out.TrustedDevice || !bytes.Equal(before, b.slot.read()) {
		t.Fatal("final CAS failure promoted", e)
	}
	w.Close()
	b.slot.mu.Lock()
	b.slot.fail = false
	b.slot.mu.Unlock()
	b.f.responseHook.Store(nil)
	// native CAS实际写入却回应丢失：当次仍返回false/失败；冷读取实际持久来源才能恢复。
	lostConfig := b.config
	lostConfig.ProtectedState = b.slot.read()
	lostConfig.SaveProtectedStateCAS = func(expected string, next []byte) error {
		if e := b.slot.cas(expected, next); e != nil {
			return e
		}
		return errors.New("synthetic native CAS acknowledgement lost")
	}
	w = environmentValue(mobileworkflow.New(lostConfig))
	out, e = w.ApplyDAGRecoveredDevice(context.Background(), nil, mobileworkflow.DAGOwnerScope{})
	if !errors.Is(e, mobileworkflow.ErrDAGPersistence) || out.TrustedDevice || bytes.Equal(before, b.slot.read()) {
		t.Fatal("lost native acknowledgement not unknown", e)
	}
	w.Close()
	w = b.open(t)
	recovered, e := w.RestoreDAGRecoveredDevice()
	if e != nil || !recovered.TrustedDevice {
		t.Fatal("cold actual committed source", e)
	}
	w.Close()
	w = b.open(t)
	out, e = w.ApplyDAGRecoveredDevice(context.Background(), nil, mobileworkflow.DAGOwnerScope{})
	if e != nil || !out.TrustedDevice || out.OperationID != sealed.OperationID || out.ContentHash != op.Operation.ContentHash || out.AcceptedSequence != original.AcceptedSequence || len(out.View.Environments) != 1 || out.View.Environments[0].Variables["SYNTHETIC_X"] != "synthetic-x-value" {
		t.Fatal("formal Boot/P4 Pull/native CAS", e)
	}
	if !bytes.Equal(journal, b3bOriginalBytes(t, b.slot.read())) {
		t.Fatal("original receipt changed")
	}
	if _, e = w.View(); !errors.Is(e, mobileworkflow.ErrRecoveryRestricted) {
		t.Fatal("legacy View parser entered", e)
	}
	if _, e = w.SetVariable(context.Background(), b.f.initial, "SYNTHETIC_X", "synthetic-other", "blocked-write"); !errors.Is(e, mobileworkflow.ErrRecoveryRestricted) {
		t.Fatal("legacy write entered", e)
	}
	w.Close()
	w = b.open(t)
	restored, e := w.RestoreDAGRecoveredDevice()
	if e != nil || !restored.TrustedDevice || restored.OperationID != out.OperationID {
		t.Fatal("cold source/ledger", e)
	}
	fresh, e := w.PullDAGRecoveredDevice(context.Background())
	if e != nil || !fresh.TrustedDevice || fresh.View.Checkpoint < out.View.Checkpoint {
		t.Fatal("fresh current authority", e)
	}
	w.Close()
	// 冷来源原双钥/收据保持；回退data checkpoint必须拒绝，不能只验证graph。
	good := b.slot.read()
	var bad map[string]json.RawMessage
	_ = json.Unmarshal(good, &bad)
	var cloud map[string]json.RawMessage
	_ = json.Unmarshal(bad["cloud"], &cloud)
	var snapshot map[string]json.RawMessage
	_ = json.Unmarshal(cloud["cloud"], &snapshot)
	snapshot["sequence"] = json.RawMessage(strconv.FormatUint(out.AcceptedSequence-1, 10))
	cloud["cloud"], _ = json.Marshal(snapshot)
	bad["cloud"], _ = json.Marshal(cloud)
	tampered, _ := json.Marshal(bad)
	b.slot.mu.Lock()
	if e = b.slot.save(tampered); e != nil {
		t.Fatal(e)
	}
	b.slot.mu.Unlock()
	c := b.config
	c.ProtectedState = tampered
	if v, e := mobileworkflow.New(c); e == nil {
		v.Close()
		t.Fatal("source checkpoint rollback accepted")
	}
	b.slot.mu.Lock()
	if e = b.slot.save(good); e != nil {
		t.Fatal(e)
	}
	b.slot.mu.Unlock()
	// 已核来源当前expiry离线到点必须清相关明文，保存仍只用CAS。
	c = b.config
	c.ProtectedState = b.slot.read()
	deadline, _ := strconv.ParseInt(in.SelectedRights[0].ExpiresAt, 10, 64)
	c.Now = func() time.Time { return time.Unix(deadline+1, 0) }
	w, e = mobileworkflow.New(c)
	if e != nil {
		t.Fatal("timed reopen", e)
	}
	expired, e := w.RestoreDAGRecoveredDevice()
	if e != nil || len(expired.View.Environments) != 0 {
		t.Fatal("offline expiry", e)
	}
	w.Close()
	if bytes.Contains(b.slot.read(), []byte("synthetic-x-value")) {
		t.Fatal("expired plaintext retained")
	}
	// 后续旧墙时钟也不能从已清缓存复活。这里只观察已持久状态，不改provider。
	w = b.open(t)
	cold, e := w.RestoreDAGRecoveredDevice()
	if e != nil || len(cold.View.Environments) != 0 {
		t.Fatal("cold expired cache revived", e)
	}
	w.Close()
	// 同已Boot验证的真实Admin设备在测试管理控制器中签none；正式B3b随后必须因当前授权而清来源。
	// 源仅从此前本机已保护good和完整原包重建，不使用server directory/未签快照。
	var current struct {
		Endpoint  string           `json:"endpoint"`
		AccountID string           `json:"accountId"`
		DeviceID  string           `json:"deviceId"`
		Cloud     localstate.State `json:"cloud"`
	}
	if json.Unmarshal(good, &current) != nil {
		t.Fatal("trusted test source")
	}
	result := environmentValue(syncclient.RecoveredDAGResultFromConfirmedOperation(op.Operation))
	verifier := environmentValue(syncclient.NewRecoveredDAGPinnedVerifier(syncclient.RecoveredDAGPinnedTrust{Trust: syncclient.PinnedTrust{AccountID: current.AccountID, AccountGeneration: 1, DeviceID: current.DeviceID, DeviceSigningPublicKey: b.config.SigningKey.Public().(ed25519.PublicKey), ReceivingPrivateKey: b.config.ReceivingPrivateKey}, Pin: result.Pin, Accepted: result.Accepted, Evidence: result.Evidence}))
	defer verifier.Close()
	engine := environmentValue(localstate.New(&originMemoryStore{state: current.Cloud}))
	client := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: current.Endpoint, ProtocolMajor: 2, HTTPClient: b.f.proxy.Client(), AccountID: current.AccountID, AccountGeneration: 1, DeviceID: current.DeviceID, Verifier: verifier, Engine: engine}))
	client = environmentValue(client.BootDevice(context.Background(), b.config.SigningKey))
	_ = environmentValue(client.Pull(context.Background()))
	tx := environmentValue(client.PrepareGrantUpdate(context.Background(), syncclient.GrantUpdateIntent{ID: "b3b-explicit-own-revoke", EnvironmentID: b.f.initial, SubjectDeviceID: current.DeviceID, Role: "none"}, b.config.SigningKey))
	packet := environmentValue(tx.ProtectedBytes())
	saveManagement, loadManagement := recoveryNativeSeal(t, "synthetic-b3b-revoke-native")
	accepted := environmentValue(tx.SubmitWithBarrier(context.Background(), func() error { return saveManagement(packet) }))
	if accepted.Sequence <= out.View.Checkpoint || !bytes.Equal(loadManagement(), packet) {
		t.Fatal("revoke original barrier/receipt")
	}
	clear(packet)
	w = b.open(t)
	revoked, e := w.PullDAGRecoveredDevice(context.Background())
	if !errors.Is(e, syncclient.ErrTrustInvalidated) || revoked.TrustedDevice {
		t.Fatal("current no-grant bypassed", e)
	}
	w.Close()
	var closed struct {
		Cloud  localstate.State `json:"cloud"`
		Root   json.RawMessage  `json:"root"`
		Source json.RawMessage  `json:"recoveredDAGDevice"`
	}
	if json.Unmarshal(b.slot.read(), &closed) != nil || !closed.Cloud.AccountClosed || len(closed.Cloud.Cloud.Environments) != 0 || len(closed.Root) != 0 || len(closed.Source) != 0 {
		t.Fatal("revoked scope retained")
	}
	w = b.open(t)
	if _, e = w.RestoreDAGRecoveredDevice(); !errors.Is(e, mobileworkflow.ErrNotTrusted) {
		t.Fatal("revoked cold source revived", e)
	}
	w.Close()
	t.Log("B3B actual HTTPS: persisted JIT scope -> B1/B2/B3a -> Boot/P4 Pull -> final CAS -> cold source -> expiry -> current none/clear; original packet unchanged; failed Pull/CAS remained restricted")
}

func TestRecoveredDAGMobileApprovesDeviceAfterColdRestartHTTPS(t *testing.T) {
	b := b3bRotated(t)
	_, err := b3Seal(t, b, b3Selection(t, b, "admin"), "approval-device-seal")
	originMust(t, err)
	_, err = b3Retry(t, b, "approval-device-confirm")
	originMust(t, err)
	manager := b.open(t)
	_, err = manager.ApplyDAGRecoveredDevice(context.Background(), b.registry, b.scope)
	originMust(t, err)
	manager.Close()
	manager = b.open(t)
	child := newMobileManagerActor(t, b.f)
	var binding struct {
		DeviceID string `json:"deviceId"`
	}
	originMust(t, json.Unmarshal(b.slot.read(), &binding))
	view, err := b.f.enrollWithManagerID(t, manager, child, "recovered-dag-approves-child", []mobileworkflow.ApprovalSelection{{EnvironmentID: b.f.initial, Role: "ro", ExpiresAt: strconv.FormatInt(time.Now().Add(5*time.Minute).Unix(), 10)}}, binding.DeviceID)
	originMust(t, err)
	if len(view.Environments) != 1 || view.Environments[0].Role != "RO" {
		t.Fatal("recovered approval did not grant selected access", view)
	}
	manager.Close()
	manager = b.open(t)
	defer manager.Close()
	result, err := manager.RetryApprovalV5(context.Background(), "recovered-dag-approves-child")
	if err != nil || result.State != "complete" {
		t.Fatal("cold approval did not confirm original", result, err)
	}
}

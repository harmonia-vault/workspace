package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/harmonia-vault/core-go/mobileworkflow"
)

func TestMobileDAGOwnerTwoWorkflowHTTPS(t *testing.T) {
	// 唯一实际场景：真实账号初始化属既有 helper 的准备阶段；B1 本身不做业务写入。
	f := newMobileManagerFixture(t)
	a := newMobileManagerActor(t, f)
	if err := a.workflow.Login(context.Background(), f.email, f.password); err != nil {
		t.Fatal(err)
	}
	// 现 Login 只更新 Go 内存；模拟既有 native 调用后的完整 Export+seal。
	prepared := environmentValue(a.workflow.ExportProtectedState())
	if err := a.config.SaveProtectedState(prepared); err != nil {
		clear(prepared)
		t.Fatal(err)
	}
	clear(prepared)
	a.workflow.Close()
	slot := &s2aNativeState{save: a.config.SaveProtectedState, load: a.load}
	before := slot.read()
	routes := &s2aRoutes{phase: "owner-open", counts: map[string]map[string]int{}}
	c := a.config
	c.HTTPClient = routes.client(f.proxy.Client())
	c.ProtectedState = slot.read()
	c.SaveProtectedState = func([]byte) error { return errors.New("B1 readonly cannot ordinary Save") }
	c.SaveProtectedStateCAS = slot.cas
	c.CheckProtectedState = slot.check
	first := environmentValue(mobileworkflow.New(c))
	defer first.Close()
	scope := mobileworkflow.DAGOwnerScope{Namespace: "synthetic-go-native", Slot: "b1-readonly", PlatformEpoch: 23}
	registry := environmentValue(mobileworkflow.NewDAGRecoveryRegistry(scope))
	defer registry.Close()
	code := []byte(f.code)
	opened, err := first.OpenDAGRecoveryOwner(context.Background(), registry, scope, code)
	if err != nil || !opened.RotationRequired || opened.TrustedDevice || !bytes.Equal(code, make([]byte, len(code))) {
		t.Fatal("real restricted owner open failed", err)
	}
	first.Close()
	c.ProtectedState = slot.read()
	second := environmentValue(mobileworkflow.New(c))
	defer second.Close()
	routes.set("second-workflow-info")
	next, err := second.RecoveryDAGOwnerInfo(context.Background(), registry, scope)
	if err != nil || next != opened || !next.RotationRequired || next.TrustedDevice {
		t.Fatal("same RAM session did not survive detached first Workflow", err)
	}
	if !bytes.Equal(before, slot.read()) || slot.commits != 0 {
		t.Fatal("B1 readonly changed protected state")
	}
	registry.Clear()
	if _, err = second.RecoveryDAGOwnerInfo(context.Background(), registry, scope); err == nil {
		t.Fatal("cleared owner survived")
	}
	routes.mu.Lock()
	defer routes.mu.Unlock()
	want := map[string]int{"GET protocol-info": 1, "POST recovery-challenges": 1, "POST recovery-sessions": 1, "GET recovery-vault-v2": 1}
	if len(routes.counts) != 1 || len(routes.counts["owner-open"]) != len(want) {
		t.Fatal("unexpected B1 request phases")
	}
	for k, n := range want {
		if routes.counts["owner-open"][k] != n {
			t.Fatal("unexpected B1 route count", k)
		}
	}
	raw, _ := json.Marshal(routes.counts)
	t.Log("B1_ROUTE_COUNTS", string(raw))
}

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/mobilebridge"
	"github.com/harmonia-vault/core-go/syncclient"
)

type dagBusinessBridgeSlot struct {
	b3bBridgeSlot
	rejects atomic.Int32
}

func (s *dagBusinessBridgeSlot) CompareAndSwapSealed(old, next []byte) error {
	if s.fail.Load() {
		s.rejects.Add(1)
		return errors.New("synthetic protected CAS rejection")
	}
	return s.b3bBridgeSlot.CompareAndSwapSealed(old, next)
}

// 真实HTTPS/TS/SQLite/HPKE业务与AES密文CAS Go适配器；不是Android认证/JNI/Flutter证据。
func TestNativeBridgeDAGBusinessHTTPS(t *testing.T) {
	f := newMobileManagerFixture(t)
	added := environmentValue(f.root.CreateEnvironment(context.Background(), "合成只读环境Y", "business-y-create"))
	var roEnv string
	for _, env := range added.Environments {
		if env.ID != f.initial {
			roEnv = env.ID
		}
	}
	if roEnv == "" {
		t.Fatal("readonly source missing")
	}
	if _, e := f.root.SetVariable(context.Background(), roEnv, "SYNTHETIC_Y", "synthetic-y-value", "business-y-put"); e != nil {
		t.Fatal(e)
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.proxy.Certificate().Raw})
	d := environmentValue(mobilebridge.NewDevice())
	defer d.Close()
	r := environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.dag.business", "workflow", 91))
	defer r.Close()
	slot := &dagBusinessBridgeSlot{}
	open := func() *mobilebridge.VaultWorkflow {
		captured := slot.read()
		defer clear(captured)
		v, e := d.OpenAtomicWorkflow(f.proxy.URL, "synthetic.dag.business\x00harmonia/workflow-state/v1\x00workflow", captured, ca, slot)
		if e != nil {
			t.Fatal("fresh authenticated adapter", e)
		}
		if e = v.AttachDAGRegistry(r); e != nil {
			v.Close()
			t.Fatal(e)
		}
		return v
	}
	command := func(op string, fields map[string]string) string {
		out := map[string]any{"version": 1, "endpoint": f.proxy.URL, "operation": op}
		for k, v := range fields {
			out[k] = v
		}
		raw, _ := json.Marshal(out)
		return string(raw)
	}
	recovery := func(op string, fields map[string]string, code []byte) map[string]any {
		v := open()
		defer v.Close()
		raw, e := v.ExecuteDAGRecovery(command(op, fields), code)
		if e != nil {
			t.Fatal("real recovery prerequisite", op, e)
		}
		var out map[string]any
		if json.Unmarshal([]byte(raw), &out) != nil || out["ok"] != true {
			t.Fatal("recovery result", op)
		}
		return out
	}
	recovery("openDAGRecoveryOwner", map[string]string{"email": f.email, "password": f.password}, []byte(f.code))
	code := recovery("beginDAGRecoveryTransition", nil, nil)["recoveryCode"].(string)
	recovery("sealDAGRecoveryTransition", nil, []byte(code))
	code = ""
	recovery("retryDAGRecoveryTransition", nil, nil)
	choices := recovery("dagRecoveredEnrollmentChoices", nil, nil)["data"].(map[string]any)
	selections := []map[string]string{}
	for _, raw := range choices["environments"].([]any) {
		env := raw.(map[string]any)
		id := env["environmentId"].(string)
		role := "rw"
		if id == roEnv {
			role = "ro"
		}
		selections = append(selections, map[string]string{"environmentId": id, "keyVersion": env["keyVersion"].(string), "role": role, "expiresAt": strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)})
	}
	selectionJSON, _ := json.Marshal(selections)
	sealed := recovery("sealDAGRecoveredDevice", map[string]string{"expectedSequence": choices["sequence"].(string), "recoveryHeadHash": choices["recoveryHeadHash"].(string), "selections": string(selectionJSON)}, nil)["data"].(map[string]any)
	original := map[string]string{"operationId": sealed["operationId"].(string), "contentHash": sealed["contentHash"].(string)}
	recovery("retryDAGRecoveredDevice", original, nil)
	applied := recovery("applyDAGRecoveredDevice", original, nil)
	if applied["trustedDevice"] != true {
		t.Fatal("not applied")
	}
	t.Log("已完成真实恢复/明确角色/正式Boot-P4Pull-CAS")
	var mutationPosts atomic.Int32
	var lose, failSave atomic.Bool
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(resp *http.Response) error {
		if resp.Request.Method == "POST" && strings.HasSuffix(resp.Request.URL.Path, "/mutations") {
			mutationPosts.Add(1)
			if resp.StatusCode == 200 && lose.Swap(false) {
				b2LoseResponse(resp)
			}
		}
		if strings.HasSuffix(resp.Request.URL.Path, "/pull") && resp.StatusCode == 200 && failSave.Swap(false) {
			slot.fail.Store(true)
		}
		return nil
	}})
	business := func(op string, fields map[string]string, value []byte) (map[string]any, error) {
		v := open()
		defer v.Close()
		raw, e := v.ExecuteDAGBusiness(command(op, fields), value)
		if !bytes.Equal(value, make([]byte, len(value))) {
			t.Fatal("input value retained")
		}
		if e != nil {
			if raw != "" {
				t.Fatal("hard failure leaked view")
			}
			return nil, e
		}
		var out map[string]any
		if json.Unmarshal([]byte(raw), &out) != nil {
			t.Fatal("business DTO")
		}
		return out, nil
	}
	fields := func(id, env, name string) map[string]string {
		return map[string]string{"requestId": id, "environmentId": env, "name": name}
	}
	checkValue := func(out map[string]any, name, value string) {
		if out["ok"] != true || out["trustedDevice"] != true {
			t.Fatal("write promoted incorrectly")
		}
		data := out["data"].(map[string]any)
		if data["write"].(map[string]any)["applied"] != true {
			t.Fatal("unapplied")
		}
		source := data["source"].(map[string]any)
		if source["operationId"] != original["operationId"] || source["contentHash"] != original["contentHash"] {
			t.Fatal("source changed")
		}
		for _, row := range source["view"].(map[string]any)["environments"].([]any) {
			env := row.(map[string]any)
			if env["id"] == f.initial {
				if env["variables"].(map[string]any)[name] != value {
					t.Fatal("verified value not received")
				}
				return
			}
		}
		t.Fatal("selected source missing")
	}
	checkUnknownSlot := func(row map[string]any) {
		seq, ok := row["sequences"].([]any)
		if !ok || len(seq) != 1 || seq[0] != "0" || row["accepted"] != float64(0) || row["applied"] != false {
			t.Fatal("actual original sequence slot mismatch")
		}
	}
	lose.Store(true)
	out, e := business("putDAGVariable", fields("dag-put-one", f.initial, "NEW_TOKEN"), []byte("SYNTHETIC_FIRST"))
	if e != nil || out["ok"] != false || out["trustedDevice"] != false || mutationPosts.Load() != 1 {
		t.Fatal("lost commit did not preserve original", e)
	}
	checkUnknownSlot(out["data"].(map[string]any)["original"].(map[string]any))
	out, e = business("pendingDAGWrites", nil, nil)
	if e != nil || len(out["data"].(map[string]any)["pending"].([]any)) != 1 || out["trustedDevice"] != false {
		t.Fatal("cold pending", e)
	}
	checkUnknownSlot(out["data"].(map[string]any)["pending"].([]any)[0].(map[string]any))
	out, e = business("retryDAGWrite", map[string]string{"requestId": "dag-put-one"}, nil)
	if e != nil || mutationPosts.Load() != 1 {
		t.Fatal("retry changed original packet", e)
	}
	checkValue(out, "NEW_TOKEN", "SYNTHETIC_FIRST")
	out, e = business("putDAGVariable", fields("dag-put-two", f.initial, "NEW_TOKEN"), []byte("SYNTHETIC_SECOND"))
	if e != nil {
		t.Fatal(e)
	}
	checkValue(out, "NEW_TOKEN", "SYNTHETIC_SECOND")
	out, e = business("retryDAGWrite", map[string]string{"requestId": "dag-put-one"}, nil)
	if e != nil || mutationPosts.Load() != 2 {
		t.Fatal("old receipt replayed write", e)
	}
	checkValue(out, "NEW_TOKEN", "SYNTHETIC_SECOND")
	_, e = business("putDAGVariable", fields("dag-put-one", f.initial, "NEW_TOKEN"), []byte("SYNTHETIC_CONFLICT"))
	if !errors.Is(e, syncclient.ErrWriteConflict) || mutationPosts.Load() != 2 {
		t.Fatal("same ID rebound")
	}
	_, e = business("putDAGVariable", fields("dag-ro-denied", roEnv, "READONLY_TOKEN"), []byte("SYNTHETIC_DENIED"))
	if !errors.Is(e, syncclient.ErrWritePermission) || mutationPosts.Load() != 2 {
		t.Fatal("RO submitted")
	}
	t.Log("已完成原ID丢回应冷续办/覆盖后历史receipt/输入冲突/RO拒写")
	out, e = business("deleteDAGVariable", fields("dag-delete", f.initial, "NEW_TOKEN"), nil)
	if e != nil || mutationPosts.Load() != 3 || out["ok"] != true {
		t.Fatal("delete", e)
	}
	for _, raw := range out["data"].(map[string]any)["source"].(map[string]any)["view"].(map[string]any)["environments"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == f.initial {
			if _, exists := row["variables"].(map[string]any)["NEW_TOKEN"]; exists {
				t.Fatal("delete not pulled")
			}
		}
	}
	t.Log("已完成共享删除经P4下发移除")
	// 真实native adapter拒绝保存：原签包业务POST之前必须封存，失败零POST。
	before := slot.read()
	failSave.Store(true)
	_, e = business("putDAGVariable", fields("dag-save-denied", f.initial, "NO_SAVE"), []byte("SYNTHETIC_DENIED"))
	if e == nil || mutationPosts.Load() != 3 || slot.rejects.Load() != 1 || !bytes.Equal(before, slot.read()) {
		t.Fatal("beforePOST CAS failed open")
	}
	t.Log("已完成native CAS拒绝且零mutation POST")
	// CAS失败已永久退休本次恢复registry；不能把旧RAM owner重新附给后续认证。
	slot.fail.Store(false)
	fresh := environmentValue(d.OpenAtomicWorkflow(f.proxy.URL, "synthetic.dag.business\x00harmonia/workflow-state/v1\x00workflow", slot.read(), ca, slot))
	defer fresh.Close()
	if e = fresh.AttachDAGRegistry(r); e == nil {
		t.Fatal("failed registry reused")
	}

}

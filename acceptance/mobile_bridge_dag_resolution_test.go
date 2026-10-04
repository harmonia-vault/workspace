package acceptance

import (
	"bytes"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/mobilebridge"
)

// 本片只有桥层原操作query/close，复用成熟真实初始化/B2签包，不预造accepted材料。
func TestNativeBridgeDAGResolutionHTTPS(t *testing.T) {
	f := newMobileManagerFixture(t)
	slot := &b3bBridgeSlot{}
	var requests, resolutionPosts, transitionPosts atomic.Int32
	var rejectClosedCAS atomic.Bool
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		requests.Add(1)
		if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovery-operation-resolutions-v1") {
			resolutionPosts.Add(1)
			if r.StatusCode == 200 && rejectClosedCAS.Swap(false) {
				slot.fail.Store(true)
			}
		}
		if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovery-authority-transitions-v2") {
			transitionPosts.Add(1)
		}
		return nil
	}})
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.proxy.Certificate().Raw})
	d := environmentValue(mobilebridge.NewDevice())
	defer d.Close()
	r := environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.closure.bridge", "workflow", 41))
	defer func() { r.Close() }()
	cmd := func(op string, fields map[string]string) string {
		data := map[string]any{"version": 1, "endpoint": f.proxy.URL, "operation": op}
		for k, v := range fields {
			data[k] = v
		}
		raw, _ := json.Marshal(data)
		return string(raw)
	}
	open := func(reg *mobilebridge.NativeDAGRegistry) *mobilebridge.VaultWorkflow {
		captured := slot.read()
		defer clear(captured)
		v, e := d.OpenAtomicWorkflow(f.proxy.URL, "synthetic.closure.bridge\x00harmonia/workflow-state/v1\x00workflow", captured, ca, slot)
		if e != nil {
			t.Fatal("captured closure workflow", e)
		}
		if e = v.AttachDAGRegistry(reg); e != nil {
			v.Close()
			t.Fatal("closure registry", e)
		}
		return v
	}
	dispatch := func(op string, fields map[string]string, code []byte) (map[string]any, error) {
		v := open(r)
		defer v.Close()
		raw, e := v.ExecuteDAGRecovery(cmd(op, fields), code)
		if !bytes.Equal(code, make([]byte, len(code))) {
			t.Fatal("caller complete code retained")
		}
		if e != nil {
			if raw != "" {
				t.Fatal("failure projected terminal metadata")
			}
			return nil, e
		}
		var data map[string]any
		if json.Unmarshal([]byte(raw), &data) != nil || data["profile"] != cryptox.RecoveryDAGCapability || data["trustedDevice"] != false || data["ok"] != true {
			t.Fatal("closure outer projection")
		}
		if op != "beginDAGRecoveryTransition" {
			for _, forbidden := range []string{"sessionToken", "privateKey", "signature", "recoveryCode", "envelope", "knownChallengeHash"} {
				if strings.Contains(raw, forbidden) {
					t.Fatal("private wire projected")
				}
			}
		}
		return data, nil
	}
	must := func(op string, fields map[string]string, code []byte) map[string]any {
		out, e := dispatch(op, fields, code)
		if e != nil {
			t.Fatal("closure stage", op, e)
		}
		return out
	}
	must("openDAGRecoveryOwner", map[string]string{"email": f.email, "password": f.password}, []byte(f.code))
	out := must("beginDAGRecoveryTransition", nil, nil)
	newCode := out["recoveryCode"].(string)
	must("sealDAGRecoveryTransition", nil, []byte(newCode))
	newCode = ""
	sealed := must("dagRecoveryPendingInfo", nil, nil)["data"].(map[string]any)
	if sealed["kind"] != "transition-v2" || sealed["acceptedSequence"] != "0" {
		t.Fatal("must use original unaccepted sealed transition")
	}
	info := must("dagRecoveryResolutionInfo", nil, nil)["data"].(map[string]any)
	id, hash := info["operationId"].(string), info["targetHash"].(string)
	fields := map[string]string{"operationId": id, "targetHash": hash}
	if id != sealed["operationId"] || info["localState"] != "pending" || len(hash) != 64 {
		t.Fatal("original target binding")
	}
	// 合法原signed包存在时错hash在任何HTTP前拒，不把invalid-code作为拒绝依据。
	before := requests.Load()
	wrong := environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.closure.bridge", "workflow", 42))
	v := open(wrong)
	raw, e := v.ExecuteDAGRecovery(cmd("closeDAGRecoveryOriginal", map[string]string{"operationId": id, "targetHash": strings.Repeat("0", 64)}), []byte(f.code))
	v.Close()
	wrong.Close()
	if e == nil || raw != "" || requests.Load() != before {
		t.Fatal("wrong original target reached network")
	}
	out = must("queryDAGRecoveryResolution", fields, []byte(f.code))
	data := out["data"].(map[string]any)
	if data["observation"] != "pending" || data["localState"] != "pending" || data["operationId"] != id || resolutionPosts.Load() != 1 || transitionPosts.Load() != 0 {
		t.Fatal("query guessed closure or submitted transition")
	}
	// server已closed、native最后CAS拒绝：不得返回closed DTO或丢原signed包。
	rejectClosedCAS.Store(true)
	out, e = dispatch("closeDAGRecoveryOriginal", fields, []byte(f.code))
	if e == nil || out != nil || resolutionPosts.Load() != 2 || transitionPosts.Load() != 0 {
		t.Fatal("closed CAS failure falsely completed")
	}
	slot.fail.Store(false)
	r.Close()
	r = environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.closure.bridge", "workflow", 43))
	cold := must("dagRecoveryPendingInfo", nil, nil)["data"].(map[string]any)
	if cold["operationId"] != id || cold["contentHash"] != sealed["contentHash"] || cold["acceptedSequence"] != "0" {
		t.Fatal("failed finalCAS lost original packet")
	}
	// Fresh当前码只查原ID获得不可变closed回执，再CAS保存墓碑与baseline；不生成新operation。
	out = must("queryDAGRecoveryResolution", fields, []byte(f.code))
	data = out["data"].(map[string]any)
	if data["observation"] != "closed" || data["localState"] != "closed" || data["confirmation"] != "native-confirmed" || data["operationId"] != id || data["targetHash"] != hash || data["sequence"] == "0" || resolutionPosts.Load() != 3 {
		t.Fatal("original closed receipt not durably restored")
	}
	closedSeq := data["sequence"]
	// 全registry/Workflow重开后只读发现：不从all-none或error推断closed。
	r.Close()
	r = environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.closure.bridge", "workflow", 44))
	before = requests.Load()
	discover := must("dagRecoveryResolutionDiscovery", nil, nil)["data"].(map[string]any)
	if discover["state"] != "closed" || discover["operationId"] != id || discover["targetHash"] != hash || requests.Load() != before {
		t.Fatal("cold durable closure discovery missing or used network")
	}
	out = must("dagRecoveryResolutionInfo", nil, nil)
	if out["data"].(map[string]any)["sequence"] != closedSeq {
		t.Fatal("cold closed metadata not retained")
	}
	out = must("closeDAGRecoveryOriginal", fields, []byte(f.code))
	if out["data"].(map[string]any)["sequence"] != closedSeq || transitionPosts.Load() != 0 {
		t.Fatal("terminal retry advanced original sequence")
	}
	// 显式新owner才做完整HPKE/已见history复核，保closed原ID；返回仍false。
	out = must("openDAGRecoveryAfterClosure", nil, []byte(f.code))
	if out["data"].(map[string]any)["rotationRequired"] != true {
		t.Fatal("closed opener promoted management")
	}
	out = must("beginDAGRecoveryTransition", nil, nil)
	another := out["recoveryCode"].(string)
	if another == "" {
		t.Fatal("explicit new operation not available")
	}
	another = ""
	prep := must("dagRecoveryPreparationInfo", nil, nil)["data"].(map[string]any)
	if prep["operationId"] == id || prep["operationId"] == "" || transitionPosts.Load() != 0 {
		t.Fatal("closed original reused or new transition submitted implicitly")
	}
	t.Log("P1 closure Go bridge: actual sealed B2 original; target mismatch zero HTTP; query pending preserved original; server closed + rejected finalCAS stayed pending; fresh current proof queried same ID -> durable closed; terminal retry immutable sequence; explicit fullHPKE new owner -> new ID, still restricted. No recovered/intents closure, SDK/UI/cap activation.")
}

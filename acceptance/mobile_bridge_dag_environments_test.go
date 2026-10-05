//go:build harmonia_boringssl

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/mobilebridge"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type dagEnvironmentBridgeSlot struct {
	b3bBridgeSlot
	rejects atomic.Int32
}

func (s *dagEnvironmentBridgeSlot) CompareAndSwapSealed(old, next []byte) error {
	if s.fail.Load() {
		s.rejects.Add(1)
		return errors.New("synthetic environment CAS rejection")
	}
	return s.b3bBridgeSlot.CompareAndSwapSealed(old, next)
}

// 单条真实HTTPS/TS/SQLite+HPKE+原生PAKE受件人+AES整份CAS适配器链，不是手机SDK。
func TestNativeBridgeDAGEnvironmentHTTPS(t *testing.T) {
	f := newMobileManagerFixture(t)
	ro := newMobileManagerActor(t, f)
	rw := newMobileManagerActor(t, f)
	_ = environmentValue(f.enroll(t, f.root, ro, "environment-reader-pair", []mobileworkflow.ApprovalSelection{{EnvironmentID: f.initial, Role: "ro", ExpiresAt: "0"}}))
	_ = environmentValue(f.root.RetryApprovalV5(context.Background(), "environment-reader-pair"))
	_ = environmentValue(f.enroll(t, f.root, rw, "environment-writer-pair", []mobileworkflow.ApprovalSelection{{EnvironmentID: f.initial, Role: "rw", ExpiresAt: "0"}}))
	_ = environmentValue(f.root.RetryApprovalV5(context.Background(), "environment-writer-pair"))
	roID := environmentValue(ro.workflow.View()).DeviceID
	rwID := environmentValue(rw.workflow.View()).DeviceID
	added := environmentValue(f.root.CreateEnvironment(context.Background(), "合成只读环境Y", "environment-ro-y"))
	roEnv := ""
	for _, env := range added.Environments {
		if env.ID != f.initial {
			roEnv = env.ID
		}
	}
	if roEnv == "" {
		t.Fatal("readonly source missing")
	}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.proxy.Certificate().Raw})
	d := environmentValue(mobilebridge.NewDevice())
	defer func() { d.Close() }()
	registry := environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.dag.environment", "workflow", 121))
	defer func() { registry.Close() }()
	slot := &dagEnvironmentBridgeSlot{}
	open := func() *mobilebridge.VaultWorkflow {
		captured := slot.read()
		defer clear(captured)
		v, e := d.OpenAtomicWorkflow(f.proxy.URL, "synthetic.dag.environment\x00harmonia/workflow-state/v1\x00workflow", captured, ca, slot)
		if e != nil {
			t.Fatal("fresh native adapter", e)
		}
		if e = v.AttachDAGRegistry(registry); e != nil {
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
	decode := func(raw string) map[string]any {
		var out map[string]any
		if json.Unmarshal([]byte(raw), &out) != nil {
			t.Fatal("bridge JSON")
		}
		return out
	}
	recovery := func(op string, fields map[string]string, code []byte) map[string]any {
		v := open()
		defer v.Close()
		raw, e := v.ExecuteDAGRecovery(command(op, fields), code)
		if e != nil {
			t.Fatal("real prerequisite", op, e)
		}
		out := decode(raw)
		if out["ok"] != true {
			t.Fatal("recovery not complete", op)
		}
		return out
	}
	recovery("openDAGRecoveryOwner", map[string]string{"email": f.email, "password": f.password}, []byte(f.code))
	code := recovery("beginDAGRecoveryTransition", nil, nil)["recoveryCode"].(string)
	recovery("sealDAGRecoveryTransition", nil, []byte(code))
	code = ""
	recovery("retryDAGRecoveryTransition", nil, nil)
	choices := recovery("dagRecoveredEnrollmentChoices", nil, nil)["data"].(map[string]any)
	expiry := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	selections := []map[string]string{}
	for _, row := range choices["environments"].([]any) {
		env := row.(map[string]any)
		role := "admin"
		if env["environmentId"] == roEnv {
			role = "ro"
		}
		selections = append(selections, map[string]string{"environmentId": env["environmentId"].(string), "keyVersion": env["keyVersion"].(string), "role": role, "expiresAt": expiry})
	}
	selected, _ := json.Marshal(selections)
	sealed := recovery("sealDAGRecoveredDevice", map[string]string{"expectedSequence": choices["sequence"].(string), "recoveryHeadHash": choices["recoveryHeadHash"].(string), "selections": string(selected)}, nil)["data"].(map[string]any)
	original := map[string]string{"operationId": sealed["operationId"].(string), "contentHash": sealed["contentHash"].(string)}
	recovery("retryDAGRecoveredDevice", original, nil)
	recovery("applyDAGRecoveredDevice", original, nil)
	t.Log("真实PAKE RO/RW+恢复后明确Admin+Boot/P4/finalCAS已完成")
	var posts atomic.Int32
	var lose, failSave atomic.Bool
	var rotationMu sync.Mutex
	var observed cryptox.EnvironmentChange
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(resp *http.Response) error {
		if resp.Request.Method == "POST" && (strings.HasSuffix(resp.Request.URL.Path, "/environment-changes-v4") || strings.HasSuffix(resp.Request.URL.Path, "/environment-changes")) {
			posts.Add(1)
			if resp.StatusCode == 200 && lose.Swap(false) {
				b2LoseResponse(resp)
			}
		}
		if resp.StatusCode == 200 && strings.HasSuffix(resp.Request.URL.Path, "/pull") {
			raw, e := io.ReadAll(resp.Body)
			if e != nil {
				return e
			}
			_ = resp.Body.Close()
			resp.Body = io.NopCloser(bytes.NewReader(raw))
			var p struct {
				EnvironmentEvents []syncclient.EnvironmentEvent `json:"environmentEvents"`
			}
			if json.Unmarshal(raw, &p) != nil {
				return errors.New("synthetic observation decoding")
			}
			for _, event := range p.EnvironmentEvents {
				ch := event.Change.Change
				if ch.Operation == "rotate" && ch.EnvironmentID == f.initial {
					rotationMu.Lock()
					observed = ch
					rotationMu.Unlock()
				}
			}
			if failSave.Swap(false) {
				slot.fail.Store(true)
			}
		}
		return nil
	}})
	environment := func(op string, fields map[string]string, name []byte) (map[string]any, error) {
		v := open()
		defer v.Close()
		raw, e := v.ExecuteDAGEnvironment(command(op, fields), name)
		if !bytes.Equal(name, make([]byte, len(name))) {
			t.Fatal("name buffer retained")
		}
		if e != nil {
			if raw != "" {
				t.Fatal("hard error leaked view")
			}
			return nil, e
		}
		return decode(raw), nil
	}
	must := func(op string, fields map[string]string, name []byte) map[string]any {
		out, e := environment(op, fields, name)
		if e != nil || out["ok"] != true || out["trustedDevice"] != true {
			t.Fatal("environment operation", op, e)
		}
		return out
	}
	source := func(out map[string]any) map[string]any {
		data := out["data"].(map[string]any)
		s := data["source"].(map[string]any)
		if s["operationId"] != original["operationId"] || s["contentHash"] != original["contentHash"] || data["environment"].(map[string]any)["applied"] != true {
			t.Fatal("current source changed")
		}
		return s
	}
	find := func(out map[string]any, id string) map[string]any {
		for _, x := range source(out)["view"].(map[string]any)["environments"].([]any) {
			env := x.(map[string]any)
			if env["id"] == id {
				return env
			}
		}
		return nil
	}
	lose.Store(true)
	out, e := environment("createDAGEnvironment", map[string]string{"requestId": "dag-env-create", "authorityEnvironmentId": f.initial}, []byte("合成环境Z"))
	if e != nil || out["ok"] != false || out["trustedDevice"] != false || posts.Load() != 1 {
		t.Fatal("unknown original missing", e)
	}
	row := out["data"].(map[string]any)["original"].(map[string]any)
	if row["requestId"] != "dag-env-create" || row["sequence"] != "0" || row["applied"] != false {
		t.Fatal("actual unknown metadata")
	}
	z := row["environmentId"].(string)
	// 真正冷New device+registry，sealed whole-state是唯一业务续办来源。
	exported := environmentValue(d.ExportProtectedMaterial())
	d.Close()
	registry.Close()
	d = environmentValue(mobilebridge.ImportProtectedMaterial(exported))
	clear(exported)
	registry = environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.dag.environment", "workflow", 122))
	out, e = environment("pendingDAGEnvironments", nil, nil)
	if e != nil || len(out["data"].(map[string]any)["pending"].([]any)) != 1 {
		t.Fatal("cold pending", e)
	}
	out = must("retryDAGEnvironment", map[string]string{"requestId": "dag-env-create"}, nil)
	if posts.Load() != 1 || find(out, z)["name"] != "合成环境Z" {
		t.Fatal("cold retry changed original")
	}
	before := posts.Load()
	_, e = environment("createDAGEnvironment", map[string]string{"requestId": "dag-env-create", "authorityEnvironmentId": f.initial}, []byte("合成不同输入"))
	if !errors.Is(e, syncclient.ErrWriteConflict) || posts.Load() != before {
		t.Fatal("same ID conflict")
	}
	v := open()
	raw, e := v.ExecuteDAGBusiness(command("putDAGVariable", map[string]string{"requestId": "dag-env-value", "environmentId": z, "name": "ENV_TOKEN"}), []byte("SYNTHETIC_Z_VALUE"))
	v.Close()
	if e != nil || decode(raw)["ok"] != true {
		t.Fatal("new environment shared value", e)
	}
	out = must("renameDAGEnvironment", map[string]string{"requestId": "dag-env-rename", "environmentId": z}, []byte("合成环境改名"))
	if find(out, z)["name"] != "合成环境改名" || find(out, z)["variables"].(map[string]any)["ENV_TOKEN"] != "SYNTHETIC_Z_VALUE" {
		t.Fatal("rename not authoritative")
	}
	t.Log("create丢回应/冷原ID/严格冲突与rename全量下发已完成")
	out = must("rotateDAGEnvironment", map[string]string{"requestId": "dag-env-rotate", "environmentId": f.initial}, nil)
	if find(out, f.initial)["variables"].(map[string]any)["SYNTHETIC_X"] != "synthetic-x-value" {
		t.Fatal("rotate lost live value")
	}
	rotationMu.Lock()
	change := observed
	rotationMu.Unlock()
	if change.KeyVersion != "2" || len(change.Grants) != 4 || len(change.Mutations) != 1 || change.RecoveryGeneration != "2" {
		t.Fatal("incomplete rotate receiver/value/recovery manifest")
	}
	receivers := map[string]struct {
		role string
		key  []byte
	}{roID: {"ro", ro.config.ReceivingPrivateKey}, rwID: {"rw", rw.config.ReceivingPrivateKey}}
	for _, g := range change.Grants {
		if subject, ok := receivers[g.Grant.SubjectDeviceID]; ok {
			if g.Grant.Role != subject.role || g.Grant.ExpiresAt != "0" {
				t.Fatal("rotation changed original role/expiry")
			}
			context := cryptox.EnvelopeContext{AccountID: g.Grant.AccountID, AccountGeneration: g.Grant.AccountGeneration, EnvironmentID: f.initial, KeyVersion: "2", RecipientType: "device", RecipientID: g.Grant.SubjectDeviceID, RecipientGeneration: g.Grant.GrantGeneration, RecipientPublicKey: g.Grant.SubjectReceivingPublicKey}
			key := environmentValue(cryptox.UnwrapEnvironmentKey(subject.key, context, environmentValue(cryptox.DecodeBase64(g.Grant.Envelope, 80, 80))))
			m := change.Mutations[0].Mutation
			plain := environmentValue(cryptox.DecryptValue(key, cryptox.ValueContext{AccountID: m.AccountID, AccountGeneration: m.AccountGeneration, EnvironmentID: f.initial, KeyVersion: "2", Name: m.Name}, environmentValue(cryptox.DecodeBase64(m.Payload, 1, 1<<20))))
			if string(plain) != "synthetic-x-value" {
				t.Fatal("valid receiver cannot read rotated live value")
			}
			clear(key)
			clear(plain)
			delete(receivers, g.Grant.SubjectDeviceID)
		} else if g.Grant.Role != "admin" || g.Grant.ExpiresAt != "0" && g.Grant.ExpiresAt != expiry {
			t.Fatal("admin expiry widened")
		}
	}
	if len(receivers) != 0 {
		t.Fatal("valid receiver missing")
	}
	t.Log("rotate完整四receivers含真实PAKE RO/RW、精确期限与live值已验证")
	before = posts.Load()
	_, e = environment("renameDAGEnvironment", map[string]string{"requestId": "dag-env-ro-denied", "environmentId": roEnv}, []byte("合成拒绝"))
	if !errors.Is(e, syncclient.ErrWritePermission) || posts.Load() != before {
		t.Fatal("RO environment POST")
	}
	out = must("deleteDAGEnvironment", map[string]string{"requestId": "dag-env-delete", "environmentId": z}, nil)
	if find(out, z) != nil {
		t.Fatal("delete source still visible")
	}
	before = posts.Load()
	out = must("retryDAGEnvironment", map[string]string{"requestId": "dag-env-rename"}, nil)
	if find(out, z) != nil || posts.Load() != before {
		t.Fatal("old original receipt replayed rename")
	}
	t.Log("delete安全下发与历史原receipt不复活环境已完成")
	beforeBlob := slot.read()
	failSave.Store(true)
	_, e = environment("createDAGEnvironment", map[string]string{"requestId": "dag-env-cas-denied", "authorityEnvironmentId": f.initial}, []byte("合成保存失败"))
	if e == nil || posts.Load() != before || !bytes.Equal(beforeBlob, slot.read()) || slot.rejects.Load() != 1 {
		t.Fatal("native beforePOST CAS failed open")
	}
	t.Log("nativeCAS拒绝严格零environment POST已完成")
}

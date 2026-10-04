package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/mobilebridge"
)

// 完整 AES 密文 CAS/Check 的 Go 适配器；不代表已执行系统强认证或 SDK。
type b3aBridgeSlot struct {
	mu     sync.Mutex
	packet []byte
}

func (s *b3aBridgeSlot) SaveSealed([]byte) error {
	return errors.New("native bridge test requires CAS")
}
func (s *b3aBridgeSlot) CompareAndSwapSealed(old, next []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (old == nil) != (s.packet == nil) || !bytes.Equal(old, s.packet) {
		return errors.New("synthetic native CAS conflict")
	}
	s.packet = bytes.Clone(next)
	return nil
}
func (s *b3aBridgeSlot) CheckSealed(old []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if (old == nil) != (s.packet == nil) || !bytes.Equal(old, s.packet) {
		return errors.New("synthetic native stale capture")
	}
	return nil
}
func (s *b3aBridgeSlot) read() []byte { s.mu.Lock(); defer s.mu.Unlock(); return bytes.Clone(s.packet) }

func TestNativeBridgeDAGRecoveredHTTPS(t *testing.T) {
	f := newMobileManagerFixture(t)
	if _, e := f.root.CreateEnvironment(context.Background(), "合成未选环境Y", "native-b3a-y"); e != nil {
		t.Fatal("second synthetic environment", e)
	}
	target := environmentValue(url.Parse(f.proxy.URL))
	reverse := httputil.NewSingleHostReverseProxy(target)
	reverse.Transport = f.proxy.Client().Transport
	var lose atomic.Bool
	lose.Store(true)
	var posts, challenges, requests atomic.Int32
	var block atomic.Bool
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var routeMu sync.Mutex
	var postedIDs, selectedEnvironments []string
	reverse.ModifyResponse = func(r *http.Response) error {
		if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovered-devices-v2") && r.StatusCode == 200 && lose.Swap(false) {
			b2LoseResponse(r)
		}
		if r.Request.Method == "GET" && strings.Contains(r.Request.URL.Path, "/recovered-devices-v2/") && block.Swap(false) {
			close(entered)
			<-release
		}
		return nil
	}
	proxy := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/recovered-device-challenges-v2") {
			challenges.Add(1)
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/recovered-devices-v2") {
			posts.Add(1)
			body, e := io.ReadAll(io.LimitReader(r.Body, 8<<20))
			if e != nil {
				w.WriteHeader(400)
				return
			}
			defer clear(body)
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			var packet cryptox.RecoveredDeviceCommandV2
			if json.Unmarshal(body, &packet) != nil {
				w.WriteHeader(400)
				return
			}
			routeMu.Lock()
			postedIDs = append(postedIDs, packet.Submission.Enrollment.OperationID)
			for _, row := range packet.Submission.SelectedRights {
				selectedEnvironments = append(selectedEnvironments, row.EnvironmentID)
			}
			routeMu.Unlock()
		}
		reverse.ServeHTTP(w, r)
	}))
	defer proxy.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxy.Certificate().Raw})
	d := environmentValue(mobilebridge.NewDevice())
	defer d.Close()
	r := environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.b3a.native", "workflow", 41))
	defer r.Close()
	slot := &b3aBridgeSlot{}
	open := func(reg *mobilebridge.NativeDAGRegistry) *mobilebridge.VaultWorkflow {
		captured := slot.read()
		defer clear(captured)
		v, e := d.OpenAtomicWorkflow(proxy.URL, "synthetic.b3a.native\x00harmonia/workflow-state/v1\x00workflow", captured, ca, slot)
		if e != nil {
			t.Fatal("captured native workflow", e)
		}
		if e = v.AttachDAGRegistry(reg); e != nil {
			v.Close()
			t.Fatal("native registry attachment", e)
		}
		return v
	}
	command := func(op string, fields map[string]string) string {
		m := map[string]any{"version": 1, "endpoint": proxy.URL, "operation": op}
		for k, v := range fields {
			m[k] = v
		}
		b, _ := json.Marshal(m)
		return string(b)
	}
	var last string
	dispatch := func(op string, fields map[string]string, code []byte) map[string]any {
		v := open(r)
		defer v.Close()
		raw, e := v.ExecuteDAGRecovery(command(op, fields), code)
		if e != nil {
			t.Fatal("native DAG operation", op, e)
		}
		if !bytes.Equal(code, make([]byte, len(code))) {
			t.Fatal("complete-code buffer retained")
		}
		last = raw
		var out map[string]any
		if json.Unmarshal([]byte(raw), &out) != nil || out["trustedDevice"] != false || out["version"] != float64(1) {
			t.Fatal("native projection")
		}
		if op == "dagRecoveredEnrollmentChoices" || op == "sealDAGRecoveredDevice" || op == "retryDAGRecoveredDevice" || op == "dagRecoveredDeviceInfo" {
			for _, key := range []string{"sessionToken", "ciphertext", "privateKey", "recoveryCode", "envelope", "selectedRights"} {
				if strings.Contains(raw, key) {
					t.Fatal("secret/wire leaked into native DTO")
				}
			}
		}
		return out
	}
	out := dispatch("openDAGRecoveryOwner", map[string]string{"email": f.email, "password": f.password}, []byte(f.code))
	if out["ok"] != true {
		t.Fatal("owner not opened")
	}
	out = dispatch("beginDAGRecoveryTransition", nil, nil)
	newCode, ok := out["recoveryCode"].(string)
	if !ok || newCode == "" {
		t.Fatal("B2 missing full code")
	}
	dispatch("sealDAGRecoveryTransition", nil, []byte(newCode))
	newCode = ""
	out = dispatch("retryDAGRecoveryTransition", nil, nil)
	if out["ok"] != true || out["data"].(map[string]any)["rotationRequired"] != false {
		t.Fatal("B2 not durably confirmed")
	}
	out = dispatch("dagRecoveredEnrollmentChoices", nil, nil)
	choices := out["data"].(map[string]any)
	envs := choices["environments"].([]any)
	if len(envs) != 2 {
		t.Fatal("verified choices missing explicit selection prerequisite")
	}
	var kv string
	for _, row := range envs {
		env := row.(map[string]any)
		if env["environmentId"] == f.initial {
			kv = env["keyVersion"].(string)
		}
	}
	if kv == "" {
		t.Fatal("chosen environment version absent")
	}
	selected, _ := json.Marshal([]map[string]string{{"environmentId": f.initial, "keyVersion": kv, "role": "ro", "expiresAt": strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}})
	sealFields := map[string]string{"expectedSequence": choices["sequence"].(string), "recoveryHeadHash": choices["recoveryHeadHash"].(string), "selections": string(selected)}
	out = dispatch("sealDAGRecoveredDevice", sealFields, nil)
	info := out["data"].(map[string]any)
	id, hash := info["operationId"].(string), info["contentHash"].(string)
	if id == "" || len(hash) != 64 || info["originalConfirmed"] != false || info["acceptedSequence"] != "0" || posts.Load() != 0 || challenges.Load() != 1 {
		t.Fatal("seal performed acceptance or lost original metadata")
	}
	sealed := slot.read()
	// 冷 metadata 与同 owner 重封不新增挑战或改原包。
	out = dispatch("dagRecoveredDeviceInfo", nil, nil)
	if out["data"].(map[string]any)["contentHash"] != hash || !bytes.Equal(sealed, slot.read()) {
		t.Fatal("cold info changed original")
	}
	out = dispatch("sealDAGRecoveredDevice", sealFields, nil)
	if out["data"].(map[string]any)["operationId"] != id || challenges.Load() != 1 || !bytes.Equal(sealed, slot.read()) {
		t.Fatal("same intent regenerated challenge/packet")
	}
	// 错ID/hash零网络，使用独立registry避免有意错误退休正常原owner。
	before := requests.Load()
	wrongReg := environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.b3a.native", "workflow", 42))
	v := open(wrongReg)
	_, e := v.ExecuteDAGRecovery(command("retryDAGRecoveredDevice", map[string]string{"operationId": id, "contentHash": strings.Repeat("0", 64)}), nil)
	v.Close()
	wrongReg.Close()
	if e == nil || requests.Load() != before || !bytes.Equal(sealed, slot.read()) {
		t.Fatal("retry mismatch sent traffic or changed original")
	}
	out = dispatch("retryDAGRecoveredDevice", map[string]string{"operationId": id, "contentHash": hash}, nil)
	if out["ok"] != false || out["error"].(map[string]any)["ownerRetained"] != true || out["data"].(map[string]any)["contentHash"] != hash {
		t.Fatal("lost response not original-only retained owner")
	}
	out = dispatch("retryDAGRecoveredDevice", map[string]string{"operationId": id, "contentHash": hash}, nil)
	info = out["data"].(map[string]any)
	if out["ok"] != true || info["state"] != "accepted-not-device-applied" || info["originalConfirmed"] != true || info["acceptedSequence"] == "0" || posts.Load() != 1 || challenges.Load() != 1 {
		t.Fatal("original receipt confirmation failed or duplicate write")
	}
	routeMu.Lock()
	validSelection := len(postedIDs) == 1 && postedIDs[0] == id && len(selectedEnvironments) == 1 && selectedEnvironments[0] == f.initial
	routeMu.Unlock()
	if !validSelection {
		t.Fatal("unselected environment or changed ID submitted")
	}
	out = dispatch("dagRecoveredDeviceInfo", nil, nil)
	if out["data"].(map[string]any)["contentHash"] != hash || strings.Contains(last, "SYNTHETIC_X") {
		t.Fatal("cold accepted info leaked values/lost original")
	}
	// 同一正式 retry 的 HTTPS 回应被暂停，Invalidate不先取桥句柄排空锁。
	block.Store(true)
	v = open(r)
	type result struct {
		raw string
		err error
	}
	done := make(chan result, 1)
	go func() {
		raw, e := v.ExecuteDAGRecovery(command("retryDAGRecoveredDevice", map[string]string{"operationId": id, "contentHash": hash}), nil)
		done <- result{raw, e}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("bounded retry did not enter verified HTTPS")
	}
	invalidated := make(chan struct{})
	go func() { v.Invalidate(); close(invalidated) }()
	select {
	case <-invalidated:
	case <-time.After(time.Second):
		t.Fatal("Invalidate waited for network-held handle lock")
	}
	select {
	case got := <-done:
		if got.err == nil || got.raw != "" {
			t.Fatal("cancelled late result published")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("network cancellation did not drain bounded operation")
	}
	releaseOnce.Do(func() { close(release) })
	closed := make(chan struct{})
	go func() { v.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(3 * time.Second):
		t.Fatal("current handle did not drain")
	}
	if posts.Load() != 1 || challenges.Load() != 1 {
		t.Fatal("cancel generated business write")
	}
	t.Log("B3a native Go: verified HTTPS/B2/HPKE -> explicit one-of-two RO selection -> sealed original -> 504 -> exact original receipt -> cold metadata untrusted; mismatch zero traffic; Invalidate canceled actual retry and rejected late result. No SDK/UI/B3b/capability activation.")
}

package acceptance

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/mobilebridge"
)

type b3bBridgeSlot struct {
	b3aBridgeSlot
	fail atomic.Bool
}

func (s *b3bBridgeSlot) CompareAndSwapSealed(old, next []byte) error {
	if s.fail.Load() {
		return errors.New("synthetic final CAS rejection")
	}
	return s.b3aBridgeSlot.CompareAndSwapSealed(old, next)
}

func TestNativeBridgeDAGAppliedHTTPS(t *testing.T) {
	f := newMobileManagerFixture(t)
	var challengeLost atomic.Bool
	challengeLost.Store(true)
	var failFinal atomic.Bool
	var enrollPosts, boots, pulls atomic.Int32
	slot := &b3bBridgeSlot{}
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovered-device-challenges-v2") && challengeLost.Swap(false) {
			b2LoseResponse(r)
		}
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovered-devices-v2") {
			enrollPosts.Add(1)
		}
		if r.StatusCode == 200 && strings.HasSuffix(r.Request.URL.Path, "/boot-sessions") {
			boots.Add(1)
		}
		if r.StatusCode == 200 && strings.HasSuffix(r.Request.URL.Path, "/pull") {
			pulls.Add(1)
			if failFinal.Swap(false) {
				slot.fail.Store(true)
			}
		}
		return nil
	}})
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.proxy.Certificate().Raw})
	d := environmentValue(mobilebridge.NewDevice())
	defer d.Close()
	r := environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.b3b.native", "workflow", 41))
	defer func() { r.Close() }()
	open := func() *mobilebridge.VaultWorkflow {
		captured := slot.read()
		defer clear(captured)
		v, e := d.OpenAtomicWorkflow(f.proxy.URL, "synthetic.b3b.native\x00harmonia/workflow-state/v1\x00workflow", captured, ca, slot)
		if e != nil {
			t.Fatal("native captured workflow", e)
		}
		if e = v.AttachDAGRegistry(r); e != nil {
			v.Close()
			t.Fatal("native registry", e)
		}
		return v
	}
	cmd := func(op string, fields map[string]string) string {
		out := map[string]any{"version": 1, "endpoint": f.proxy.URL, "operation": op}
		for k, v := range fields {
			out[k] = v
		}
		raw, _ := json.Marshal(out)
		return string(raw)
	}
	dispatch := func(op string, fields map[string]string, code []byte) (map[string]any, error) {
		v := open()
		defer v.Close()
		raw, e := v.ExecuteDAGRecovery(cmd(op, fields), code)
		if !bytes.Equal(code, make([]byte, len(code))) {
			t.Fatal("caller code retained")
		}
		if e != nil {
			if raw != "" {
				t.Fatal("hard error returned public data")
			}
			return nil, e
		}
		var out map[string]any
		if json.Unmarshal([]byte(raw), &out) != nil {
			t.Fatal("native DTO")
		}
		return out, nil
	}
	must := func(op string, fields map[string]string, code []byte) map[string]any {
		out, e := dispatch(op, fields, code)
		if e != nil {
			t.Fatal("native stage", op, e)
		}
		return out
	}
	must("openDAGRecoveryOwner", map[string]string{"email": f.email, "password": f.password}, []byte(f.code))
	out := must("beginDAGRecoveryTransition", nil, nil)
	newCode := out["recoveryCode"].(string)
	// 真实B2 preparation期，B3a冷info为none而不是读取不相干Pending报错。
	out = must("dagRecoveredDeviceInfo", nil, nil)
	if out["data"].(map[string]any)["state"] != "none" || out["trustedDevice"] != false {
		t.Fatal("transition preparation misidentified")
	}
	must("sealDAGRecoveryTransition", nil, []byte(newCode))
	newCode = ""
	must("retryDAGRecoveryTransition", nil, nil)
	out = must("dagRecoveredEnrollmentChoices", nil, nil)
	choices := out["data"].(map[string]any)
	rows := choices["environments"].([]any)
	if len(rows) != 1 {
		t.Fatal("synthetic choice count")
	}
	env := rows[0].(map[string]any)
	selection, _ := json.Marshal([]map[string]string{{"environmentId": env["environmentId"].(string), "keyVersion": env["keyVersion"].(string), "role": "ro", "expiresAt": strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}})
	fields := map[string]string{"expectedSequence": choices["sequence"].(string), "recoveryHeadHash": choices["recoveryHeadHash"].(string), "selections": string(selection)}
	out = must("sealDAGRecoveredDevice", fields, nil)
	interrupted := out["data"].(map[string]any)
	if out["ok"] != false || interrupted["state"] != "interrupted-original" || interrupted["phase"] != "intent" || interrupted["contentHash"] != "" || interrupted["trustedDevice"] != false {
		t.Fatal("challenge loss fabricated sealed original")
	}
	intentID := interrupted["operationId"].(string)
	out = must("dagRecoveredDeviceInfo", nil, nil)
	if out["data"].(map[string]any)["operationId"] != intentID || out["data"].(map[string]any)["contentHash"] != "" {
		t.Fatal("cold original preparation inaccessible")
	}
	out = must("sealDAGRecoveredDevice", fields, nil)
	original := out["data"].(map[string]any)
	id, hash := original["operationId"].(string), original["contentHash"].(string)
	if id != intentID || len(hash) != 64 || enrollPosts.Load() != 0 {
		t.Fatal("continuation changed original ID or submitted early")
	}
	out = must("retryDAGRecoveredDevice", map[string]string{"operationId": id, "contentHash": hash}, nil)
	if out["trustedDevice"] != false || out["data"].(map[string]any)["originalConfirmed"] != true || enrollPosts.Load() != 1 {
		t.Fatal("accepted original promoted before device CAS")
	}
	accepted := slot.read()
	failFinal.Store(true)
	out, e := dispatch("applyDAGRecoveredDevice", map[string]string{"operationId": id, "contentHash": hash}, nil)
	if e == nil || out != nil || !bytes.Equal(accepted, slot.read()) || boots.Load() != 1 || pulls.Load() != 1 {
		t.Fatal("final CAS failure promoted or lost original")
	}
	slot.fail.Store(false)
	r.Close()
	r = environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.b3b.native", "workflow", 42))
	out = must("dagRecoveredDeviceInfo", nil, nil)
	if out["data"].(map[string]any)["contentHash"] != hash || out["trustedDevice"] != false {
		t.Fatal("CAS failure lost restricted original")
	}
	out = must("applyDAGRecoveredDevice", map[string]string{"operationId": id, "contentHash": hash}, nil)
	if out["ok"] != true || out["trustedDevice"] != true || boots.Load() != 2 || pulls.Load() != 2 || enrollPosts.Load() != 1 {
		t.Fatal("formal Boot/P4/finalCAS not completed")
	}
	checkView := func(out map[string]any) map[string]any {
		data := out["data"].(map[string]any)
		if data["trustedDevice"] != true || data["operationId"] != id || data["contentHash"] != hash {
			t.Fatal("trusted source changed original")
		}
		binding := data["binding"].(map[string]any)
		view := data["view"].(map[string]any)
		if binding["accountId"] == "" || binding["accountGeneration"] != "1" || binding["deviceId"] != view["deviceId"] || binding["checkpoint"] != view["checkpoint"] {
			t.Fatal("protected account scope missing")
		}
		envs := view["environments"].([]any)
		if len(envs) != 1 {
			t.Fatal("authorized environment missing")
		}
		erow := envs[0].(map[string]any)
		if erow["id"] != f.initial || erow["role"] != "RO" || erow["variables"].(map[string]any)["SYNTHETIC_X"] != "synthetic-x-value" {
			t.Fatal("real authorized read page unavailable")
		}
		return binding
	}
	binding := checkView(out)
	out = must("restoreDAGRecoveredDevice", nil, nil)
	checkView(out)
	if boots.Load() != 2 || pulls.Load() != 2 {
		t.Fatal("offline restore used network")
	}
	out = must("pullDAGRecoveredDevice", nil, nil)
	checkView(out)
	if boots.Load() != 3 || pulls.Load() != 3 {
		t.Fatal("fresh pull did not revalidate current device")
	}
	// 由真实首次初始化A持钥Boot后签none，服务器验证当前Admin及DAG历史来源。
	rootView := environmentValue(f.root.View())
	account, gen := binding["accountId"].(string), binding["accountGeneration"].(string)
	rootID := rootView.DeviceID
	rootRecv := environmentValue(ecdh.X25519().NewPrivateKey(f.rootActor.config.ReceivingPrivateKey)).PublicKey().Bytes()
	base := "/v1/accounts/" + account
	var ch struct {
		ChallengeID    string   `json:"challengeId"`
		Nonce          string   `json:"nonce"`
		ExpiresAt      int64    `json:"expiresAt"`
		SigningPayload []string `json:"signingPayload"`
	}
	if got := callJSON(t, f.proxy.Client(), f.proxy.URL, base+"/boot-challenges", "", "", "", map[string]string{"deviceId": rootID, "accountGeneration": gen}, &ch); got != 200 {
		t.Fatal("real root boot challenge", got)
	}
	proof := environmentValue(cryptox.NewDeviceBootProof(account, gen, rootID, f.rootActor.config.SigningKey.Public().(ed25519.PublicKey), rootRecv, ch.ChallengeID, ch.Nonce, strconv.FormatInt(ch.ExpiresAt, 10)))
	payload := environmentValue(proof.SigningBytes())
	var expected []string
	if json.Unmarshal(payload, &expected) != nil || !reflect.DeepEqual(expected, ch.SigningPayload) || ch.ExpiresAt <= time.Now().Unix() {
		t.Fatal("root boot server context mismatched")
	}
	sig := environmentValue(cryptox.SignDeviceBootProof(proof, f.rootActor.config.SigningKey))
	var session struct {
		Token string `json:"token"`
	}
	if got := callJSON(t, f.proxy.Client(), f.proxy.URL, base+"/boot-sessions", "", "", "", map[string]string{"deviceId": rootID, "accountGeneration": gen, "challengeId": ch.ChallengeID, "signature": sig}, &session); got != 200 {
		t.Fatal("real root device session", got)
	}
	material := environmentValue(d.ExportProtectedMaterial())
	defer clear(material)
	seedOffset := len("HARMKEY1")
	subKey := ed25519.NewKeyFromSeed(material[seedOffset : seedOffset+32])
	defer clear(subKey)
	subRecv := environmentValue(ecdh.X25519().NewPrivateKey(material[seedOffset+32:])).PublicKey().Bytes()
	grant := cryptox.Grant{AccountID: account, AccountGeneration: gen, IssuerDeviceID: rootID, SubjectDeviceID: binding["deviceId"].(string), SubjectSigningPublicKey: cryptox.EncodeBase64(subKey.Public().(ed25519.PublicKey)), SubjectReceivingPublicKey: cryptox.EncodeBase64(subRecv), EnvironmentID: f.initial, KeyVersion: env["keyVersion"].(string), GrantGeneration: "2", Role: "none", ExpiresAt: "0", IdempotencyKey: "native-b3b-current-none", Envelope: ""}
	signed := environmentValue(cryptox.SignGrant(grant, f.rootActor.config.SigningKey))
	body := environmentValue(json.Marshal(cryptox.GrantToWire(signed)))
	req := environmentValue(http.NewRequest("POST", f.proxy.URL+base+"/grants", bytes.NewReader(body)))
	req.Header.Set("Authorization", "Bearer "+session.Token)
	req.Header.Set("X-Harmonia-Device-Id", rootID)
	req.Header.Set("X-Harmonia-Account-Generation", gen)
	req.Header.Set("Harmonia-Protocol-Major", "2")
	req.Header.Set("Content-Type", "application/json")
	resp := environmentValue(f.proxy.Client().Do(req))
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	session.Token = ""
	if resp.StatusCode != 200 {
		t.Fatal("real current none transaction", resp.StatusCode)
	}
	out, e = dispatch("pullDAGRecoveredDevice", nil, nil)
	if e == nil || out != nil {
		t.Fatal("current no-grant device retained trusted read result")
	}
	r.Close()
	r = environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.b3b.native", "workflow", 43))
	if out, e = dispatch("restoreDAGRecoveredDevice", nil, nil); e == nil || out != nil {
		t.Fatal("invalidated cold source remained trusted")
	}
	t.Log("P1 B3b Go bridge: challenge504 exact-intent continuation; real accepted original remained false; formal Boot/P4 + rejected finalCAS stayed restricted; original retry finalCAS -> authorized variable view; cold offline restore + fresh pull; real signed none -> current boot rejection and cold source cleared. No SDK/UI/cap/legacy fallback.")
}

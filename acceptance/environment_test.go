package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/syncclient"
)

func environmentValue[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}
	return value
}

// 本验收只使用已知合成设备夹具与测试内固定管理公钥；不计为真实入网。
// 真实首次初始化与 SPAKE2 双签入网由 enrollment_test.go 独立验收。
// 本地状态和 provider 都是临时纯 Go 隔离文件，不读取宿主环境或保存真实钥匙。
func TestGoEnvironmentLifecycleHTTPSRotationDeletionAndGlobalRevocation(t *testing.T) {
	ctx := context.Background()
	recovery := environmentValue(cryptox.DeriveRecoveryKeys(bytes.Repeat([]byte{5}, 32), "synthetic-account", "1", "1"))
	devKey := environmentValue(cryptox.GenerateEnvironmentKey())
	recoveryEnvelope := func(id, version string, key []byte) string {
		t.Helper()
		p := environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: "synthetic-account", AccountGeneration: "1", EnvironmentID: id, KeyVersion: version, RecipientType: "recovery", RecipientID: "synthetic-account", RecipientGeneration: "1", RecipientPublicKey: cryptox.EncodeBase64(recovery.ReceivingPublic)}))
		return cryptox.EncodeBase64(p)
	}
	input := filepath.Join(t.TempDir(), "synthetic-envelopes.json")
	data, _ := json.Marshal([]map[string]string{{"environmentId": "dev", "envelope": recoveryEnvelope("dev", "1", devKey)}})
	if err := os.WriteFile(input, data, 0600); err != nil {
		t.Fatal(err)
	}
	f := startFixture(t, "--with-trust-root", "--recovery-envelopes", input)
	if err := cryptox.VerifyTrustRoot(f.AccountID, f.AccountGeneration, f.TrustRoot, recovery.SigningPublic); err != nil {
		t.Fatal(err)
	}
	backend := environmentValue(url.Parse(f.Endpoint))
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(backend))
	defer proxy.Close()
	httpClient := proxy.Client()
	httpClient.Timeout = 15 * time.Second
	base := "/v1/accounts/" + f.AccountID
	var login struct {
		Token string `json:"token"`
	}
	if status := callJSON(t, httpClient, proxy.URL, "/v1/login", "", "", "", map[string]string{"email": f.Email, "credential": f.Credential}, &login); status != 200 {
		t.Fatalf("合成登录HTTP %d", status)
	}
	tokens := map[string]string{}
	for _, id := range []string{"admin", "writer", "reader"} {
		tokens[id] = bindDevice(t, f, httpClient, proxy.URL, login.Token, id)
	}
	var sequence uint64
	post := func(path, actor string, payload any) syncclient.Acceptance {
		t.Helper()
		var accepted syncclient.Acceptance
		if status := callJSON(t, httpClient, proxy.URL, base+path, tokens[actor], actor, "1", payload, &accepted); status != 200 {
			t.Fatalf("%s HTTP %d", path, status)
		}
		sequence = accepted.Sequence
		return accepted
	}
	grantFor := func(id, env, version, generation, role, operationID string, key []byte) cryptox.SignedGrantWire {
		t.Helper()
		packet := environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: env, KeyVersion: version, RecipientType: "device", RecipientID: id, RecipientGeneration: generation, RecipientPublicKey: f.Devices[id].ReceivingPublicKey}))
		g := cryptox.Grant{AccountID: f.AccountID, AccountGeneration: "1", IssuerDeviceID: "admin", SubjectDeviceID: id, SubjectSigningPublicKey: f.Devices[id].SigningPublicKey, SubjectReceivingPublicKey: f.Devices[id].ReceivingPublicKey, EnvironmentID: env, KeyVersion: version, GrantGeneration: generation, Role: role, ExpiresAt: "0", IdempotencyKey: operationID, Envelope: cryptox.EncodeBase64(packet)}
		return cryptox.GrantToWire(environmentValue(cryptox.SignGrant(g, keyFor(t, f, "admin"))))
	}
	roles := map[string]string{"admin": "admin", "reader": "ro", "writer": "rw"}
	// 先把默认夹具的占位封套替换为真正 HPKE，三种设备使用同一环境钥。
	for _, id := range []string{"writer", "reader", "admin"} {
		post("/grants", "admin", grantFor(id, "dev", "1", "2", roles[id], "real-dev-"+id, devKey))
	}
	prodKey := environmentValue(cryptox.GenerateEnvironmentKey())
	label := func(key []byte, version, name string) string {
		return cryptox.EncodeBase64(environmentValue(cryptox.EncryptEnvironmentLabel(key, cryptox.EnvironmentLabelContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "prod", KeyVersion: version}, []byte(name))))
	}
	change := cryptox.EnvironmentChange{AccountID: f.AccountID, AccountGeneration: "1", DeviceID: "admin", EnvironmentID: "prod", Operation: "create", AuthorityEnvironmentID: "dev", AuthorityKeyVersion: "1", AuthorityGrantGeneration: "2", PreviousKeyVersion: "0", KeyVersion: "1", ExpectedSequence: strconv.FormatUint(sequence, 10), IdempotencyKey: "real-create-prod", LabelPayload: label(prodKey, "1", "合成环境"), RecoveryGeneration: "1", RecoveryEnvelope: recoveryEnvelope("prod", "1", prodKey), Grants: []cryptox.SignedGrantWire{grantFor("admin", "prod", "1", "1", "admin", "real-create-prod-admin", prodKey)}, Mutations: []cryptox.SignedMutationWire{}}
	created := environmentValue(cryptox.SignEnvironmentChange(change, keyFor(t, f, "admin")))
	createResult := post("/environment-changes", "admin", created)
	if retry := post("/environment-changes", "admin", created); !retry.Replayed || retry.Sequence != createResult.Sequence {
		t.Fatal("环境创建重试产生新序号")
	}
	for _, id := range []string{"reader", "writer"} {
		post("/grants", "admin", grantFor(id, "prod", "1", "1", roles[id], "real-prod-"+id, prodKey))
	}
	write := func(actor, env, version, generation, name, value, operationID string, key []byte) cryptox.SignedMutationWire {
		t.Helper()
		packet := environmentValue(cryptox.EncryptValue(key, cryptox.ValueContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: env, KeyVersion: version, Name: name}, []byte(value)))
		mutation := cryptox.Mutation{AccountID: f.AccountID, AccountGeneration: "1", DeviceID: actor, EnvironmentID: env, KeyVersion: version, GrantGeneration: generation, Operation: "put", IdempotencyKey: operationID, Name: name, Payload: cryptox.EncodeBase64(packet)}
		return cryptox.MutationToWire(environmentValue(cryptox.SignMutation(mutation, keyFor(t, f, actor))))
	}
	oldWrite := write("writer", "prod", "1", "1", "SYNTHETIC_STACK", "synthetic-high", "prod-stack", prodKey)
	post("/mutations", "writer", oldWrite)
	post("/mutations", "writer", write("writer", "prod", "1", "1", "SYNTHETIC_ADDED", "synthetic-added", "prod-added", prodKey))
	post("/mutations", "writer", write("writer", "dev", "1", "2", "SYNTHETIC_STACK", "synthetic-low", "dev-stack", devKey))
	root := filepath.Join(t.TempDir(), "isolated-user")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	store := environmentValue(localstate.OpenFileStore(filepath.Join(root, "state.json")))
	defer store.Close()
	engine := environmentValue(localstate.New(store))
	verifier := environmentValue(syncclient.NewPinnedVerifier(syncclient.PinnedTrust{AccountID: f.AccountID, AccountGeneration: 1, DeviceID: "reader", DeviceSigningPublicKey: keyFor(t, f, "reader").Public().(ed25519.PublicKey), ReceivingPrivateKey: bytes.Repeat([]byte{8}, 32), Managers: map[string]ed25519.PublicKey{"admin": keyFor(t, f, "admin").Public().(ed25519.PublicKey)}}))
	config := syncclient.Config{Endpoint: proxy.URL, HTTPClient: httpClient, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: "reader", Token: tokens["reader"], Engine: engine, Verifier: verifier}
	reader := environmentValue(syncclient.New(config))
	if _, err := reader.Pull(ctx); err != nil {
		t.Fatal(err)
	}
	if err := engine.Activate("dev", 10, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := engine.Activate("prod", 20, time.Now()); err != nil {
		t.Fatal(err)
	}
	provider := &localstate.FileProvider{Path: filepath.Join(root, "provider.json")}
	initial, _ := json.Marshal(map[string]string{"SYNTHETIC_STACK": "original-synthetic", "UNRELATED": "keep-synthetic"})
	if err := os.WriteFile(provider.Path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	if err := engine.Reconcile(ctx, provider, time.Now()); err != nil {
		t.Fatal(err)
	}
	values := environmentValue(provider.Snapshot(ctx, []string{"SYNTHETIC_STACK", "SYNTHETIC_ADDED", "UNRELATED"}))
	if values["SYNTHETIC_STACK"] != "synthetic-high" || values["SYNTHETIC_ADDED"] != "synthetic-added" {
		t.Fatal("初始设备HPKE/AEAD或优先级下发错误")
	}
	change.Operation = "rename"
	change.AuthorityEnvironmentID = "prod"
	change.AuthorityGrantGeneration = "1"
	change.PreviousKeyVersion = "1"
	change.ExpectedSequence = strconv.FormatUint(sequence, 10)
	change.IdempotencyKey = "real-rename-prod"
	change.LabelPayload = label(prodKey, "1", "改名后的合成环境")
	change.RecoveryEnvelope = ""
	change.Grants = nil
	renamed := environmentValue(cryptox.SignEnvironmentChange(change, keyFor(t, f, "admin")))
	post("/environment-changes", "admin", renamed)
	if _, err := reader.Pull(ctx); err != nil {
		t.Fatal("改名签事件未被正式客户端接受", err)
	}
	var listed struct {
		Environments []struct {
			EnvironmentID string                  `json:"environmentId"`
			KeyVersion    string                  `json:"keyVersion"`
			LabelPayload  string                  `json:"labelPayload"`
			Grant         cryptox.SignedGrantWire `json:"grant"`
		} `json:"environments"`
	}
	if status := callJSON(t, httpClient, proxy.URL, base+"/environments", tokens["reader"], "reader", "1", nil, &listed); status != 200 {
		t.Fatalf("列表HTTP %d", status)
	}
	for _, env := range listed.Environments {
		if env.EnvironmentID == "prod" {
			packet := environmentValue(cryptox.DecodeBase64(env.LabelPayload, 40, 65576))
			plain := environmentValue(cryptox.DecryptEnvironmentLabel(prodKey, cryptox.EnvironmentLabelContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "prod", KeyVersion: "1"}, packet))
			if string(plain) != "改名后的合成环境" {
				t.Fatal("端到端名称解密错误")
			}
		}
	}
	rotatedKey := environmentValue(cryptox.GenerateEnvironmentKey())
	if bytes.Equal(rotatedKey, prodKey) {
		t.Fatal("环境轮换未生成独立新钥")
	}
	rotation := change
	rotation.Operation = "rotate"
	rotation.AuthorityKeyVersion = "1"
	rotation.AuthorityGrantGeneration = "1"
	rotation.KeyVersion = "2"
	rotation.ExpectedSequence = strconv.FormatUint(sequence, 10)
	rotation.IdempotencyKey = "real-rotate-prod"
	rotation.LabelPayload = label(rotatedKey, "2", "改名后的合成环境")
	rotation.RecoveryEnvelope = recoveryEnvelope("prod", "2", rotatedKey)
	rotation.Grants = nil
	for _, id := range []string{"admin", "reader", "writer"} {
		rotation.Grants = append(rotation.Grants, grantFor(id, "prod", "2", "2", roles[id], "real-rotate-prod-"+id, rotatedKey))
	}
	rotation.Mutations = []cryptox.SignedMutationWire{write("admin", "prod", "2", "2", "SYNTHETIC_STACK", "synthetic-high", "rekey-stack", rotatedKey), write("admin", "prod", "2", "2", "SYNTHETIC_ADDED", "synthetic-added", "rekey-added", rotatedKey)}
	var before syncclient.Pull
	if status := callJSON(t, httpClient, proxy.URL, base+"/pull?after=0", tokens["admin"], "admin", "1", nil, &before); status != 200 {
		t.Fatal(status)
	}
	incomplete := rotation
	incomplete.IdempotencyKey = "missing-device-envelope"
	incomplete.Grants = rotation.Grants[:2]
	invalid := environmentValue(cryptox.SignEnvironmentChange(incomplete, keyFor(t, f, "admin")))
	if status := callJSON(t, httpClient, proxy.URL, base+"/environment-changes", tokens["admin"], "admin", "1", invalid, nil); status != 409 {
		t.Fatalf("遗漏设备封套未拒绝HTTP %d", status)
	}
	invalid.Change = rotation
	invalid.Change.RecoveryEnvelope = "" // 用非法wire验证服务器，不能绕过Go signing helper作成功操作。
	if status := callJSON(t, httpClient, proxy.URL, base+"/environment-changes", tokens["admin"], "admin", "1", invalid, nil); status != 400 {
		t.Fatalf("遗漏恢复封套未拒绝HTTP %d", status)
	}
	var after syncclient.Pull
	callJSON(t, httpClient, proxy.URL, base+"/pull?after=0", tokens["admin"], "admin", "1", nil, &after)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("失败轮换改变持久序号、封套或变量")
	}
	rotated := environmentValue(cryptox.SignEnvironmentChange(rotation, keyFor(t, f, "admin")))
	result := post("/environment-changes", "admin", rotated)
	if retry := post("/environment-changes", "admin", rotated); !retry.Replayed || retry.Sequence != result.Sequence {
		t.Fatal("环境轮换重试产生新写")
	}
	if status := callJSON(t, httpClient, proxy.URL, base+"/mutations", tokens["writer"], "writer", "1", oldWrite, nil); status != 403 {
		t.Fatalf("旧钥匙版本写仍接受HTTP %d", status)
	}
	if _, err := reader.Pull(ctx); err != nil {
		t.Fatal("读设备新钥匙完整补拉失败", err)
	}
	if got := engine.State().Cloud.Environments["prod"]; got.KeyVersion != 2 || got.GrantGeneration != 2 || got.Values["SYNTHETIC_STACK"] != "synthetic-high" || got.Values["SYNTHETIC_ADDED"] != "synthetic-added" {
		t.Fatal("读设备未验签解新封套和全部重加密值")
	}
	var challenge recoveryChallenge
	if status := callJSON(t, httpClient, proxy.URL, base+"/recovery-challenges", "", "", "", map[string]string{"accountGeneration": "1"}, &challenge); status != 200 {
		t.Fatal(status)
	}
	fields := []string{"harmonia/recovery-proof/v1", f.AccountID, "1", "1", challenge.ChallengeID, challenge.Nonce, strconv.FormatInt(challenge.ExpiresAt, 10)}
	if !reflect.DeepEqual(fields, challenge.SigningPayload) {
		t.Fatal("恢复挑战未绑定本账号")
	}
	var restricted recoverySession
	proof := map[string]string{"accountGeneration": "1", "challengeId": challenge.ChallengeID, "signature": signFields(fields, recovery.SigningPrivate)}
	if status := callJSON(t, httpClient, proxy.URL, base+"/recovery-sessions", "", "", "", proof, &restricted); status != 200 {
		t.Fatal(status)
	}
	var recovered recoveryVault
	if status := callJSON(t, httpClient, proxy.URL, base+"/recovery-vault", restricted.Token, "", "1", nil, &recovered); status != 200 {
		t.Fatal(status)
	}
	if len(recovered.Environments) != 2 {
		t.Fatal("当前恢复封套集合不完整")
	}
	for _, env := range recovered.Environments {
		packet := environmentValue(cryptox.DecodeBase64(env.Envelope, 80, 80))
		key := environmentValue(cryptox.UnwrapEnvironmentKey(recovery.ReceivingPrivate, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: env.EnvironmentID, KeyVersion: env.KeyVersion, RecipientType: "recovery", RecipientID: f.AccountID, RecipientGeneration: "1", RecipientPublicKey: cryptox.EncodeBase64(recovery.ReceivingPublic)}, packet))
		expected := devKey
		if env.EnvironmentID == "prod" {
			expected = rotatedKey
			if env.KeyVersion != "2" {
				t.Fatal("恢复仍给旧版本")
			}
		}
		if !bytes.Equal(key, expected) {
			t.Fatal("恢复封套未与环境新钥原子一致")
		}
	}
	if err := engine.SetOverride("prod", "SYNTHETIC_STACK", "offline-local-override", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := engine.Reconcile(ctx, provider, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := provider.Apply(ctx, []localstate.Change{{Name: "UNRELATED", Value: func() *string { s := "external-unrelated"; return &s }()}}); err != nil {
		t.Fatal(err)
	}
	deletion := change
	deletion.Operation = "delete"
	deletion.AuthorityKeyVersion = "2"
	deletion.AuthorityGrantGeneration = "2"
	deletion.PreviousKeyVersion = "2"
	deletion.KeyVersion = "2"
	deletion.ExpectedSequence = strconv.FormatUint(sequence, 10)
	deletion.IdempotencyKey = "real-delete-prod"
	deletion.LabelPayload = ""
	deletion.Grants = nil
	deletion.Mutations = nil
	if err := engine.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	post("/environment-changes", "admin", environmentValue(cryptox.SignEnvironmentChange(deletion, keyFor(t, f, "admin"))))
	if _, err := reader.RefreshAuthorizations(ctx); err != nil {
		t.Fatal("暂停时删除墓碑未被验证", err)
	}
	if _, exists := engine.State().Cloud.Environments["prod"]; exists {
		t.Fatal("删除后仍保留环境缓存")
	}
	if _, exists := engine.State().Overrides["prod"]; exists {
		t.Fatal("删除后override仍保留来源")
	}
	if err := engine.Reconcile(ctx, provider, time.Now()); err != nil {
		t.Fatal(err)
	}
	values = environmentValue(provider.Snapshot(ctx, []string{"SYNTHETIC_STACK", "SYNTHETIC_ADDED", "UNRELATED"}))
	if values["SYNTHETIC_STACK"] != "synthetic-low" || values["UNRELATED"] != "external-unrelated" {
		t.Fatal("删除未回退剩余激活环境或污染无关修改")
	}
	if _, exists := values["SYNTHETIC_ADDED"]; exists {
		t.Fatal("删除后工具新增项未移除")
	}
	if err := engine.SetPaused(false); err != nil {
		t.Fatal(err)
	}
	if err := engine.Deactivate("dev"); err != nil {
		t.Fatal(err)
	}
	if err := engine.Reconcile(ctx, provider, time.Now()); err != nil {
		t.Fatal(err)
	}
	values = environmentValue(provider.Snapshot(ctx, []string{"SYNTHETIC_STACK", "UNRELATED"}))
	if values["SYNTHETIC_STACK"] != "original-synthetic" || values["UNRELATED"] != "external-unrelated" {
		t.Fatal("全部来源停用未逐key恢复原值")
	}
	if err := engine.Activate("dev", 10, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := engine.SetOverride("dev", "SYNTHETIC_STACK", "local-before-revoke", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := engine.Reconcile(ctx, provider, time.Now()); err != nil {
		t.Fatal(err)
	}
	var revocation cryptox.DeviceRevocation
	if status := callJSON(t, httpClient, proxy.URL, base+"/device-revocations", tokens["admin"], "admin", "1", map[string]string{"subjectDeviceId": "reader", "idempotencyKey": "real-revoke-reader"}, &revocation); status != 200 {
		t.Fatal(status)
	}
	tokenHash := sha256.Sum256([]byte(tokens["admin"]))
	if revocation.AccountID != f.AccountID || revocation.AccountGeneration != "1" || revocation.DeviceID != "admin" || revocation.SubjectDeviceID != "reader" || revocation.SubjectSigningPublicKey != f.Devices["reader"].SigningPublicKey || revocation.SubjectReceivingPublicKey != f.Devices["reader"].ReceivingPublicKey || revocation.SessionHash != hex.EncodeToString(tokenHash[:]) || len(revocation.Authorities) != 1 || revocation.Authorities[0].EnvironmentID != "dev" {
		t.Fatal("撤销挑战不绑定本地确认的账号/设备公钥和权限")
	}
	expiry := environmentValue(strconv.ParseInt(revocation.ExpiresAt, 10, 64))
	if expiry <= time.Now().Unix() || expiry > time.Now().Unix()+125 {
		t.Fatal("撤销挑战期限错误")
	}
	signedRevocation := environmentValue(cryptox.SignDeviceRevocation(revocation, keyFor(t, f, "admin")))
	revoked := post("/device-revocations/complete", "admin", signedRevocation)
	if retry := post("/device-revocations/complete", "admin", signedRevocation); !retry.Replayed || retry.Sequence != revoked.Sequence {
		t.Fatal("撤销重试变成新写")
	}
	if _, err := reader.Pull(ctx); err == nil {
		t.Fatal("撤销后旧设备会话仍可拉取")
	}
	config.Token = ""
	boot := environmentValue(syncclient.NewForBoot(config))
	if _, err := boot.BootDevice(ctx, keyFor(t, f, "reader")); !errors.Is(err, syncclient.ErrTrustInvalidated) {
		t.Fatalf("撤销设备boot未触发缓存停止: %v", err)
	}
	state := engine.State()
	if state.Cloud.AccountID != "" || len(state.Cloud.Environments) != 0 || len(state.Overrides) != 0 || len(state.Managed) != 0 {
		t.Fatal("撤销持钥复查后明文缓存/override未清")
	}
	if err := engine.Reconcile(ctx, provider, time.Now()); err != nil {
		t.Fatal(err)
	}
	values = environmentValue(provider.Snapshot(ctx, []string{"SYNTHETIC_STACK", "UNRELATED"}))
	if values["SYNTHETIC_STACK"] != "original-synthetic" || values["UNRELATED"] != "external-unrelated" {
		t.Fatal("全局撤销未逐key恢复原值且保留无关修改")
	}
	t.Log("通过：真实Go HPKE/AEAD/Ed→HTTPS→TS SQLite创建/改名/完整轮换及恢复封套；遗漏封套原子拒绝，旧版本拒写，reader补拉新值；暂停删除墓碑清override并逐key回退/恢复，全局撤销旧会话与boot失效并清缓存。夹具入网不计真实入网，宿主环境未读取/修改。")
}

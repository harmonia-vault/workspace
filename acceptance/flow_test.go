package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localipc"
	"github.com/harmonia-vault/core-go/localkeys"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 只启动独立测试入口，使用临时 SQLite 与合成设备。TLS 信任仅限 httptest。
type fixture struct {
	Endpoint              string            `json:"endpoint"`
	AccountID             string            `json:"accountId"`
	AccountGeneration     string            `json:"accountGeneration"`
	Email                 string            `json:"email"`
	Credential            string            `json:"credential"`
	EnvironmentID         string            `json:"environmentId"`
	SyntheticSigningSeeds map[string]string `json:"syntheticSigningSeeds"`
	Devices               map[string]struct {
		SigningPublicKey   string `json:"signingPublicKey"`
		ReceivingPublicKey string `json:"receivingPublicKey"`
	} `json:"devices"`
	Grants             []syncclient.SignedGrant `json:"grants"`
	TrustRoot          cryptox.TrustRoot        `json:"trustRoot"`
	RecoveryGeneration string                   `json:"recoveryGeneration"`
}

func startFixture(t *testing.T, arguments ...string) fixture {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(source))
	cmd := exec.Command("node", append([]string{"--import", "tsx", "tests/synthetic-server.ts"}, arguments...)...)
	cmd.Dir = filepath.Join(root, "server")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		t.Fatal("无法启动合成服务器；先安装固定的 server 依赖")
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	})
	ready := make(chan []byte, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- bytes.Clone(scanner.Bytes())
		} else {
			ready <- nil
		}
	}()
	var data []byte
	select {
	case data = <-ready:
	case <-time.After(45 * time.Second):
		t.Fatal("合成服务器启动超时")
	}
	var result fixture
	if len(data) == 0 || json.Unmarshal(data, &result) != nil {
		t.Fatal("合成服务器未返回预期测试元数据")
	}
	endpoint, err := url.Parse(result.Endpoint)
	if err != nil || endpoint.Scheme != "http" || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("测试服务器必须仅绑定 loopback")
	}
	return result
}

func callJSON(t *testing.T, client *http.Client, endpoint, path, token, device, generation string, body any, out any) int {
	t.Helper()
	var reader io.Reader
	method := http.MethodGet
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
		method = http.MethodPost
	}
	request, err := http.NewRequest(method, endpoint+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Harmonia-Device-Id", device)
		request.Header.Set("X-Harmonia-Account-Generation", generation)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal("测试 HTTPS 请求失败")
	}
	defer response.Body.Close()
	if out != nil && response.StatusCode >= 200 && response.StatusCode < 300 {
		if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
			t.Fatal("响应结构无效")
		}
	}
	return response.StatusCode
}

func keyFor(t *testing.T, f fixture, device string) ed25519.PrivateKey {
	t.Helper()
	seed, err := cryptox.DecodeBase64(f.SyntheticSigningSeeds[device], 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func bindDevice(t *testing.T, f fixture, client *http.Client, endpoint, loginToken, device string) string {
	t.Helper()
	base := "/v1/accounts/" + f.AccountID
	var challenge struct {
		ChallengeID    string   `json:"challengeId"`
		Nonce          string   `json:"nonce"`
		ExpiresAt      int64    `json:"expiresAt"`
		SigningPayload []string `json:"signingPayload"`
	}
	if got := callJSON(t, client, endpoint, base+"/device-challenges", loginToken, device, f.AccountGeneration, map[string]any{}, &challenge); got != 200 {
		t.Fatalf("设备挑战 HTTP %d", got)
	}
	hash := sha256.Sum256([]byte(loginToken))
	expected := []string{"harmonia/device-session/v1", f.AccountID, f.AccountGeneration, device, hex.EncodeToString(hash[:]), challenge.ChallengeID, challenge.Nonce, strconv.FormatInt(challenge.ExpiresAt, 10)}
	if !reflect.DeepEqual(expected, challenge.SigningPayload) {
		t.Fatal("服务器挑战未绑定测试账号、设备和登录会话")
	}
	if _, err := cryptox.DecodeBase64(challenge.Nonce, 32, 32); err != nil {
		t.Fatal(err)
	}
	if challenge.ExpiresAt <= time.Now().Unix() || challenge.ExpiresAt > time.Now().Unix()+125 {
		t.Fatal("设备挑战期限无效")
	}
	encoded, _ := json.Marshal(expected)
	proof := map[string]string{"challengeId": challenge.ChallengeID, "signature": cryptox.EncodeBase64(ed25519.Sign(keyFor(t, f, device), encoded))}
	var bound struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expiresAt"`
	}
	if got := callJSON(t, client, endpoint, base+"/device-sessions", loginToken, device, f.AccountGeneration, proof, &bound); got != 200 {
		t.Fatalf("设备持钥证明 HTTP %d", got)
	}
	if got := callJSON(t, client, endpoint, base+"/device-sessions", loginToken, device, f.AccountGeneration, proof, nil); got != 403 {
		t.Fatalf("挑战重放应被拒绝，实际 HTTP %d", got)
	}
	return bound.Token
}

func wireMutation(t *testing.T, m cryptox.Mutation, key ed25519.PrivateKey) syncclient.SignedMutation {
	t.Helper()
	signed, err := cryptox.SignMutation(m, key)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(m)
	var result syncclient.Mutation
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return syncclient.SignedMutation{Mutation: result, Signature: signed.Signature}
}
func wireGrant(t *testing.T, g cryptox.Grant, key ed25519.PrivateKey) syncclient.SignedGrant {
	t.Helper()
	signed, err := cryptox.SignGrant(g, key)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(g)
	var result syncclient.Grant
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return syncclient.SignedGrant{Grant: result, Signature: signed.Signature}
}

func TestGoHTTPSNodeSQLiteVerifiedFlow(t *testing.T) {
	f := startFixture(t)
	backend, err := url.Parse(f.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	// 只在测试内反向代理合成 loopback 服务；正式客户端仍强制 HTTPS。
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(backend))
	defer proxy.Close()
	client := proxy.Client()
	client.Timeout = 15 * time.Second
	var login struct {
		Token string `json:"token"`
	}
	if got := callJSON(t, client, proxy.URL, "/v1/login", "", "", "", map[string]string{"email": f.Email, "credential": f.Credential}, &login); got != 200 {
		t.Fatalf("登录 HTTP %d", got)
	}
	base := "/v1/accounts/" + f.AccountID
	if got := callJSON(t, client, proxy.URL, base+"/pull?after=0", login.Token, "reader", f.AccountGeneration, nil, nil); got != 403 {
		t.Fatalf("仅登录冒用 reader 应拒绝，实际 HTTP %d", got)
	}
	adminToken := bindDevice(t, f, client, proxy.URL, login.Token, "admin")
	writerToken := bindDevice(t, f, client, proxy.URL, login.Token, "writer")
	readerToken := bindDevice(t, f, client, proxy.URL, login.Token, "reader")
	if got := callJSON(t, client, proxy.URL, base+"/pull?after=0", writerToken, "reader", f.AccountGeneration, nil, nil); got != 403 {
		t.Fatalf("设备会话跨设备复用应拒绝，实际 HTTP %d", got)
	}
	if got := callJSON(t, client, proxy.URL, "/v1/accounts/other-account/pull?after=0", writerToken, "writer", f.AccountGeneration, nil, nil); got != 401 {
		t.Fatalf("跨账号会话应拒绝，实际 HTTP %d", got)
	}

	environmentKey, err := cryptox.GenerateEnvironmentKey()
	if err != nil {
		t.Fatal(err)
	}
	var writerGrant cryptox.Grant
	for _, candidate := range f.Grants {
		if candidate.Grant.SubjectDeviceID == "writer" {
			data, _ := json.Marshal(candidate.Grant)
			_ = json.Unmarshal(data, &writerGrant)
		}
	}
	writerGrant.GrantGeneration = "2"
	writerGrant.IdempotencyKey = "e2e-real-hpke-writer"
	envelope, err := cryptox.WrapEnvironmentKey(environmentKey, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: f.AccountGeneration, EnvironmentID: f.EnvironmentID, KeyVersion: "1", RecipientType: "device", RecipientID: "writer", RecipientGeneration: "2", RecipientPublicKey: f.Devices["writer"].ReceivingPublicKey})
	if err != nil {
		t.Fatal(err)
	}
	writerGrant.Envelope = cryptox.EncodeBase64(envelope)
	if got := callJSON(t, client, proxy.URL, base+"/grants", adminToken, "admin", f.AccountGeneration, wireGrant(t, writerGrant, keyFor(t, f, "admin")), nil); got != 200 {
		t.Fatalf("Go 管理签 + HPKE 授权 HTTP %d", got)
	}

	receivingPrivate := bytes.Repeat([]byte{8}, 32)
	verifier, err := syncclient.NewPinnedVerifier(syncclient.PinnedTrust{AccountID: f.AccountID, AccountGeneration: 1, DeviceID: "writer", DeviceSigningPublicKey: keyFor(t, f, "writer").Public().(ed25519.PublicKey), ReceivingPrivateKey: receivingPrivate, Managers: map[string]ed25519.PublicKey{"admin": keyFor(t, f, "admin").Public().(ed25519.PublicKey)}})
	if err != nil {
		t.Fatal(err)
	}
	// 真实 encrypted Store 与原生本地 IPC，所有配置仍只写临时目录。
	temporaryRoot, err := os.MkdirTemp("/tmp", "harmonia-e2e-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(temporaryRoot) })
	privateRoot, err := filepath.EvalSymlinks(temporaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	userID, err := localkeys.CurrentUserID()
	if err != nil {
		t.Fatal(err)
	}
	storeConfig := localkeys.Config{Directory: filepath.Join(privateRoot, "protected-state"), UserID: userID}
	store, err := localkeys.OpenEncryptedStateStore(storeConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if duplicate, err := localkeys.OpenEncryptedStateStore(storeConfig); err == nil {
		duplicate.Close()
		t.Fatal("第二个进程状态拥有者未被拒绝")
	}
	// 此旧流程明确使用测试夹具的固定设备和已知管理签名；它只测试下发，
	// 不计为入网成功。enrollment_test.go 另验真实首次初始化与 SPAKE2 入网。
	receivingPublic, err := cryptox.DecodeBase64(f.Devices["writer"].ReceivingPublicKey, 32, 32)
	if err != nil {
		t.Fatal(err)
	}
	fixtureKeys := localkeys.DeviceKeys{DeviceID: "writer", SigningSeed: keyFor(t, f, "writer").Seed(), SigningPublic: keyFor(t, f, "writer").Public().(ed25519.PublicKey), ReceivingPrivate: receivingPrivate, ReceivingPublic: receivingPublic}
	if err = store.Vault().SaveDeviceKeys(fixtureKeys); err != nil {
		t.Fatal(err)
	}
	fixtureProof, _ := json.Marshal(map[string]any{"syntheticFixture": true, "grant": wireGrant(t, writerGrant, keyFor(t, f, "admin"))})
	if err = store.Vault().SaveTrustContext(localkeys.TrustContext{Endpoint: proxy.URL, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: "writer", SigningPublic: fixtureKeys.SigningPublic, ReceivingPublic: receivingPublic, Managers: map[string][]byte{"admin": keyFor(t, f, "admin").Public().(ed25519.PublicKey)}, PairingProfile: localkeys.EnrollmentPairingProfile, EnrollmentCertificate: fixtureProof, EnrollmentKey: "synthetic-fixture-pins", Accepted: true}); err != nil {
		t.Fatal(err)
	}
	engine, err := localstate.New(store)
	if err != nil {
		t.Fatal(err)
	}
	providerPath := filepath.Join(privateRoot, "synthetic-provider.json")
	initialProvider, _ := json.Marshal(map[string]string{"SYNTHETIC_E2E": "original-synthetic", "UNRELATED": "keep-synthetic"})
	if err = os.WriteFile(providerPath, initialProvider, 0600); err != nil {
		t.Fatal(err)
	}
	provider := &localstate.FileProvider{Path: providerPath}
	endpoint := localipc.Endpoint{Directory: filepath.Join(privateRoot, "ipc"), UserID: userID}
	background, err := localipc.Listen(localipc.Config{Endpoint: endpoint, Engine: engine, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	ipcContext, cancelIPC := context.WithCancel(context.Background())
	ipcDone := make(chan error, 1)
	go func() { ipcDone <- background.Serve(ipcContext) }()
	defer func() {
		cancelIPC()
		background.Close()
		if err := <-ipcDone; err != nil {
			t.Error(err)
		}
	}()
	callIPC := func(command string) localipc.Response {
		t.Helper()
		response, err := localipc.Call(context.Background(), endpoint, localipc.Request{Command: command})
		if err != nil || !response.OK {
			t.Fatalf("本地 IPC %s 失败: %v", command, err)
		}
		return response
	}
	loginClient, err := syncclient.New(syncclient.Config{Endpoint: proxy.URL, HTTPClient: client, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: "writer", Token: login.Token, Verifier: verifier, Engine: engine})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = loginClient.Pull(context.Background()); err == nil {
		t.Fatal("正式客户端仅登录会话不得拉取")
	}
	synced, err := loginClient.BindDevice(context.Background(), keyFor(t, f, "writer"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = synced.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	bootClient, err := syncclient.NewForBoot(syncclient.Config{Endpoint: proxy.URL, HTTPClient: client, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: "writer", Verifier: verifier, Engine: engine})
	if err != nil {
		t.Fatal(err)
	}
	synced, err = bootClient.BootDevice(context.Background(), keyFor(t, f, "writer"))
	if err != nil {
		t.Fatal("无登录凭据的已授权设备 boot 证明失败", err)
	}
	if err = engine.Activate(f.EnvironmentID, 10, time.Now()); err != nil {
		t.Fatal(err)
	}
	valueContext := cryptox.ValueContext{AccountID: f.AccountID, AccountGeneration: f.AccountGeneration, EnvironmentID: f.EnvironmentID, KeyVersion: "1", Name: "SYNTHETIC_E2E"}
	packet, err := cryptox.EncryptValue(environmentKey, valueContext, []byte("synthetic-env-value"))
	if err != nil {
		t.Fatal(err)
	}
	mutation := cryptox.Mutation{AccountID: f.AccountID, AccountGeneration: f.AccountGeneration, DeviceID: "writer", EnvironmentID: f.EnvironmentID, KeyVersion: "1", GrantGeneration: "2", Operation: "put", IdempotencyKey: "e2e-write-1", Name: "SYNTHETIC_E2E", Payload: cryptox.EncodeBase64(packet)}
	signed := wireMutation(t, mutation, keyFor(t, f, "writer"))
	result, err := synced.Submit(context.Background(), signed)
	if err != nil || !result.Applied {
		t.Fatalf("签名写入/验证下发失败: %v", err)
	}
	effective, err := engine.Effective(time.Now())
	if err != nil || effective[mutation.Name] != "synthetic-env-value" {
		t.Fatal("HPKE 解封 + AEAD 解密后未形成正确本机值")
	}
	if err = background.Reconcile(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if callIPC("export").Values[mutation.Name] != "synthetic-env-value" {
		t.Fatal("CLI IPC 未读到后台已验签下发值")
	}
	onDisk, err := os.ReadFile(filepath.Join(storeConfig.Directory, "state.v1.enc"))
	if err != nil || bytes.Contains(onDisk, []byte("synthetic-env-value")) {
		t.Fatal("服务状态中发现合成明文或无法读取密文")
	}
	retried, err := synced.Submit(context.Background(), signed)
	if err != nil || !retried.Accepted.Replayed || retried.Accepted.Sequence != result.Accepted.Sequence {
		t.Fatal("同幂等写重试未保留原序号")
	}
	mutation.Payload = cryptox.EncodeBase64(bytes.Repeat([]byte{7}, 40))
	if got := callJSON(t, client, proxy.URL, base+"/mutations", writerToken, "writer", f.AccountGeneration, wireMutation(t, mutation, keyFor(t, f, "writer")), nil); got != 409 {
		t.Fatalf("同 ID 不同内容应拒绝，实际 HTTP %d", got)
	}
	mutation.DeviceID = "reader"
	mutation.GrantGeneration = "1"
	mutation.IdempotencyKey = "e2e-reader-write"
	if got := callJSON(t, client, proxy.URL, base+"/mutations", readerToken, "reader", f.AccountGeneration, wireMutation(t, mutation, keyFor(t, f, "reader")), nil); got != 403 {
		t.Fatalf("RO 即使能签密文也不得写，实际 HTTP %d", got)
	}

	callIPC("pause")
	externalProvider, _ := json.Marshal(map[string]string{"SYNTHETIC_E2E": "external-synthetic", "UNRELATED": "external-unrelated"})
	if err = os.WriteFile(providerPath, externalProvider, 0600); err != nil {
		t.Fatal(err)
	}
	if err = background.Reconcile(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	pausedValues, err := provider.Snapshot(context.Background(), []string{"SYNTHETIC_E2E", "UNRELATED"})
	if err != nil || pausedValues[mutation.Name] != "external-synthetic" || pausedValues["UNRELATED"] != "external-unrelated" {
		t.Fatal("暂停期间仍纠正外部修改")
	}

	writerGrant.GrantGeneration = "3"
	writerGrant.Role = "none"
	writerGrant.Envelope = ""
	writerGrant.IdempotencyKey = "e2e-revoke-writer"
	if got := callJSON(t, client, proxy.URL, base+"/grants", adminToken, "admin", f.AccountGeneration, wireGrant(t, writerGrant, keyFor(t, f, "admin")), nil); got != 200 {
		t.Fatalf("管理签撤销 HTTP %d", got)
	}
	if _, err = synced.AcceptRevocationHint(context.Background()); err != nil {
		t.Fatal(err)
	}
	effective, err = engine.Effective(time.Now())
	if err != nil || len(effective) != 0 {
		t.Fatal("收到撤销后旧值仍生效")
	}
	if err = background.Reconcile(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	restoredValues, err := provider.Snapshot(context.Background(), []string{"SYNTHETIC_E2E", "UNRELATED"})
	if err != nil || restoredValues[mutation.Name] != "original-synthetic" || restoredValues["UNRELATED"] != "external-unrelated" {
		t.Fatal("暂停时撤销未逐 key 恢复原值，或抹掉无关外部修改")
	}
	if len(callIPC("export").Values) != 0 {
		t.Fatal("撤销后 IPC 仍暴露旧来源")
	}
	if _, err = synced.Submit(context.Background(), signed); err == nil {
		t.Fatal("撤销后重试不得绕过当前权限")
	}

	// 重新授权后，旧 checkpoint 之后没有新的 put；客户端仍须补拉旧值。
	writerGrant.GrantGeneration = "4"
	writerGrant.Role = "rw"
	writerGrant.IdempotencyKey = "e2e-regrant-writer"
	envelope, err = cryptox.WrapEnvironmentKey(environmentKey, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: f.AccountGeneration, EnvironmentID: f.EnvironmentID, KeyVersion: "1", RecipientType: "device", RecipientID: "writer", RecipientGeneration: "4", RecipientPublicKey: f.Devices["writer"].ReceivingPublicKey})
	if err != nil {
		t.Fatal(err)
	}
	writerGrant.Envelope = cryptox.EncodeBase64(envelope)
	if got := callJSON(t, client, proxy.URL, base+"/grants", adminToken, "admin", f.AccountGeneration, wireGrant(t, writerGrant, keyFor(t, f, "admin")), nil); got != 200 {
		t.Fatalf("重新授权 HTTP %d", got)
	}
	callIPC("resume")
	if _, err = synced.Pull(context.Background()); err != nil {
		t.Fatal(err)
	}
	effective, err = engine.Effective(time.Now())
	if err != nil || effective["SYNTHETIC_E2E"] != "synthetic-env-value" {
		t.Fatal("重新授权未补拉旧序号中的环境值")
	}
	t.Log("通过：Go 管理签/HPKE/AEAD → HTTPS → TypeScript 当前权限 → SQLite 序号 → Go 验签解密 → 加密持久状态 → 原生 IPC/隔离 provider；持钥挑战、防重放、RO、幂等、暂停与撤销逐 key 恢复。")
}

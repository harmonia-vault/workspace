package acceptance

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestMobileWorkflowRealInitializationCRUDSealedResumeAndUnknownResults(t *testing.T) {
	ctx := context.Background()
	fixture := startFixture(t, "--capture-email")
	backend := environmentValue(url.Parse(fixture.Endpoint))
	proxyHandler := httputil.NewSingleHostReverseProxy(backend)
	var loseInit, loseMutation, loseEnvironment atomic.Bool
	proxyHandler.ModifyResponse = func(response *http.Response) error {
		path := response.Request.URL.Path
		lose := false
		if response.Request.Method == "POST" && response.StatusCode == 200 {
			if strings.HasSuffix(path, "/vault-initializations/workflow-init/complete") {
				lose = loseInit.Swap(false)
			} else if strings.HasSuffix(path, "/mutations") {
				lose = loseMutation.Swap(false)
			} else if strings.HasSuffix(path, "/environment-changes") {
				lose = loseEnvironment.Swap(false)
			}
		}
		if lose {
			_ = response.Body.Close()
			body := []byte(`{"error":"synthetic_lost_response"}`)
			response.StatusCode = 502
			response.Body = io.NopCloser(bytes.NewReader(body))
			response.ContentLength = int64(len(body))
			response.Header.Set("Content-Length", strconv.Itoa(len(body)))
		}
		return nil
	}
	proxy := httptest.NewTLSServer(proxyHandler)
	defer proxy.Close()
	client := proxy.Client()
	_, signing, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, receiving, err := cryptox.GenerateReceivingKey()
	if err != nil {
		t.Fatal(err)
	}
	sealKey := make([]byte, 32)
	if _, err = rand.Read(sealKey); err != nil {
		t.Fatal(err)
	}
	block := environmentValue(aes.NewCipher(sealKey))
	gcm := environmentValue(cipher.NewGCM(block))
	var sealed []byte
	failSave := false
	// 仅为Go测试的原生保护边界替身；不是Android Keystore或真实生物认证验收。
	save := func(plain []byte) error {
		if failSave {
			return errors.New("synthetic native seal failure")
		}
		nonce := make([]byte, gcm.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		sealed = gcm.Seal(nonce, nonce, plain, []byte("synthetic-native-workflow-state-v1"))
		return nil
	}
	load := func() []byte {
		if len(sealed) < gcm.NonceSize() {
			t.Fatal("no sealed context")
		}
		return environmentValue(gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte("synthetic-native-workflow-state-v1")))
	}
	config := mobileworkflow.Config{Endpoint: proxy.URL, HTTPClient: client, SigningKey: signing, ReceivingPrivateKey: receiving, SaveProtectedState: save}
	workflow := environmentValue(mobileworkflow.New(config))
	defer func() { workflow.Close() }()
	email, password := "mobile-workflow@example.invalid", "synthetic-password-only-for-test"
	registered := environmentValue(workflow.Register(ctx, email, password))
	if !registered.VerificationRequired {
		t.Fatal("registration email policy bypassed")
	}
	var mail []struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Text    string `json:"text"`
	}
	if status := callJSON(t, client, proxy.URL, "/test/emails", "", "", "", nil, &mail); status != 200 {
		t.Fatal(status)
	}
	var proof mobileworkflow.EmailProof
	for _, message := range mail {
		if message.To == email {
			for _, line := range strings.Split(message.Text, "\n") {
				if strings.HasPrefix(line, "{") {
					if json.Unmarshal([]byte(line), &proof) != nil {
						t.Fatal("synthetic email proof invalid")
					}
				}
			}
		}
	}
	if proof.AccountID != registered.AccountID || proof.Token == "" {
		t.Fatal("synthetic verification proof missing")
	}
	if err = workflow.VerifyEmail(ctx, proof); err != nil {
		t.Fatal(err)
	}
	if err = workflow.Login(ctx, email, password); err != nil {
		t.Fatal(err)
	}
	code := environmentValue(workflow.BeginInitialization(ctx, "初始合成环境", "workflow-init"))
	if len(code) != 52 {
		t.Fatal("complete recovery seed code unavailable")
	}
	if _, err = workflow.CompleteInitialization(ctx, code[:len(code)-1]); err == nil {
		t.Fatal("partial recovery code accepted")
	}
	loseInit.Store(true)
	if _, err = workflow.CompleteInitialization(ctx, code); !errors.Is(err, mobileworkflow.ErrPending) {
		t.Fatalf("lost accepted init result not pending: %v", err)
	}
	pending := load()
	if bytes.Contains(pending, []byte(code)) || bytes.Contains(pending, []byte(password)) {
		t.Fatal("recovery code/password persisted in native context")
	}
	workflow.Close()
	config.ProtectedState = pending
	workflow = environmentValue(mobileworkflow.New(config))
	view := environmentValue(workflow.CompleteInitialization(ctx, code))
	if len(view.Environments) != 1 || view.Environments[0].Name != "初始合成环境" {
		t.Fatal("real initial root and verified name not shown")
	}
	initialID := view.Environments[0].ID
	loseEnvironment.Store(true)
	if _, err = workflow.CreateEnvironment(ctx, "第二合成环境", "workflow-create"); err == nil {
		t.Fatal("lost environment response incorrectly succeeded")
	}
	workflow.Close()
	config.ProtectedState = load()
	workflow = environmentValue(mobileworkflow.New(config))
	view = environmentValue(workflow.CreateEnvironment(ctx, "第二合成环境", "workflow-create"))
	if len(view.Environments) != 2 {
		t.Fatal("same environment request retry created duplicate or lost accepted env")
	}
	secondID := ""
	for _, env := range view.Environments {
		if env.Name == "第二合成环境" {
			secondID = env.ID
		}
	}
	if secondID == "" {
		t.Fatal("new environment name missing")
	}
	before := view.Checkpoint
	if _, err = workflow.CreateEnvironment(ctx, "不同意图", "workflow-create"); !errors.Is(err, syncclient.ErrWriteConflict) {
		t.Fatal("same request id changed intent accepted", err)
	}
	view = environmentValue(workflow.Pull(ctx))
	if view.Checkpoint != before {
		t.Fatal("conflicting retry became new write")
	}
	loseMutation.Store(true)
	if _, err = workflow.SetVariable(ctx, secondID, "SYNTHETIC_MOBILE_KEY", "synthetic-workflow-value", "workflow-put"); err == nil {
		t.Fatal("lost variable accepted result incorrectly succeeded")
	}
	workflow.Close()
	config.ProtectedState = load()
	workflow = environmentValue(mobileworkflow.New(config))
	view = environmentValue(workflow.SetVariable(ctx, secondID, "SYNTHETIC_MOBILE_KEY", "synthetic-workflow-value", "workflow-put"))
	found := false
	for _, env := range view.Environments {
		if env.ID == secondID && env.Variables["SYNTHETIC_MOBILE_KEY"] == "synthetic-workflow-value" {
			found = true
		}
	}
	if !found {
		t.Fatal("resumed shared signed ciphertext retry failed verified pull")
	}
	before = view.Checkpoint
	view = environmentValue(workflow.SetVariable(ctx, secondID, "SYNTHETIC_MOBILE_KEY", "synthetic-workflow-value", "workflow-put"))
	if view.Checkpoint != before {
		t.Fatal("retry regenerated random nonce or server sequence")
	}
	view = environmentValue(workflow.RenameEnvironment(ctx, secondID, "改名后的合成环境", "workflow-rename"))
	view = environmentValue(workflow.DeleteVariable(ctx, secondID, "SYNTHETIC_MOBILE_KEY", "workflow-delete-variable"))
	for _, env := range view.Environments {
		if env.ID == secondID && len(env.Variables) != 0 {
			t.Fatal("variable delete not authoritatively applied")
		}
	}
	failSave = true
	if _, err = workflow.SetVariable(ctx, initialID, "SHOULD_NOT_UPLOAD", "synthetic-only", "workflow-seal-failure"); err == nil {
		t.Fatal("native persistence failure did not stop write")
	}
	failSave = false
	view = environmentValue(workflow.Pull(ctx))
	for _, env := range view.Environments {
		if _, ok := env.Variables["SHOULD_NOT_UPLOAD"]; ok {
			t.Fatal("write sent before encrypted journal saved")
		}
	}
	view = environmentValue(workflow.DeleteEnvironment(ctx, secondID, "workflow-delete-env"))
	if len(view.Environments) != 1 || view.Environments[0].ID != initialID {
		t.Fatal("signed environment delete not applied")
	}
	state := load()
	var protected map[string]json.RawMessage
	if json.Unmarshal(state, &protected) != nil {
		t.Fatal("protected context malformed")
	}
	if _, ok := protected["pending"]; ok {
		t.Fatal("consumed init login token retained")
	}
	if bytes.Contains(state, []byte(password)) || bytes.Contains(state, signing.Seed()) || bytes.Contains(state, receiving) {
		t.Fatal("private keys/password leaked to context")
	}
	hash := cryptox.PasswordCredential(password)
	if bytes.Contains(state, []byte(hex.EncodeToString(hash[:]))) {
		t.Fatal("credential persisted")
	}
	workflow.Close()
	config.ProtectedState = state
	workflow = environmentValue(mobileworkflow.New(config))
	offline := environmentValue(workflow.View())
	if !reflect.DeepEqual(offline.Environments, view.Environments) {
		t.Fatal("sealed verified cache changed while offline")
	}
	if err = workflow.ApproveDevice(ctx, "12345678"); !errors.Is(err, mobileworkflow.ErrUnsupported) {
		t.Fatal("unimplemented phone approval falsely succeeded")
	}
	if err = workflow.Recover(ctx, code); !errors.Is(err, mobileworkflow.ErrUnsupported) {
		t.Fatal("unimplemented phone recovery falsely succeeded")
	}
	// 当前业务层不暴露session给Dart；测试原生持钥代码从同一已确认本地根建立独立设备绑定会话。
	public := signing.Public().(ed25519.PublicKey)
	// 合成测试直接走同一boot协议取得自己的随机token；没有生产token导出接口。
	token := mobileWorkflowBootToken(t, client, proxy.URL, registered.AccountID, view.DeviceID, signing, receiving)
	var revoke cryptox.DeviceRevocation
	base := "/v1/accounts/" + registered.AccountID
	if status := callJSON(t, client, proxy.URL, base+"/device-revocations", token, view.DeviceID, "1", map[string]string{"subjectDeviceId": view.DeviceID, "idempotencyKey": "workflow-self-revoke"}, &revoke); status != 200 {
		t.Fatal("revocation challenge", status)
	}
	tokenHash := sha256.Sum256([]byte(token))
	if revoke.AccountID != registered.AccountID || revoke.AccountGeneration != "1" || revoke.DeviceID != view.DeviceID || revoke.SubjectDeviceID != view.DeviceID || revoke.SubjectSigningPublicKey != cryptox.EncodeBase64(public) || revoke.SessionHash != hex.EncodeToString(tokenHash[:]) {
		t.Fatal("revocation binding changed")
	}
	signed := environmentValue(cryptox.SignDeviceRevocation(revoke, signing))
	var accepted syncclient.Acceptance
	if status := callJSON(t, client, proxy.URL, base+"/device-revocations/complete", token, view.DeviceID, "1", signed, &accepted); status != 200 {
		t.Fatal("revocation not accepted", status)
	}
	if _, err = workflow.Pull(ctx); !errors.Is(err, syncclient.ErrTrustInvalidated) {
		t.Fatal("old session401 then boot403 did not invalidate native workflow", err)
	}
	if _, err = workflow.View(); !errors.Is(err, mobileworkflow.ErrClosed) {
		t.Fatal("revoked authenticated operation still returns cache", err)
	}
	invalidated := load()
	var closed struct {
		Root  *cryptox.TrustRoot `json:"root"`
		Cloud localstate.State   `json:"cloud"`
	}
	if json.Unmarshal(invalidated, &closed) != nil || closed.Root != nil || !closed.Cloud.AccountClosed || len(closed.Cloud.Cloud.Environments) != 0 || bytes.Contains(invalidated, []byte("synthetic-workflow-value")) {
		t.Fatal("revocation did not persist cleared closed context")
	}
	config.ProtectedState = invalidated
	workflow = environmentValue(mobileworkflow.New(config))
	if _, err = workflow.View(); !errors.Is(err, mobileworkflow.ErrNotTrusted) {
		t.Fatal("revoked closed native context revived trust", err)
	}
	if err = workflow.Login(ctx, email, password); err == nil {
		t.Fatal("closed context login silently revived trusted capability")
	}
	t.Log("通过：真实注册/合成捕获邮件验证→SHA256登录→完整恢复码重输双签首机初始化→boot+同验签pull→环境/变量CRUD；初始化/创建/变量写接受后502，AES密封context重启查询同id不重加密、不新增seq；原生seal失败先拒写，离线已验缓存可读。未声称Android持久层已接高层，未实现批准/恢复继续拒绝。")
}

func mobileWorkflowBootToken(t *testing.T, client *http.Client, endpoint, account, device string, signing ed25519.PrivateKey, receiving []byte) string {
	t.Helper()
	base := "/v1/accounts/" + account
	var challenge syncclient.DeviceChallenge
	if status := callJSON(t, client, endpoint, base+"/boot-challenges", "", "", "", map[string]string{"deviceId": device, "accountGeneration": "1"}, &challenge); status != 200 {
		t.Fatal(status)
	}
	receive := environmentValue(ecdh.X25519().NewPrivateKey(receiving))
	proof := environmentValue(cryptox.NewDeviceBootProof(account, "1", device, signing.Public().(ed25519.PublicKey), receive.PublicKey().Bytes(), challenge.ChallengeID, challenge.Nonce, strconv.FormatInt(challenge.ExpiresAt, 10)))
	bytes := environmentValue(proof.SigningBytes())
	var fields []string
	_ = json.Unmarshal(bytes, &fields)
	if !reflect.DeepEqual(fields, challenge.SigningPayload) {
		t.Fatal("boot challenge not locallybound")
	}
	signature := environmentValue(cryptox.SignDeviceBootProof(proof, signing))
	var session syncclient.DeviceSession
	if status := callJSON(t, client, endpoint, base+"/boot-sessions", "", "", "", map[string]string{"deviceId": device, "accountGeneration": "1", "challengeId": challenge.ChallengeID, "signature": signature}, &session); status != 200 {
		t.Fatal(status)
	}
	return session.Token
}

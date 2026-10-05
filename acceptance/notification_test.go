package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 实际Go客户端经HTTPS/WSS代理连接正式TS通知适配和临时SQLite。
// 已知合成设备只用于传输/权限/验证拉取验收，不代替真实PAKE入网测试。
func TestGoWSSNodeSQLiteNotificationReconnectPauseAndRevocation(t *testing.T) {
	f := startFixture(t)
	backend, err := url.Parse(f.Endpoint)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(backend))
	defer proxy.Close()
	httpClient := proxy.Client()
	httpClient.Timeout = 15 * time.Second
	var login struct {
		Token string `json:"token"`
	}
	if code := callJSON(t, httpClient, proxy.URL, "/v1/login", "", "", "", map[string]string{"email": f.Email, "credential": f.Credential}, &login); code != 200 {
		t.Fatalf("合成登录HTTP %d", code)
	}
	adminToken := bindDevice(t, f, httpClient, proxy.URL, login.Token, "admin")
	writerToken := bindDevice(t, f, httpClient, proxy.URL, login.Token, "writer")
	base := "/v1/accounts/" + f.AccountID
	key, err := cryptox.GenerateEnvironmentKey()
	if err != nil {
		t.Fatal(err)
	}
	defer clear(key)
	var grant cryptox.Grant
	for _, g := range f.Grants {
		if g.Grant.SubjectDeviceID == "writer" {
			grant = g.Grant
		}
	}
	grant.GrantGeneration = "2"
	grant.IdempotencyKey = "notification-real-hpke"
	envelope, err := cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", RecipientType: "device", RecipientID: "writer", RecipientGeneration: "2", RecipientPublicKey: grant.SubjectReceivingPublicKey})
	if err != nil {
		t.Fatal(err)
	}
	grant.Envelope = cryptox.EncodeBase64(envelope)
	if code := callJSON(t, httpClient, proxy.URL, base+"/grants", adminToken, "admin", "1", wireGrant(t, grant, keyFor(t, f, "admin")), nil); code != 200 {
		t.Fatalf("真实HPKE授权HTTP %d", code)
	}
	store, err := localstate.OpenFileStore(filepath.Join(t.TempDir(), "private", "synthetic-state.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	engine, err := localstate.New(store)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := syncclient.NewPinnedVerifierV5(syncclient.IssuerDAGPinnedTrust{AccountID: f.AccountID, AccountGeneration: 1, DeviceID: "writer", DeviceSigningPublicKey: keyFor(t, f, "writer").Public().(ed25519.PublicKey), ReceivingPrivateKey: bytes.Repeat([]byte{9}, 32), Receipt: syncclient.EnrollmentReceiptV5{IdempotencyKey: "fixture-writer", Approval: f.DAGEnrollments["writer"]}})
	if err != nil {
		t.Fatal(err)
	}
	defer verifier.Close()
	config := syncclient.Config{Endpoint: proxy.URL, HTTPClient: httpClient, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: "writer", Token: writerToken, Engine: engine, Verifier: verifier}
	client, err := syncclient.New(config)
	if err != nil {
		t.Fatal(err)
	}
	read := func(subscription *syncclient.NotificationSubscription) syncclient.NotificationHint {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		hint, e := subscription.Read(ctx)
		if e != nil {
			t.Fatal(e)
		}
		return hint
	}
	pull := func() {
		t.Helper()
		if _, e := client.Pull(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	mutate := func(index int, value string) uint64 {
		t.Helper()
		packet, e := cryptox.EncryptValue(key, cryptox.ValueContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", Name: "WS_SYNTHETIC"}, []byte(value))
		if e != nil {
			t.Fatal(e)
		}
		m := wireMutation(t, cryptox.Mutation{AccountID: f.AccountID, AccountGeneration: "1", DeviceID: "writer", EnvironmentID: "dev", KeyVersion: "1", GrantGeneration: "2", Operation: "put", IdempotencyKey: "notification-write-" + strconv.Itoa(index), Name: "WS_SYNTHETIC", Payload: cryptox.EncodeBase64(packet)}, keyFor(t, f, "writer"))
		var accepted syncclient.Acceptance
		if code := callJSON(t, httpClient, proxy.URL, base+"/mutations", writerToken, "writer", "1", m, &accepted); code != 200 {
			t.Fatalf("实际签写HTTP %d", code)
		}
		return accepted.Sequence
	}
	subscription, err := client.OpenNotifications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if hint := read(subscription); hint.Sequence != 4 || engine.State().Cloud.Sequence != 0 {
		t.Fatal("初始通知直接推进检查点")
	}
	pull()
	sequence := mutate(1, "first-wss-synthetic")
	if hint := read(subscription); hint.Sequence != sequence || engine.State().Cloud.Sequence != 4 {
		t.Fatal("持久提交通知错误或直接应用了数据")
	}
	pull()
	if engine.State().Cloud.Environments["dev"].Values["WS_SYNTHETIC"] != "first-wss-synthetic" {
		t.Fatal("通知后的真实验签解密失败")
	}
	subscription.Close()
	sequence = mutate(2, "missed-wss-synthetic")
	subscription, err = client.OpenNotifications(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()
	if hint := read(subscription); hint.Sequence != sequence || engine.State().Cloud.Sequence != 5 {
		t.Fatal("重连初始提示未补漏边界")
	}
	pull()
	if engine.State().Cloud.Environments["dev"].Values["WS_SYNTHETIC"] != "missed-wss-synthetic" {
		t.Fatal("断线期间的持久值未补拉")
	}
	if err = engine.Activate("dev", 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = engine.SetOverride("dev", "WS_SYNTHETIC", "local-wss-synthetic", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = engine.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	sequence = mutate(3, "paused-wss-synthetic")
	if hint := read(subscription); hint.Sequence != sequence {
		t.Fatal("暂停期间通知缺失")
	}
	if _, err = client.RefreshAuthorizations(context.Background()); err != nil {
		t.Fatal(err)
	}
	state := engine.State()
	if state.Cloud.Sequence != 6 || state.Cloud.AuthorizationSequence != sequence || state.Cloud.Environments["dev"].Values["WS_SYNTHETIC"] != "missed-wss-synthetic" {
		t.Fatal("暂停提示应用了普通值或推进了数据检查点")
	}
	if err = engine.SetPaused(false); err != nil {
		t.Fatal(err)
	}
	pull()
	if engine.State().Cloud.Environments["dev"].Values["WS_SYNTHETIC"] != "paused-wss-synthetic" {
		t.Fatal("恢复未全量补漏")
	}
	if err = engine.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	revoked := grant
	revoked.Role = "none"
	revoked.Envelope = ""
	revoked.GrantGeneration = "3"
	revoked.IdempotencyKey = "notification-revoke"
	if code := callJSON(t, httpClient, proxy.URL, base+"/grants", adminToken, "admin", "1", wireGrant(t, revoked, keyFor(t, f, "admin")), nil); code != 200 {
		t.Fatalf("实际撤销HTTP %d", code)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_, err = subscription.Read(closeCtx)
	cancel()
	if !errors.Is(err, syncclient.ErrNotificationAuthorization) {
		t.Fatal("服务未逐次检查当前授权并关闭4003", err)
	}
	if len(engine.State().Cloud.Environments) != 1 {
		t.Fatal("未验证的WS关闭直接执行了撤销")
	}
	if _, err = client.RefreshAuthorizations(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(engine.State().Cloud.Environments) != 0 || len(engine.State().Overrides) != 0 {
		t.Fatal("暂停时实际签授权撤销未清缓存/override")
	}
	config.Token = ""
	boot, err := syncclient.NewForBoot(config)
	if err != nil {
		t.Fatal(err)
	}
	_, err = boot.BootDevice(context.Background(), keyFor(t, f, "writer"))
	var fault *syncclient.RequestError
	if !errors.Is(err, syncclient.ErrTrustInvalidated) || !errors.As(err, &fault) || fault.Code != "no_current_grant" || engine.State().AccountClosed {
		t.Fatal("无当前授权Boot处理不准确", err)
	}
	t.Log("通过：真实Go→HTTPS/WSS→TS/SQLite票据和逐次授权；断线漏通知按持久序号补拉，暂停只授权投影，恢复重建，4003再验签撤销和无当前授权Boot；全程合成数据。")
}

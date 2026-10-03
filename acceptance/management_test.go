package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localkeys"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/pairing"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 所有设备经真实双向 native PAKE 接受，测试不注入服务端受信设备。
func TestNativeMobileDeviceGrantManagementSealedRetryAndOtherRevocation(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("须固定 BoringSSL 原生构建；默认构建未运行真实配对验收")
	}
	ctx := context.Background()
	fixture := startFixture(t, "--empty-vault", "--capture-email")
	f := &originFixture{t: t, email: "management@example.invalid", password: "synthetic-management-password"}
	var loseGrant, loseRevoke, failSeal atomic.Bool
	var grantPosts, revokePosts atomic.Int64
	var tamper atomic.Int64
	var targetID string
	backend := environmentValue(url.Parse(fixture.Endpoint))
	proxy := httputil.NewSingleHostReverseProxy(backend)
	proxy.ModifyResponse = func(r *http.Response) error {
		path := r.Request.URL.Path
		if r.StatusCode == 200 && r.Request.Method == "GET" && strings.HasSuffix(path, "/grant-management") && tamper.Load() != 0 {
			body, e := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if e != nil {
				return e
			}
			var c syncclient.ManagementControl
			if e = json.Unmarshal(body, &c); e != nil {
				return e
			}
			for i := range c.Subjects {
				if c.Subjects[i].DeviceID == targetID {
					if tamper.Load() == 1 {
						c.Subjects[i].SigningPublicKey = cryptox.EncodeBase64(make([]byte, 32))
					} else {
						c.Subjects[i].CurrentGrant = nil
						c.Subjects[i].HighestGrantGeneration = "0"
					}
				}
			}
			data, e := json.Marshal(c)
			if e != nil {
				return e
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
			r.ContentLength = int64(len(data))
			r.Header.Set("Content-Length", strconv.Itoa(len(data)))
		}
		lose := false
		if r.StatusCode == 200 && r.Request.Method == "POST" {
			if strings.HasSuffix(path, "/grants") {
				grantPosts.Add(1)
				lose = loseGrant.Swap(false)
			}
			if strings.HasSuffix(path, "/device-revocations/complete") {
				revokePosts.Add(1)
				lose = loseRevoke.Swap(false)
			}
		}
		if lose {
			_ = r.Body.Close()
			data := []byte(`{"error":"request_rejected"}`)
			r.StatusCode = 504
			r.Body = io.NopCloser(bytes.NewReader(data))
			r.ContentLength = int64(len(data))
			r.Header.Set("Content-Length", strconv.Itoa(len(data)))
		}
		return nil
	}
	f.proxy = httptest.NewTLSServer(proxy)
	defer f.proxy.Close()
	keys := environmentValue(localkeys.GenerateDeviceKeys("management-root-placeholder"))
	key := ed25519.NewKeyFromSeed(keys.SigningSeed)
	defer clear(key)
	seal, load := recoveryNativeSeal(t, "synthetic-management-native-state")
	save := func(data []byte) error {
		var s struct {
			Management *struct {
				Pending json.RawMessage `json:"pending"`
			} `json:"management"`
		}
		if json.Unmarshal(data, &s) != nil {
			return errors.New("synthetic native state invalid")
		}
		if s.Management != nil && len(s.Management.Pending) > 0 && string(s.Management.Pending) != "null" && failSeal.Swap(false) {
			return errors.New("synthetic native save rejected")
		}
		return seal(data)
	}
	cfg := mobileworkflow.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), SigningKey: key, ReceivingPrivateKey: keys.ReceivingPrivate, SaveProtectedState: save}
	w := environmentValue(mobileworkflow.New(cfg))
	defer func() { w.Close() }()
	registration := environmentValue(w.Register(ctx, f.email, f.password))
	f.account = registration.AccountID
	f.generation = registration.AccountGeneration
	var mails []struct{ To, Text string }
	if callJSON(t, f.proxy.Client(), f.proxy.URL, "/test/emails", "", "", "", nil, &mails) != 200 {
		t.Fatal("synthetic verification mail unavailable")
	}
	var emailProof mobileworkflow.EmailProof
	for _, m := range mails {
		if m.To == f.email {
			for _, line := range strings.Split(m.Text, "\n") {
				if strings.HasPrefix(line, "{") {
					originMust(t, json.Unmarshal([]byte(line), &emailProof))
				}
			}
		}
	}
	originMust(t, w.VerifyEmail(ctx, emailProof))
	originMust(t, w.Login(ctx, f.email, f.password))
	code := environmentValue(w.BeginInitialization(ctx, "synthetic-managed-environment", "management-init"))
	view := environmentValue(w.CompleteInitialization(ctx, code))
	code = ""
	if len(view.Environments) != 1 {
		t.Fatal("real initialization missing environment")
	}
	env := view.Environments[0].ID
	var native struct {
		DeviceID string                    `json:"deviceId"`
		Root     *cryptox.TrustRoot        `json:"root"`
		Initial  []cryptox.SignedGrantWire `json:"initialAuthorities"`
	}
	data := load()
	originMust(t, json.Unmarshal(data, &native))
	clear(data)
	if native.Root == nil {
		t.Fatal("real original root missing")
	}
	f.root = *native.Root
	f.initial = native.Initial
	keys.DeviceID = native.DeviceID
	a := &originActor{keys: keys, key: key, engine: environmentValue(localstate.New(&originMemoryStore{state: localstate.EmptyState()}))}
	a.verifier = environmentValue(syncclient.NewRootPinnedVerifierWithOrigins(syncclient.OriginRootPinnedTrust{Trust: syncclient.PinnedTrust{AccountID: f.account, AccountGeneration: 1, DeviceID: keys.DeviceID, DeviceSigningPublicKey: keys.SigningPublic, ReceivingPrivateKey: keys.ReceivingPrivate}, Root: f.root, InitialAuthorities: f.initial}))
	defer a.verifier.Close()
	f.boot(a)
	f.pull(a)
	b := f.enroll(a, "management-reader", env, "ro", "0", false)
	targetID = b.keys.DeviceID
	list := environmentValue(w.ManagementDevices(ctx, env))
	if len(list) != 2 {
		t.Fatal("verified management directory missing original PAKE identity")
	}
	before := grantPosts.Load()
	if _, e := b.client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: "management-ro-denied", EnvironmentID: env, SubjectDeviceID: a.keys.DeviceID, Role: "rw"}, b.key); e == nil {
		t.Fatal("reader prepared a grant")
	}
	if grantPosts.Load() != before {
		t.Fatal("reader submitted a grant")
	}
	for _, mode := range []int64{1, 2} {
		tamper.Store(mode)
		if _, e := w.PrepareDeviceGrant(ctx, syncclient.GrantUpdateIntent{ID: "management-forged-" + strconv.FormatInt(mode, 10), EnvironmentID: env, SubjectDeviceID: b.keys.DeviceID, Role: "rw"}); e == nil {
			t.Fatal("untrusted public key or already-seen generation rollback accepted")
		}
		tamper.Store(0)
	}
	// 保存失败禁止POST，同实例继续使用已产生的原包；取消永久保留原ID。
	failSeal.Store(true)
	if _, e := w.PrepareDeviceGrant(ctx, syncclient.GrantUpdateIntent{ID: "management-cancel", EnvironmentID: env, SubjectDeviceID: b.keys.DeviceID, Role: "rw"}); e == nil {
		t.Fatal("failed native seal reported prepared")
	}
	if grantPosts.Load() != before {
		t.Fatal("grant sent before encrypted journal saved")
	}
	originMust(t, w.CancelManagement("management-cancel"))
	if _, e := w.PrepareDeviceGrant(ctx, syncclient.GrantUpdateIntent{ID: "management-cancel", EnvironmentID: env, SubjectDeviceID: b.keys.DeviceID, Role: "rw"}); !errors.Is(e, mobileworkflow.ErrManagementConflict) {
		t.Fatal("retired id reused", e)
	}
	in := syncclient.GrantUpdateIntent{ID: "management-lost", EnvironmentID: env, SubjectDeviceID: b.keys.DeviceID, Role: "rw"}
	info := environmentValue(w.PrepareDeviceGrant(ctx, in))
	if info.Attempted || info.State != "prepared" || grantPosts.Load() != before {
		t.Fatal("prepare performed shared write")
	}
	loseGrant.Store(true)
	if out, e := w.RetryManagement(ctx, in.ID); !errors.Is(e, mobileworkflow.ErrManagementPending) || !out.AcceptanceUnknown {
		t.Fatal("lost accepted response did not preserve unknown transaction", e)
	}
	if !errors.Is(w.CancelManagement(in.ID), mobileworkflow.ErrManagementPending) {
		t.Fatal("attempted unknown request cancelled")
	}
	if _, e := w.View(); !errors.Is(e, mobileworkflow.ErrManagementPending) {
		t.Fatal("pending management did not gate other vault operations")
	}
	saved := load()
	var protected map[string]json.RawMessage
	originMust(t, json.Unmarshal(saved, &protected))
	if bytes.Contains(saved, key.Seed()) || bytes.Contains(saved, keys.ReceivingPrivate) {
		t.Fatal("native journal included device private material")
	}
	w.Close()
	cfg.ProtectedState = saved
	w = environmentValue(mobileworkflow.New(cfg))
	clear(saved)
	// 原RW已经接受后，被独立的Admin更新覆盖；旧receipt仍应正确完成而不能重写RW。
	f.pull(a)
	newer := environmentValue(a.client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: "management-later-admin", EnvironmentID: env, SubjectDeviceID: b.keys.DeviceID, Role: "admin"}, a.key))
	accepted := environmentValue(newer.Submit(ctx))
	if !environmentValue(newer.Confirm(ctx, accepted)).Applied {
		t.Fatal("newer grant failed verified pull")
	}
	posts := grantPosts.Load()
	result := environmentValue(w.RetryManagement(ctx, in.ID))
	if !result.Accepted || !result.Applied || grantPosts.Load() != posts {
		t.Fatal("original accepted status replayed a new grant")
	}
	list = environmentValue(w.ManagementDevices(ctx, env))
	found := false
	for _, row := range list {
		if row.DeviceID == b.keys.DeviceID {
			found = row.Role == "admin" && row.GrantGeneration == "3"
		}
	}
	if !found {
		t.Fatal("old receipt overwrote later target permissions")
	}
	f.pull(b)
	// None代际保留，后续到期/旧KV同样必须最高GG+1。
	update := func(id, role string, expires int64) {
		t.Helper()
		environmentValue(w.PrepareDeviceGrant(ctx, syncclient.GrantUpdateIntent{ID: id, EnvironmentID: env, SubjectDeviceID: b.keys.DeviceID, Role: role, ExpiresAt: expires}))
		r := environmentValue(w.RetryManagement(ctx, id))
		if !r.Accepted || !r.Applied {
			t.Fatal("online grant was not applied by verified pull")
		}
	}
	update("management-none", "none", 0)
	f.pull(b)
	if len(b.engine.State().Cloud.Environments) != 0 {
		t.Fatal("none did not clear reader cache")
	}
	expiry := time.Now().Unix() + 2
	update("management-expiring", "ro", expiry)
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Unix() <= expiry && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	f.rotate(a, env, "synthetic-rotated-label", "management-rotation")
	list = environmentValue(w.ManagementDevices(ctx, env))
	for _, row := range list {
		if row.DeviceID == b.keys.DeviceID && (row.GrantGeneration != "5" || row.KeyVersion != "1" || row.ExpiresAt != expiry) {
			t.Fatal("expired oldKV highest grant lost")
		}
	}
	update("management-regrant", "rw", 0)
	f.pull(b)
	if b.engine.State().Cloud.Environments[env].KeyVersion != 2 || b.engine.State().Cloud.Environments[env].GrantGeneration != 6 {
		t.Fatal("oldKV regrant reset highest generation or key")
	}
	temporary := time.Now().Unix() + 60
	update("management-temporary-admin", "admin", temporary)
	f.pull(b)
	if _, e := b.client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: "management-ceiling-denied", EnvironmentID: env, SubjectDeviceID: a.keys.DeviceID, Role: "admin"}, b.key); !errors.Is(e, syncclient.ErrWritePermission) {
		t.Fatal("temporary admin granted permanent admin", e)
	}
	c := f.enroll(a, "management-other-reader", env, "ro", "0", false)
	f.pull(b)
	none := environmentValue(b.client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: "management-temporary-none", EnvironmentID: env, SubjectDeviceID: c.keys.DeviceID, Role: "none"}, b.key))
	na := environmentValue(none.Submit(ctx))
	if !environmentValue(none.Confirm(ctx, na)).Applied {
		t.Fatal("temporary admin none ceiling normalization failed")
	}
	f.pull(c)
	if len(c.engine.State().Cloud.Environments) != 0 {
		t.Fatal("per-environment revocation retained target cache")
	}
	// B仅X Admin，Y未授权：全局撤销必须逐环境检查，不能拿X角色作为账号管理员。
	f.create(a, env, "management-Y", "management-admin-only-Y", "management-create-Y", false)
	f.pull(b)
	if _, e := b.client.PrepareOtherRevocation(ctx, "management-not-all-admin", c.keys.DeviceID, env, b.key); e == nil {
		t.Fatal("single environment admin prepared global device revocation")
	}
	// 其他设备全局撤销：丢接受响应后密封重启，只查同ID exacthash，旧会话与boot均失效。
	environmentValue(w.PrepareOtherDeviceRevocation(ctx, "management-global", b.keys.DeviceID, env))
	// 尚未POST就原生Close/New；fresh boot只用于刷新当前权，提交仍持原短期token/sessionHash。
	prepared := load()
	w.Close()
	cfg.ProtectedState = prepared
	w = environmentValue(mobileworkflow.New(cfg))
	clear(prepared)
	if revokePosts.Load() != 0 {
		t.Fatal("prepared native restart submitted revocation")
	}
	loseRevoke.Store(true)
	if r, e := w.RetryManagement(ctx, "management-global"); !errors.Is(e, mobileworkflow.ErrManagementPending) || !r.AcceptanceUnknown {
		t.Fatal("lost global revoke did not retain original pending", e)
	}
	saved = load()
	w.Close()
	cfg.ProtectedState = saved
	w = environmentValue(mobileworkflow.New(cfg))
	clear(saved)
	oldRevokePosts := revokePosts.Load()
	result = environmentValue(w.RetryManagement(ctx, "management-global"))
	if !result.Accepted || !result.Applied || revokePosts.Load() != oldRevokePosts {
		t.Fatal("global revoke retry generated a replacement request")
	}
	if _, e := b.client.Pull(ctx); e == nil {
		t.Fatal("globally revoked old device session still reads")
	}
	boot := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: f.account, AccountGeneration: 1, DeviceID: b.keys.DeviceID, Engine: b.engine, Verifier: b.verifier}))
	if _, e := boot.BootDevice(ctx, b.key); !errors.Is(e, syncclient.ErrTrustInvalidated) {
		t.Fatal("globally revoked device boot not invalidated", e)
	}
	// 短时原bearer到期后仅保留原签包，真正的新boot只能查询旧id，不能改绑再提交。
	f.pull(a)
	var offset atomic.Int64
	virtual := environmentValue(syncclient.New(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: f.account, AccountGeneration: 1, DeviceID: a.keys.DeviceID, Token: a.session.Token, Engine: a.engine, Verifier: a.verifier, Now: func() time.Time { return time.Now().Add(time.Duration(offset.Load()) * time.Second) }}))
	oldToken := a.session.Token
	tx := environmentValue(virtual.PrepareOtherRevocation(ctx, "management-expired-original", c.keys.DeviceID, env, a.key))
	originalHash := environmentValue(tx.ContentHash())
	loseRevoke.Store(true)
	if _, e := tx.Submit(ctx); e == nil {
		t.Fatal("lost accepted native revocation reported success")
	}
	postCount := revokePosts.Load()
	offset.Store(130)
	tx.DiscardExpiredToken()
	sealedNative := environmentValue(tx.ProtectedBytes())
	if bytes.Contains(sealedNative, []byte(oldToken)) {
		t.Fatal("expired native bearer remained in journal")
	}
	restored := environmentValue(virtual.RestoreOtherRevocation(sealedNative))
	fresh := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: f.account, AccountGeneration: 1, DeviceID: a.keys.DeviceID, Engine: a.engine, Verifier: a.verifier}))
	fresh = environmentValue(fresh.BootDevice(ctx, a.key))
	exact := environmentValue(restored.StatusThrough(ctx, fresh))
	if !exact.Accepted || exact.ContentHash != originalHash {
		t.Fatal("new boot status changed original transaction receipt")
	}
	if _, e := restored.Submit(ctx); !errors.Is(e, syncclient.ErrSelfRevocationExpired) || revokePosts.Load() != postCount {
		t.Fatal("expired original request resubmitted or rebound", e)
	}
	clear(sealedNative)
	// 取消/未知/结束的元数据不包含bearer、封套、目录公钥。
	meta := environmentValue(json.Marshal(environmentValue(w.ManagementInfo())))
	if bytes.Contains(meta, []byte("session")) || bytes.Contains(meta, []byte("envelope")) || bytes.Contains(meta, []byte("signature")) {
		t.Fatal("management metadata leaked native packet")
	}
	t.Log("真实HTTPS: PAKE身份、改权、期限上限、最高代际、504密封重启、旧回执、全局撤销通过")
}

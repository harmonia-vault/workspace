package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
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

	"github.com/harmonia-vault/core-go/accountreset"
	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/mobilebridge"
	"github.com/harmonia-vault/core-go/mobileworkflow"
)

// 该 cleanup 是隔离 Go 原生边界替身，不是 SDK/Keystore 或真实槽删除证明。
type mailResetCleanup struct {
	clear  func() error
	empty  bool
	checks int
}

func (c *mailResetCleanup) ClearAndReacquireEmpty() error {
	// 原 owner 已 Logout 后，续办只重验该合成空槽，不重用已关闭 Workflow。
	if c.empty {
		return c.CheckEmpty()
	}
	if e := c.clear(); e != nil {
		return e
	}
	c.empty = true
	return nil
}
func (c *mailResetCleanup) CheckEmpty() error {
	c.checks++
	if !c.empty {
		return accountreset.ErrInput
	}
	return nil
}

// 一条真实注册/初始 vault → 邮件申请 → 原证明重置的 HTTPS/SQLite 链。
// 邮件捕获只接受 .invalid；证明及密码只在 RAM，不写日志或测试输出。
func TestNativeAccountResetRequestedEmailHTTPSOriginalProofAndUnknownCommit(t *testing.T) {
	ctx := context.Background()
	f := startFixture(t, "--empty-vault", "--capture-email")
	backend := environmentValue(url.Parse(f.Endpoint))
	handler := httputil.NewSingleHostReverseProxy(backend)
	var lose atomic.Bool
	var requestPosts, commitPosts atomic.Int64
	handler.ModifyResponse = func(r *http.Response) error {
		if r.Request.Method == http.MethodPost && r.StatusCode == http.StatusOK {
			if r.Request.URL.Path == "/v1/account-reset/request" {
				requestPosts.Add(1)
			}
			if strings.HasSuffix(r.Request.URL.Path, "/account-reset/complete") {
				commitPosts.Add(1)
				if lose.Swap(false) {
					_ = r.Body.Close()
					b := []byte(`{"error":"request_rejected"}`)
					r.StatusCode, r.Body, r.ContentLength = 504, io.NopCloser(bytes.NewReader(b)), int64(len(b))
					r.Header.Set("Content-Length", strconv.Itoa(len(b)))
				}
			}
		}
		return nil
	}
	proxy := httptest.NewTLSServer(handler)
	defer proxy.Close()
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: proxy.Certificate().Raw})
	_, key, e := ed25519.GenerateKey(rand.Reader)
	originMust(t, e)
	defer clear(key)
	_, receiving, e := cryptox.GenerateReceivingKey()
	originMust(t, e)
	defer clear(receiving)
	save, _ := recoveryNativeSeal(t, "synthetic-mail-reset-state")
	w := environmentValue(mobileworkflow.New(mobileworkflow.Config{Endpoint: proxy.URL, HTTPClient: proxy.Client(), SigningKey: key, ReceivingPrivateKey: receiving, SaveProtectedState: save}))
	defer w.Close()
	email, oldPassword := "native-reset-mail@example.invalid", "SYNTHETIC_OLD_MAIL_PASSWORD"
	registered := environmentValue(w.Register(ctx, email, oldPassword))
	var mails []struct{ To, Subject, Text string }
	readMail := func(subject string) []byte {
		t.Helper()
		mails = nil
		if callJSON(t, proxy.Client(), proxy.URL, "/test/emails", "", "", "", nil, &mails) != 200 {
			t.Fatal("合成邮件不可读")
		}
		for _, m := range mails {
			if m.To == email && m.Subject == subject {
				for _, line := range strings.Split(m.Text, "\n") {
					if code, ok := strings.CutPrefix(line, "验证码："); ok && len(code) == 8 {
						return []byte(code)
					}
				}
			}
		}
		t.Fatal("本次合成证明缺失")
		return nil
	}
	verify := readMail("Harmonia 邮箱验证码")
	verification := mobileworkflow.EmailVerification{AccountID: registered.AccountID, AccountGeneration: registered.AccountGeneration, Code: string(verify)}
	clear(verify)
	originMust(t, w.VerifyEmail(ctx, verification))
	verification = mobileworkflow.EmailVerification{}
	originMust(t, w.Login(ctx, email, oldPassword))
	code := environmentValue(w.BeginInitialization(ctx, "合成待重置环境", "native-mail-init"))
	view := environmentValue(w.CompleteInitialization(ctx, code))
	code = ""
	if len(view.Environments) != 1 {
		t.Fatal("真实初始 vault 未创建")
	}
	mailOwner := environmentValue(mobilebridge.OpenNativeAccountResetMail(proxy.URL, "synthetic-package\x00harmonia/workflow-state/v1\x00synthetic-slot", ca))
	defer mailOwner.Close()
	input := []byte(email)
	accepted := environmentValue(mailOwner.RequestEmail(input))
	var limited struct {
		Accepted          bool `json:"accepted"`
		RetryAfterSeconds int  `json:"retryAfterSeconds"`
	}
	originMust(t, json.Unmarshal([]byte(accepted), &limited))
	if !limited.Accepted && limited.RetryAfterSeconds > 0 && limited.RetryAfterSeconds <= 30 {
		time.Sleep(time.Duration(limited.RetryAfterSeconds+1) * time.Second)
		input = []byte(email)
		accepted = environmentValue(mailOwner.RequestEmail(input))
	}

	if accepted != `{"version":1,"accepted":true,"trustedDevice":false}` || !bytes.Equal(input, make([]byte, len(input))) || requestPosts.Load() != 1 {
		t.Fatal("原生邮件入口未严格消费/接受")
	}
	shortCode := readMail("Harmonia 账号重置验证码")
	proof := environmentValue(json.Marshal(map[string]string{"email": email, "code": string(shortCode)}))
	clear(shortCode)
	resetClient := environmentValue(accountreset.New(accountreset.Config{Endpoint: proxy.URL, HTTPClient: proxy.Client()}))
	p := environmentValue(resetClient.ResolveCode(ctx, proof))
	if p.AccountID != registered.AccountID || p.AccountGeneration != registered.AccountGeneration {
		t.Fatal("证明账号范围错误")
	}
	retained := bytes.Clone(proof)
	defer clear(retained)
	reset := environmentValue(mobilebridge.NewNativeAccountReset(proxy.URL, "synthetic-package\x00harmonia/workflow-state/v1\x00synthetic-slot", proof, ca))
	defer reset.Close()
	if !bytes.Equal(proof, make([]byte, len(proof))) {
		t.Fatal("证明输入未消费")
	}
	if _, e = reset.Query(); e != nil {
		t.Fatal("重置前原证明查询失败")
	}
	password := []byte("SYNTHETIC_NEW_MAIL_PASSWORD")
	digest := sha256.Sum256(password)
	credential := hex.EncodeToString(digest[:])
	clear(digest[:])
	originMust(t, reset.Prepare(password, accountreset.Confirmation))
	if !bytes.Equal(password, make([]byte, len(password))) {
		t.Fatal("新密码输入未消费")
	}
	cleanup := &mailResetCleanup{clear: w.Logout}
	lose.Store(true)
	ticket := environmentValue(reset.BeginCompletion())
	if out, e := ticket.Complete(cleanup); e == nil || out != "" || commitPosts.Load() != 1 {
		t.Fatal("已接受丢回应被伪报完成")
	}
	ticket = environmentValue(reset.BeginCompletion()) // 原证明先查 accepted，不产生替代包。
	out := environmentValue(ticket.Complete(cleanup))
	var metadata struct {
		Version       int
		TrustedDevice bool
		Outcome       accountreset.Outcome
	}
	originMust(t, json.Unmarshal([]byte(out), &metadata))
	if metadata.Version != 1 || metadata.TrustedDevice || metadata.Outcome.State != "complete" || metadata.Outcome.AccountID != registered.AccountID || metadata.Outcome.AccountGeneration != "2" || metadata.Outcome.Source != "status" || commitPosts.Load() != 1 || cleanup.checks != 4 {
		t.Fatal("原 proof 续办不符合一次提交/本机清理门")
	}
	cold := environmentValue(mobilebridge.OpenNativeAccountResetQuery(proxy.URL, "synthetic-package\x00harmonia/workflow-state/v1\x00synthetic-slot", retained, ca))
	defer cold.Close()
	if _, e = cold.Query(); e != nil {
		t.Fatal("冷证明无法查询原 accepted")
	}
	if e = cold.Prepare([]byte("SYNTHETIC_REPLACEMENT_FORBIDDEN"), accountreset.Confirmation); e == nil {
		t.Fatal("冷查询生成新重置")
	}
	var login struct{ AccountID, AccountGeneration string }
	if callJSON(t, proxy.Client(), proxy.URL, "/v1/login", "", "", "", map[string]string{"email": email, "credential": credential}, &login) != 200 || login.AccountID != registered.AccountID || login.AccountGeneration != "2" {
		t.Fatal("新密码未成为新 generation")
	}
	old := sha256.Sum256([]byte(oldPassword))
	if callJSON(t, proxy.Client(), proxy.URL, "/v1/login", "", "", "", map[string]string{"email": email, "credential": hex.EncodeToString(old[:])}, nil) != 401 {
		t.Fatal("旧密码未失效")
	}
	clear(old[:])
}

package acceptance

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
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
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
)

type realSelfFixture struct {
	t              *testing.T
	workflow       *mobileworkflow.Workflow
	config         mobileworkflow.Config
	proxy          *httptest.Server
	sealed         []byte
	aead           cipher.AEAD
	failSelfSave   atomic.Bool
	mode           atomic.Int32
	clockOffset    atomic.Int64
	mu             sync.Mutex
	completeBodies [][]byte
	completeTokens []string
	challengePosts int
	bootPosts      int
}

func newRealSelfFixture(t *testing.T) *realSelfFixture {
	t.Helper()
	f := &realSelfFixture{t: t}
	backend := startFixture(t, "--capture-email")
	target := environmentValue(url.Parse(backend.Endpoint))
	reverse := httputil.NewSingleHostReverseProxy(target)
	reverse.ModifyResponse = func(response *http.Response) error {
		if response.Request.Method == "POST" && strings.HasSuffix(response.Request.URL.Path, "/device-revocations/complete") && response.StatusCode == 200 && f.mode.CompareAndSwap(2, 0) {
			_ = response.Body.Close()
			body := []byte(`{"error":"synthetic_lost_accepted_response"}`)
			response.StatusCode = 502
			response.Body = io.NopCloser(bytes.NewReader(body))
			response.ContentLength = int64(len(body))
			response.Header.Set("Content-Length", strconv.Itoa(len(body)))
		}
		return nil
	}
	f.proxy = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if r.Method == "POST" {
			if strings.HasSuffix(path, "/device-revocations") {
				f.mu.Lock()
				f.challengePosts++
				f.mu.Unlock()
			}
			if strings.HasSuffix(path, "/boot-challenges") {
				f.mu.Lock()
				f.bootPosts++
				f.mu.Unlock()
			}
			if strings.HasSuffix(path, "/device-revocations/complete") {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					w.WriteHeader(500)
					return
				}
				f.mu.Lock()
				f.completeBodies = append(f.completeBodies, bytes.Clone(body))
				f.completeTokens = append(f.completeTokens, r.Header.Get("Authorization"))
				f.mu.Unlock()
				r.Body = io.NopCloser(bytes.NewReader(body))
				if f.mode.CompareAndSwap(1, 0) {
					w.WriteHeader(502)
					_, _ = io.WriteString(w, `{"error":"synthetic_before_acceptance"}`)
					return
				}
			}
		}
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(f.proxy.Close)
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
	f.aead = environmentValue(cipher.NewGCM(environmentValue(aes.NewCipher(sealKey))))
	f.config = mobileworkflow.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), SigningKey: signing, ReceivingPrivateKey: receiving, Now: func() time.Time { return time.Now().Add(time.Duration(f.clockOffset.Load()) * time.Second) }, SaveProtectedState: func(plain []byte) error {
		var context struct {
			Self []byte `json:"selfRevocation"`
		}
		if json.Unmarshal(plain, &context) != nil {
			return errors.New("synthetic context malformed")
		}
		if len(context.Self) > 0 && f.failSelfSave.Load() {
			return errors.New("synthetic native AES save failure")
		}
		nonce := make([]byte, f.aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		f.sealed = f.aead.Seal(nonce, nonce, plain, []byte("synthetic-selfrevocation-native-v1"))
		return nil
	}}
	f.workflow = environmentValue(mobileworkflow.New(f.config))
	t.Cleanup(func() { f.workflow.Close() })
	ctx := context.Background()
	email, password := "selfrevocation-test@example.invalid", "synthetic-self-password-only"
	registered := environmentValue(f.workflow.Register(ctx, email, password))
	var messages []struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Text    string `json:"text"`
	}
	if status := callJSON(t, f.proxy.Client(), f.proxy.URL, "/test/emails", "", "", "", nil, &messages); status != 200 {
		t.Fatal(status)
	}
	var proof mobileworkflow.EmailProof
	for _, message := range messages {
		if message.To == email {
			for _, line := range strings.Split(message.Text, "\n") {
				if strings.HasPrefix(line, "{") {
					_ = json.Unmarshal([]byte(line), &proof)
				}
			}
		}
	}
	if proof.AccountID != registered.AccountID || proof.Token == "" {
		t.Fatal("real registered synthetic email proof missing")
	}
	if err = f.workflow.VerifyEmail(ctx, proof); err != nil {
		t.Fatal(err)
	}
	if err = f.workflow.Login(ctx, email, password); err != nil {
		t.Fatal(err)
	}
	code := environmentValue(f.workflow.BeginInitialization(ctx, "自撤销合成环境", "self-init"))
	view := environmentValue(f.workflow.CompleteInitialization(ctx, code))
	if len(view.Environments) != 1 {
		t.Fatal("real first root initialization missing")
	}
	_ = environmentValue(f.workflow.SetVariable(ctx, view.Environments[0].ID, "SELF_SYNTHETIC_CACHE", "synthetic-self-cache-value", "self-value"))
	return f
}
func (f *realSelfFixture) plain() []byte {
	f.t.Helper()
	if len(f.sealed) < f.aead.NonceSize() {
		f.t.Fatal("native state not AES sealed")
	}
	return environmentValue(f.aead.Open(nil, f.sealed[:f.aead.NonceSize()], f.sealed[f.aead.NonceSize():], []byte("synthetic-selfrevocation-native-v1")))
}
func (f *realSelfFixture) reopen() {
	f.t.Helper()
	f.workflow.Close()
	f.config.ProtectedState = f.plain()
	f.workflow = environmentValue(mobileworkflow.New(f.config))
}
func (f *realSelfFixture) counts() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.challengePosts, len(f.completeBodies), f.bootPosts
}
func (f *realSelfFixture) assertCleared(t *testing.T) {
	t.Helper()
	var state struct {
		Root  *cryptox.TrustRoot `json:"root"`
		Cloud localstate.State   `json:"cloud"`
		Self  []byte             `json:"selfRevocation"`
	}
	plain := f.plain()
	if json.Unmarshal(plain, &state) != nil || state.Root != nil || !state.Cloud.AccountClosed || len(state.Self) > 0 || len(state.Cloud.Cloud.Environments) > 0 || bytes.Contains(plain, []byte("synthetic-self-cache-value")) {
		t.Fatal("accepted/invalidated self-revocation did not persist closed scrubbed context")
	}
}
func TestMobileSelfRevocationRealSQLiteKnownAcceptanceLostResponseResumeAndSaveGate(t *testing.T) {
	ctx := context.Background()
	t.Run("known-200", func(t *testing.T) {
		f := newRealSelfFixture(t)
		result, err := f.workflow.RevokeSelf(ctx, "real-self-known")
		if err != nil || !result.Completed || !result.DeviceInvalidated || result.AcceptanceUnknown || result.Sequence == 0 {
			t.Fatal(result, err)
		}
		f.assertCleared(t)
		if _, err = f.workflow.View(); !errors.Is(err, mobileworkflow.ErrClosed) {
			t.Fatal("known revoke still returned native cache", err)
		}
		challenge, posts, _ := f.counts()
		if challenge != 1 || posts != 1 {
			t.Fatal(challenge, posts)
		}
	})
	t.Run("accepted-502-unknown", func(t *testing.T) {
		f := newRealSelfFixture(t)
		f.mode.Store(2)
		result, err := f.workflow.RevokeSelf(ctx, "real-self-lost")
		if !errors.Is(err, mobileworkflow.ErrSelfRevocationPending) || !result.AcceptanceUnknown || result.Completed {
			t.Fatal(result, err)
		}
		f.reopen()
		if _, err = f.workflow.View(); !errors.Is(err, mobileworkflow.ErrSelfRevocationPending) {
			t.Fatal("unknown restored offline cache readable", err)
		}
		if _, err = f.workflow.Pull(ctx); !errors.Is(err, mobileworkflow.ErrSelfRevocationPending) {
			t.Fatal("unknown restored normal pull allowed", err)
		}
		if environmentValue(f.workflow.SelfRevocationInfo()).ID != "real-self-lost" {
			t.Fatal("same-id metadata missing")
		}
		result, err = f.workflow.RevokeSelf(ctx, "real-self-lost")
		if !errors.Is(err, syncclient.ErrTrustInvalidated) || result.Completed || !result.DeviceInvalidated || !result.AcceptanceUnknown {
			t.Fatal("401/status then realboot403 is not own receipt completion", result, err)
		}
		f.assertCleared(t)
		challenge, posts, _ := f.counts()
		if challenge != 1 || posts != 1 {
			t.Fatal("lost result regenerated/reposted", challenge, posts)
		}
	})
	t.Run("before-acceptance-same-token-bundle", func(t *testing.T) {
		f := newRealSelfFixture(t)
		f.mode.Store(1)
		_, err := f.workflow.RevokeSelf(ctx, "real-self-original")
		if !errors.Is(err, mobileworkflow.ErrSelfRevocationPending) {
			t.Fatal(err)
		}
		_, _, bootsBefore := f.counts()
		f.reopen()
		result, err := f.workflow.RevokeSelf(ctx, "real-self-original")
		if err != nil || !result.Completed || result.AcceptanceUnknown {
			t.Fatal(result, err)
		}
		challenge, posts, bootsAfter := f.counts()
		f.mu.Lock()
		sameBody := bytes.Equal(f.completeBodies[0], f.completeBodies[1])
		sameSession := f.completeTokens[0] == f.completeTokens[1]
		f.mu.Unlock()
		if challenge != 1 || posts != 2 || bootsAfter != bootsBefore || !sameBody || !sameSession {
			t.Fatal("restart did not reuse exact original signature/session before deadline", challenge, posts, bootsBefore, bootsAfter, sameBody, sameSession)
		}
		f.assertCleared(t)
	})
	t.Run("native-save-failure", func(t *testing.T) {
		f := newRealSelfFixture(t)
		f.failSelfSave.Store(true)
		if _, err := f.workflow.RevokeSelf(ctx, "real-self-save"); err == nil {
			t.Fatal("failed native save proceeded")
		}
		challenge, posts, _ := f.counts()
		if challenge != 1 || posts != 0 {
			t.Fatal("native save failure reached irreversible POST", challenge, posts)
		}
		f.failSelfSave.Store(false)
		if err := f.workflow.Logout(); err != nil {
			t.Fatal(err)
		}
		f.assertCleared(t)
	})
	t.Run("expired-unknown-no-bearer-or-replay", func(t *testing.T) {
		f := newRealSelfFixture(t)
		f.mode.Store(1)
		_, err := f.workflow.RevokeSelf(ctx, "real-self-expiry")
		if !errors.Is(err, mobileworkflow.ErrSelfRevocationPending) {
			t.Fatal(err)
		}
		f.clockOffset.Add(121)
		f.reopen()
		result, err := f.workflow.RevokeSelf(ctx, "real-self-expiry")
		if err == nil || result.Completed || !result.AcceptanceUnknown {
			t.Fatal("wall clock expiry falsely reported completion", result, err)
		}
		info := environmentValue(f.workflow.SelfRevocationInfo())
		if info.State != "expired-pending" || info.ID != "real-self-expiry" {
			t.Fatal(info)
		}
		if _, err = f.workflow.View(); !errors.Is(err, mobileworkflow.ErrSelfRevocationPending) {
			t.Fatal("expired unknown native cache reopened", err)
		}
		_, posts, _ := f.counts()
		if posts != 1 {
			t.Fatal("expired original request replayed")
		}
		var state struct {
			Self []byte `json:"selfRevocation"`
		}
		_ = json.Unmarshal(f.plain(), &state)
		var journal struct {
			Token string `json:"sessionToken"`
		}
		_ = json.Unmarshal(state.Self, &journal)
		if journal.Token != "" || len(state.Self) == 0 {
			t.Fatal("expired original bearer not scrubbed or pending evidence forgotten")
		}
	})
	t.Log("真实Go首机双签初始化→HTTPS→TS SQLite：known200精确completed；接受后502，AES重启原status401→boot403只报告deviceInvalidated+ownAcceptanceUnknown；未接受502重启保持原签包/原token/原id，不新boot/挑战；native save失败零完成POST；本地壁钟到期清原bearer且pending拒读、不重放。Go AES替身不是Android Keystore验收。")
}

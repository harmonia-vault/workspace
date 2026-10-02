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
	"reflect"
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

type realRecoveryFixture struct {
	t                 *testing.T
	proxy             *httptest.Server
	workflow          *mobileworkflow.Workflow
	config            mobileworkflow.Config
	code              string
	email             string
	password          string
	accountID         string
	root              *mobileworkflow.Workflow
	rootConfig        mobileworkflow.Config
	load              func() []byte
	failSignatureSave atomic.Bool
	failCompletedSave atomic.Bool
	loss              atomic.Int32
	vaultTamper       atomic.Int32
	clockOffset       atomic.Int64
	mu                sync.Mutex
	begins            int
	completes         int
	completeBodies    [][]byte
	completeTokens    []string
}

func recoveryNativeSeal(t *testing.T, aad string) (func([]byte) error, func() []byte) {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	block := environmentValue(aes.NewCipher(key))
	gcm := environmentValue(cipher.NewGCM(block))
	clear(key)
	var sealed []byte
	return func(plain []byte) error {
			nonce := make([]byte, gcm.NonceSize())
			if _, err := rand.Read(nonce); err != nil {
				return err
			}
			sealed = gcm.Seal(nonce, nonce, plain, []byte(aad))
			return nil
		}, func() []byte {
			t.Helper()
			if len(sealed) < gcm.NonceSize() {
				t.Fatal("native protected recovery state missing")
			}
			return environmentValue(gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(aad)))
		}
}
func newRealRecoveryFixture(t *testing.T) *realRecoveryFixture {
	t.Helper()
	ctx := context.Background()
	f := &realRecoveryFixture{t: t, email: "mobile-recovery@example.invalid", password: "synthetic-mobile-recovery-password"}
	server := startFixture(t, "--capture-email")
	target := environmentValue(url.Parse(server.Endpoint))
	reverse := httputil.NewSingleHostReverseProxy(target)
	reverse.ModifyResponse = func(r *http.Response) error {
		if mode := f.vaultTamper.Load(); mode != 0 && r.StatusCode == 200 && r.Request.Method == "GET" && strings.HasSuffix(r.Request.URL.Path, "/recovery-vault") {
			if r.Request.URL.Query().Get("capability") != "issuer-origin-v1" {
				return errors.New("synthetic recovery reader did not request committed initialization capability")
			}
			data, err := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err != nil {
				return err
			}
			data, err = tamperRecoveryVaultBody(data, mode)
			if err != nil {
				f.t.Error("synthetic recovery proof attack could not be constructed", err)
				return err
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
			r.ContentLength = int64(len(data))
			r.Header.Set("Content-Length", strconv.Itoa(len(data)))
			return nil
		}
		if r.StatusCode != 200 || r.Request.Method != "POST" {
			return nil
		}
		path := r.Request.URL.Path
		lose := strings.HasSuffix(path, "/recovery-rotations") && f.loss.CompareAndSwap(1, 0) || strings.Contains(path, "/recovery-rotations/") && strings.HasSuffix(path, "/complete") && f.loss.CompareAndSwap(2, 0)
		if lose {
			_ = r.Body.Close()
			data := []byte(`{"error":"synthetic_lost_accepted_recovery_response"}`)
			r.StatusCode = 502
			r.Body = io.NopCloser(bytes.NewReader(data))
			r.ContentLength = int64(len(data))
			r.Header.Set("Content-Length", strconv.Itoa(len(data)))
		}
		return nil
	}
	f.proxy = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/recovery-rotations") {
			f.mu.Lock()
			f.begins++
			f.mu.Unlock()
		}
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/recovery-rotations/") && strings.HasSuffix(r.URL.Path, "/complete") {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				w.WriteHeader(500)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(data))
			f.mu.Lock()
			f.completes++
			f.completeBodies = append(f.completeBodies, bytes.Clone(data))
			f.completeTokens = append(f.completeTokens, r.Header.Get("Authorization"))
			f.mu.Unlock()
			if f.loss.CompareAndSwap(3, 0) {
				w.WriteHeader(502)
				_, _ = io.WriteString(w, `{"error":"synthetic_before_recovery_acceptance"}`)
				return
			}
		}
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(f.proxy.Close)
	_, rootKey := environmentValue2(ed25519.GenerateKey(rand.Reader))
	_, rootReceiving, err := cryptox.GenerateReceivingKey()
	if err != nil {
		t.Fatal(err)
	}
	rootSave, _ := recoveryNativeSeal(t, "synthetic-native-root-recovery-v1")
	f.rootConfig = mobileworkflow.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), SigningKey: rootKey, ReceivingPrivateKey: rootReceiving, SaveProtectedState: rootSave}
	f.root = environmentValue(mobileworkflow.New(f.rootConfig))
	t.Cleanup(f.root.Close)
	registration := environmentValue(f.root.Register(ctx, f.email, f.password))
	f.accountID = registration.AccountID
	var mails []struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Text    string `json:"text"`
	}
	if status := callJSON(t, f.proxy.Client(), f.proxy.URL, "/test/emails", "", "", "", nil, &mails); status != 200 {
		t.Fatal(status)
	}
	var proof mobileworkflow.EmailProof
	for _, mail := range mails {
		if mail.To == f.email {
			for _, line := range strings.Split(mail.Text, "\n") {
				if strings.HasPrefix(line, "{") {
					_ = json.Unmarshal([]byte(line), &proof)
				}
			}
		}
	}
	if proof.AccountID != registration.AccountID || proof.Token == "" {
		t.Fatal("real synthetic account proof missing")
	}
	if err = f.root.VerifyEmail(ctx, proof); err != nil {
		t.Fatal(err)
	}
	if err = f.root.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	f.code = environmentValue(f.root.BeginInitialization(ctx, "恢复合成一", "real-recovery-init"))
	initial := environmentValue(f.root.CompleteInitialization(ctx, f.code))
	env1 := initial.Environments[0].ID
	if _, err = f.root.SetVariable(ctx, env1, "RECOVERY_SYNTHETIC_ONE", "synthetic-recovery-value-one", "recovery-var-one"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.root.SetVariable(ctx, env1, "RECOVERY_SYNTHETIC_TWO", "synthetic-recovery-value-two", "recovery-var-two"); err != nil {
		t.Fatal(err)
	}
	_, key := environmentValue2(ed25519.GenerateKey(rand.Reader))
	_, receive, err := cryptox.GenerateReceivingKey()
	if err != nil {
		t.Fatal(err)
	}
	save, load := recoveryNativeSeal(t, "synthetic-native-restricted-recovery-v1")
	f.load = load
	f.config = mobileworkflow.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), SigningKey: key, ReceivingPrivateKey: receive, Now: func() time.Time { return time.Now().Add(time.Duration(f.clockOffset.Load()) * time.Second) }, SaveProtectedState: func(plain []byte) error {
		var state struct {
			Recovery *struct {
				Completed bool `json:"rotationCompleted"`
				Rotation  *struct {
					Signature string `json:"signature"`
				} `json:"rotation"`
			} `json:"recovery"`
		}
		if json.Unmarshal(plain, &state) != nil {
			return errors.New("synthetic protected recovery JSON invalid")
		}
		if f.failCompletedSave.Load() && state.Recovery != nil && state.Recovery.Completed {
			return errors.New("synthetic native completed recovery save failure")
		}
		if f.failSignatureSave.Load() && state.Recovery != nil && state.Recovery.Rotation != nil && state.Recovery.Rotation.Signature != "" {
			return errors.New("synthetic native recovery save failure")
		}
		return save(plain)
	}}
	f.workflow = environmentValue(mobileworkflow.New(f.config))
	t.Cleanup(func() { f.workflow.Close() })
	if err = f.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	info := environmentValue(f.workflow.BeginRecovery(ctx, f.code))
	if info.TrustedDevice || !info.RotationRequired || info.State != "rotation-required" || info.Environments != 1 {
		t.Fatal(info)
	}
	return f
}
func environmentValue2[A any, B any](a A, b B, err error) (A, B) {
	if err != nil {
		panic(err)
	}
	return a, b
}
func (f *realRecoveryFixture) reopen() {
	f.t.Helper()
	f.workflow.Close()
	f.config.ProtectedState = f.load()
	f.workflow = environmentValue(mobileworkflow.New(f.config))
}
func (f *realRecoveryFixture) counts() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.begins, f.completes
}
func (f *realRecoveryFixture) assertRestricted() {
	f.t.Helper()
	if _, err := f.workflow.View(); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
		f.t.Fatal("recovery became ordinary trusted view", err)
	}
	if _, err := f.workflow.Pull(context.Background()); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
		f.t.Fatal("recovery became ordinary device pull", err)
	}
	if _, err := f.workflow.RevokeSelf(context.Background(), "recovery-cannot-revoke"); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
		f.t.Fatal("recovery became trusted self-revoker", err)
	}
	plain := f.load()
	var state struct {
		Root  *cryptox.TrustRoot `json:"root"`
		Cloud struct {
			Cloud struct {
				AccountID string `json:"accountId"`
			} `json:"cloud"`
		} `json:"cloud"`
	}
	if json.Unmarshal(plain, &state) != nil || state.Root != nil || state.Cloud.Cloud.AccountID != "" {
		f.t.Fatal("restricted recovery installed trusted root/cloud")
	}
	if bytes.Contains(plain, []byte(f.code)) || bytes.Contains(plain, []byte(f.password)) {
		f.t.Fatal("native pending retained recovery code or password")
	}
}
func assertRecoveredInitial(t *testing.T, w *mobileworkflow.Workflow) {
	t.Helper()
	v := environmentValue(w.RecoveryView())
	if v.Info.TrustedDevice || len(v.Environments) != 1 {
		t.Fatal(v.Info)
	}
	values := map[string]string{}
	for _, env := range v.Environments {
		for name, value := range env.Variables {
			values[name] = value
		}
	}
	if values["RECOVERY_SYNTHETIC_ONE"] != "synthetic-recovery-value-one" || values["RECOVERY_SYNTHETIC_TWO"] != "synthetic-recovery-value-two" {
		t.Fatal("actual recovery HPKE/AEAD did not recover initial environment")
	}
}
func TestMobileRecoveryRealSQLiteRestrictedFullRotationUnknownResumeAndSaveGate(t *testing.T) {
	ctx := context.Background()
	t.Run("accepted-lost-response-full-reentry-and-new-code", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		assertRecoveredInitial(t, f.workflow)
		f.assertRestricted()
		newCode := environmentValue(f.workflow.BeginRecoveryRotation(ctx, "mobile-recovery-rotation"))
		if len(newCode) != 52 || newCode == f.code {
			t.Fatal("independent full new recovery code missing")
		}
		if _, err := f.workflow.CompleteRecoveryRotation(ctx, newCode[:51]); err == nil {
			t.Fatal("partial new recovery code accepted")
		}
		if _, err := f.workflow.CompleteRecoveryRotation(ctx, f.code); err == nil {
			t.Fatal("old code accepted as full new code")
		}
		f.loss.Store(2)
		if _, err := f.workflow.CompleteRecoveryRotation(ctx, newCode); !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
			t.Fatal("actual accepted response loss not pending", err)
		}
		if bytes.Contains(f.load(), []byte(newCode)) {
			t.Fatal("complete new code persisted")
		}
		f.reopen()
		f.assertRestricted()
		if _, err := f.workflow.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
			t.Fatal("rotation pending leaked restricted values", err)
		}
		info := environmentValue(f.workflow.QueryRecoveryRotation(ctx))
		if info.State != "accepted-unverified" || !info.RotationRequired || info.TrustedDevice {
			t.Fatal(info)
		}
		done := environmentValue(f.workflow.CompleteRecoveryRotation(ctx, newCode))
		if done.State != "rotation-complete-restricted" || done.RotationRequired || done.TrustedDevice || done.RecoveryGeneration != "2" {
			t.Fatal(done)
		}
		f.reopen()
		assertRecoveredInitial(t, f.workflow)
		f.assertRestricted()
		begins, completes := f.counts()
		if begins != 1 || completes != 1 {
			t.Fatal("receipt query created new write", begins, completes)
		}
		// A new device with the old code fails against actual post-rotation server keys.
		_, key := environmentValue2(ed25519.GenerateKey(rand.Reader))
		_, receive, err := cryptox.GenerateReceivingKey()
		if err != nil {
			t.Fatal(err)
		}
		save, _ := recoveryNativeSeal(t, "synthetic-second-recovery-device")
		other := environmentValue(mobileworkflow.New(mobileworkflow.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), SigningKey: key, ReceivingPrivateKey: receive, SaveProtectedState: save}))
		defer other.Close()
		if err = other.Login(ctx, f.email, f.password); err != nil {
			t.Fatal(err)
		}
		if _, err = other.BeginRecovery(ctx, f.code); err == nil {
			t.Fatal("old recovery code still restored rotated account")
		}
		info = environmentValue(other.BeginRecovery(ctx, newCode))
		if !info.RotationRequired || info.TrustedDevice || info.RecoveryGeneration != "2" {
			t.Fatal(info)
		}
		assertRecoveredInitial(t, other)
	})
	t.Run("begin-response-loss-original-proposal", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		f.loss.Store(1)
		code, err := f.workflow.BeginRecoveryRotation(ctx, "recovery-begin-unknown")
		if code == "" || !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
			t.Fatal("unknown begin lost displayed code", err)
		}
		f.reopen()
		done := environmentValue(f.workflow.CompleteRecoveryRotation(ctx, code))
		if done.RotationRequired || done.TrustedDevice {
			t.Fatal(done)
		}
		begins, completes := f.counts()
		if begins != 1 || completes != 1 {
			t.Fatal("begin resume changed original operation", begins, completes)
		}
	})
	t.Run("preaccept-loss-original-signature-and-session", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		code := environmentValue(f.workflow.BeginRecoveryRotation(ctx, "recovery-exact-retry"))
		f.loss.Store(3)
		if _, err := f.workflow.CompleteRecoveryRotation(ctx, code); !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
			t.Fatal(err)
		}
		f.reopen()
		done := environmentValue(f.workflow.CompleteRecoveryRotation(ctx, code))
		if done.TrustedDevice || done.RotationRequired {
			t.Fatal(done)
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.begins != 1 || f.completes != 2 || !bytes.Equal(f.completeBodies[0], f.completeBodies[1]) || f.completeTokens[0] != f.completeTokens[1] {
			t.Fatal("native restart changed original challenge/signature/session")
		}
	})
	t.Run("native-save-failure-before-complete-post", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		code := environmentValue(f.workflow.BeginRecoveryRotation(ctx, "recovery-save-gate"))
		f.failSignatureSave.Store(true)
		if _, err := f.workflow.CompleteRecoveryRotation(ctx, code); err == nil {
			t.Fatal("native save failure ignored")
		}
		_, completes := f.counts()
		if completes != 0 {
			t.Fatal("complete POST sent before native saved signature")
		}
		f.failSignatureSave.Store(false)
		f.reopen()
		done := environmentValue(f.workflow.CompleteRecoveryRotation(ctx, code))
		if done.RotationRequired || done.TrustedDevice {
			t.Fatal(done)
		}
	})
	t.Run("client-expiry-and-clock-rollback-no-complete", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		code := environmentValue(f.workflow.BeginRecoveryRotation(ctx, "recovery-expiry"))
		f.clockOffset.Store(121)
		if _, err := f.workflow.CompleteRecoveryRotation(ctx, code); !errors.Is(err, mobileworkflow.ErrRecoveryExpired) {
			t.Fatal("expired original challenge replayed", err)
		}
		if _, err := f.workflow.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
			t.Fatal("expired pending revealed cache", err)
		}
		f.clockOffset.Store(-10)
		if _, err := f.workflow.CompleteRecoveryRotation(ctx, code); !errors.Is(err, mobileworkflow.ErrRecoveryExpired) {
			t.Fatal("clock rollback allowed complete", err)
		}
		_, completes := f.counts()
		if completes != 0 {
			t.Fatal("clock changes caused complete POST")
		}
	})
}

func TestMobileRecoveryIncompleteEnvelopesMissingOriginAndOldSessionInvalidation(t *testing.T) {
	ctx := context.Background()
	t.Run("missing-envelope-is-atomic-rejection", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		code := environmentValue(f.workflow.BeginRecoveryRotation(ctx, "recovery-full-envelope-set"))
		var native struct {
			Recovery struct {
				Token string `json:"sessionToken"`
				Vault struct {
					Sequence uint64 `json:"sequence"`
				} `json:"vault"`
				Rotation struct {
					Proposal cryptox.RecoveryRotationProposal `json:"proposal"`
				} `json:"rotation"`
			} `json:"recovery"`
		}
		if json.Unmarshal(f.load(), &native) != nil {
			t.Fatal("synthetic native recovery context parse failed")
		}
		incomplete := native.Recovery.Rotation.Proposal
		incomplete.IdempotencyKey = "recovery-incomplete-envelope"
		incomplete.Envelopes = []cryptox.RecoveryEnvelope{}
		base := "/v1/accounts/" + f.accountID
		if got := callJSON(t, f.proxy.Client(), f.proxy.URL, base+"/recovery-rotations", native.Recovery.Token, "", "1", incomplete, nil); got != 409 {
			t.Fatal("missing necessary envelope accepted", got)
		}
		var old struct {
			Generation string `json:"recoveryGeneration"`
			Sequence   uint64 `json:"sequence"`
		}
		if got := callJSON(t, f.proxy.Client(), f.proxy.URL, base+"/recovery-vault", native.Recovery.Token, "", "1", nil, &old); got != 200 || old.Generation != "1" || old.Sequence != native.Recovery.Vault.Sequence {
			t.Fatal("failed envelope set changed authoritative state", got)
		}
		done := environmentValue(f.workflow.CompleteRecoveryRotation(ctx, code))
		if done.RotationRequired || done.TrustedDevice {
			t.Fatal(done)
		}
		assertRecoveredInitial(t, f.workflow)
	})
	t.Run("new-environment-without-signed-origin-fails-closed", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		if _, err := f.root.CreateEnvironment(ctx, "仍缺来源的合成环境", "recovery-origin-missing"); err != nil {
			t.Fatal(err)
		}
		if code, err := f.workflow.BeginRecoveryRotation(ctx, "no-unproved-env-rotation"); err == nil || code != "" {
			t.Fatal("unproved new environment accepted for recovery", err)
		}
		begins, completes := f.counts()
		if begins != 0 || completes != 0 {
			t.Fatal("missing origin submitted recovery mutation", begins, completes)
		}
		f.assertRestricted()
	})
	t.Run("old-other-recovery-session-401-clears-cache", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		_, key := environmentValue2(ed25519.GenerateKey(rand.Reader))
		_, receive, err := cryptox.GenerateReceivingKey()
		if err != nil {
			t.Fatal(err)
		}
		save, load := recoveryNativeSeal(t, "synthetic-old-other-session")
		other := environmentValue(mobileworkflow.New(mobileworkflow.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), SigningKey: key, ReceivingPrivateKey: receive, SaveProtectedState: save}))
		defer other.Close()
		if err = other.Login(ctx, f.email, f.password); err != nil {
			t.Fatal(err)
		}
		if _, err = other.BeginRecovery(ctx, f.code); err != nil {
			t.Fatal(err)
		}
		code := environmentValue(f.workflow.BeginRecoveryRotation(ctx, "invalidate-old-recovery-session"))
		if _, err = f.workflow.CompleteRecoveryRotation(ctx, code); err != nil {
			t.Fatal(err)
		}
		if _, err = other.BeginRecoveryRotation(ctx, "old-session-cannot-rotate"); !errors.Is(err, syncclient.ErrTrustInvalidated) {
			t.Fatal("old recovery session remained authorized", err)
		}
		if _, err = other.RecoveryView(); !errors.Is(err, mobileworkflow.ErrClosed) {
			t.Fatal("received old-session invalidation retained plaintext", err)
		}
		plain := load()
		var state struct {
			Recovery json.RawMessage `json:"recovery"`
			Cloud    struct {
				AccountClosed bool `json:"accountClosed"`
			} `json:"cloud"`
		}
		if json.Unmarshal(plain, &state) != nil || len(state.Recovery) > 0 || !state.Cloud.AccountClosed || bytes.Contains(plain, []byte("synthetic-recovery-value-one")) {
			t.Fatal("received recovery session invalidation not durably scrubbed")
		}
	})
}

func TestMobileRecoveryAcceptedPersistenceAndExpiredSignatureJournalResume(t *testing.T) {
	ctx := context.Background()
	t.Run("accepted-final-native-save-failure-stays-unverified", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		code := environmentValue(f.workflow.BeginRecoveryRotation(ctx, "recovery-final-save"))
		f.failCompletedSave.Store(true)
		if info, err := f.workflow.CompleteRecoveryRotation(ctx, code); !errors.Is(err, syncclient.ErrAcceptedNotApplied) || info.TrustedDevice || !info.RotationRequired {
			t.Fatal("final native failure faked completion", info, err)
		}
		if _, err := f.workflow.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
			t.Fatal("failed native completion exposed applied cache", err)
		}
		f.failCompletedSave.Store(false)
		f.reopen()
		info := environmentValue(f.workflow.CompleteRecoveryRotation(ctx, code))
		if info.RotationRequired || info.TrustedDevice {
			t.Fatal(info)
		}
		begins, completes := f.counts()
		if begins != 1 || completes != 1 {
			t.Fatal("final persistence retry created another cloud write", begins, completes)
		}
	})
	t.Run("expired-signed-journal-restores-without-bearer", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		code := environmentValue(f.workflow.BeginRecoveryRotation(ctx, "recovery-expired-signed"))
		f.loss.Store(3)
		if _, err := f.workflow.CompleteRecoveryRotation(ctx, code); !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
			t.Fatal(err)
		}
		f.clockOffset.Store(901)
		f.reopen()
		info := environmentValue(f.workflow.RecoveryInfo())
		if info.State != "expired-pending" || info.TrustedDevice {
			t.Fatal(info)
		}
		var state struct {
			Recovery struct {
				Token    string `json:"sessionToken"`
				Closed   bool   `json:"sessionClosed"`
				Rotation struct {
					Signature string `json:"signature"`
				} `json:"rotation"`
			} `json:"recovery"`
		}
		if json.Unmarshal(f.load(), &state) != nil || state.Recovery.Token != "" || !state.Recovery.Closed || state.Recovery.Rotation.Signature == "" {
			t.Fatal("expiry lost receipt signature or retained bearer")
		}
		f.clockOffset.Store(902)
		f.reopen()
		if _, err := f.workflow.CompleteRecoveryRotation(ctx, code); !errors.Is(err, mobileworkflow.ErrRecoveryExpired) {
			t.Fatal("expired original session replayed", err)
		}
		_, completes := f.counts()
		if completes != 1 {
			t.Fatal("expired journal generated another POST")
		}
	})
}

func (f *realRecoveryFixture) freshRecoveryContext() *mobileworkflow.Workflow {
	f.t.Helper()
	_, key := environmentValue2(ed25519.GenerateKey(rand.Reader))
	_, receive, err := cryptox.GenerateReceivingKey()
	if err != nil {
		f.t.Fatal(err)
	}
	save, _ := recoveryNativeSeal(f.t, "synthetic-fresh-genesis-recovery")
	w := environmentValue(mobileworkflow.New(mobileworkflow.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), SigningKey: key, ReceivingPrivateKey: receive, SaveProtectedState: save}))
	f.t.Cleanup(w.Close)
	if err = w.Login(context.Background(), f.email, f.password); err != nil {
		f.t.Fatal(err)
	}
	return w
}
func (f *realRecoveryFixture) rotateRootInitialEnvironment() {
	f.t.Helper()
	t := f.t
	var native struct {
		AccountID         string                    `json:"accountId"`
		AccountGeneration string                    `json:"accountGeneration"`
		DeviceID          string                    `json:"deviceId"`
		Root              *cryptox.TrustRoot        `json:"root"`
		Cloud             localstate.State          `json:"cloud"`
		Grants            []cryptox.SignedGrantWire `json:"grants"`
	}
	if json.Unmarshal(environmentValue(f.root.ExportProtectedState()), &native) != nil || native.Root == nil || len(native.Grants) != 1 {
		t.Fatal("real initial-root protected evidence missing")
	}
	g := native.Grants[0].Grant
	base := "/v1/accounts/" + native.AccountID
	var challenge syncclient.DeviceChallenge
	if got := callJSON(t, f.proxy.Client(), f.proxy.URL, base+"/boot-challenges", "", "", "", map[string]string{"deviceId": native.DeviceID, "accountGeneration": native.AccountGeneration}, &challenge); got != 200 {
		t.Fatal(got)
	}
	proof := cryptox.DeviceBootProof{AccountID: native.AccountID, AccountGeneration: native.AccountGeneration, DeviceID: native.DeviceID, SigningPublicKey: native.Root.RootSigningPublicKey, ReceivingPublicKey: native.Root.RootReceivingPublicKey, ChallengeID: challenge.ChallengeID, Nonce: challenge.Nonce, ExpiresAt: strconv.FormatInt(challenge.ExpiresAt, 10)}
	raw := environmentValue(proof.SigningBytes())
	var fields []string
	_ = json.Unmarshal(raw, &fields)
	if !reflect.DeepEqual(fields, challenge.SigningPayload) {
		t.Fatal("boot candidate is not exactly the synthetic root device")
	}
	var session struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expiresAt"`
	}
	if got := callJSON(t, f.proxy.Client(), f.proxy.URL, base+"/boot-sessions", "", "", "", map[string]string{"deviceId": native.DeviceID, "accountGeneration": native.AccountGeneration, "challengeId": challenge.ChallengeID, "signature": environmentValue(cryptox.SignDeviceBootProof(proof, f.rootConfig.SigningKey))}, &session); got != 200 {
		t.Fatal(got)
	}
	key := environmentValue(cryptox.GenerateEnvironmentKey())
	defer clear(key)
	deviceEnvelope := environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: native.AccountID, AccountGeneration: native.AccountGeneration, EnvironmentID: g.EnvironmentID, KeyVersion: "2", RecipientType: "device", RecipientID: native.DeviceID, RecipientGeneration: "2", RecipientPublicKey: native.Root.RootReceivingPublicKey}))
	recoverEnvelope := environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: native.AccountID, AccountGeneration: native.AccountGeneration, EnvironmentID: g.EnvironmentID, KeyVersion: "2", RecipientType: "recovery", RecipientID: native.AccountID, RecipientGeneration: native.Root.RecoveryGeneration, RecipientPublicKey: native.Root.RecoveryReceivingPublicKey}))
	next := g
	next.KeyVersion = "2"
	next.GrantGeneration = "2"
	next.IdempotencyKey = "root-rotated-initial-grant"
	next.Envelope = cryptox.EncodeBase64(deviceEnvelope)
	signedGrant := environmentValue(cryptox.SignGrant(next, f.rootConfig.SigningKey))
	label := environmentValue(cryptox.EncryptEnvironmentLabel(key, cryptox.EnvironmentLabelContext{AccountID: native.AccountID, AccountGeneration: native.AccountGeneration, EnvironmentID: g.EnvironmentID, KeyVersion: "2"}, []byte("轮换的初始合成环境")))
	change := cryptox.EnvironmentChange{AccountID: native.AccountID, AccountGeneration: native.AccountGeneration, DeviceID: native.DeviceID, EnvironmentID: g.EnvironmentID, Operation: "rotate", AuthorityEnvironmentID: g.EnvironmentID, AuthorityKeyVersion: g.KeyVersion, AuthorityGrantGeneration: g.GrantGeneration, PreviousKeyVersion: g.KeyVersion, KeyVersion: "2", ExpectedSequence: strconv.FormatUint(native.Cloud.Cloud.Sequence, 10), IdempotencyKey: "root-rotation-before-fresh-recovery", RecoveryGeneration: native.Root.RecoveryGeneration, RecoveryEnvelope: cryptox.EncodeBase64(recoverEnvelope), LabelPayload: cryptox.EncodeBase64(label), Grants: []cryptox.SignedGrantWire{cryptox.GrantToWire(signedGrant)}}
	view := environmentValue(f.root.View())
	for _, env := range view.Environments {
		if env.ID == g.EnvironmentID {
			for name, value := range env.Variables {
				packet := environmentValue(cryptox.EncryptValue(key, cryptox.ValueContext{AccountID: native.AccountID, AccountGeneration: native.AccountGeneration, EnvironmentID: env.ID, KeyVersion: "2", Name: name}, []byte(value)))
				mutation := cryptox.Mutation{AccountID: native.AccountID, AccountGeneration: native.AccountGeneration, DeviceID: native.DeviceID, EnvironmentID: env.ID, KeyVersion: "2", GrantGeneration: "2", Operation: "put", IdempotencyKey: "root-pre-recovery-" + name, Name: name, Payload: cryptox.EncodeBase64(packet)}
				change.Mutations = append(change.Mutations, cryptox.MutationToWire(environmentValue(cryptox.SignMutation(mutation, f.rootConfig.SigningKey))))
			}
		}
	}
	signed := environmentValue(cryptox.SignEnvironmentChange(change, f.rootConfig.SigningKey))
	var accepted syncclient.Acceptance
	if got := callJSON(t, f.proxy.Client(), f.proxy.URL, base+"/environment-changes", session.Token, native.DeviceID, native.AccountGeneration, signed, &accepted); got != 200 || accepted.Sequence <= native.Cloud.Cloud.Sequence {
		t.Fatal("real root environment rotation was not accepted", got)
	}
}
func TestMobileRecoveryFreshContextRejectsRootCreatedAndRotatedEnvironmentWithoutOrigin(t *testing.T) {
	t.Run("fresh-after-root-create", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		if _, err := f.root.CreateEnvironment(context.Background(), "新根环境", "root-created-before-fresh-recovery"); err != nil {
			t.Fatal(err)
		}
		fresh := f.freshRecoveryContext()
		if _, err := fresh.BeginRecovery(context.Background(), f.code); !errors.Is(err, mobileworkflow.ErrRecoveryEvidence) {
			t.Fatal("fresh root-created environment accepted without genesis/origin", err)
		}
		if info, err := fresh.RecoveryInfo(); err != nil || info.State != "none" || info.TrustedDevice {
			t.Fatal("partial unproved recovery became visible", info, err)
		}
	})
	t.Run("fresh-after-root-rotate", func(t *testing.T) {
		f := newRealRecoveryFixture(t)
		f.rotateRootInitialEnvironment()
		fresh := f.freshRecoveryContext()
		if _, err := fresh.BeginRecovery(context.Background(), f.code); !errors.Is(err, mobileworkflow.ErrRecoveryEvidence) {
			t.Fatal("fresh root-rotated key version accepted without genesis/origin", err)
		}
		if _, err := fresh.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
			t.Fatal("unproved rotated recovery exposed partial plaintext", err)
		}
	})
}

// The attacker can repackage authentic current root grants and real recovery
// envelopes, but cannot sign the initialization-specific dual proof.
func tamperRecoveryVaultBody(data []byte, mode int32) ([]byte, error) {
	var body map[string]json.RawMessage
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}
	if mode == 1 {
		body["originalInitialization"] = json.RawMessage("null")
		return json.Marshal(body)
	}
	var original struct {
		Proposal          cryptox.InitializationProposal `json:"proposal"`
		Proof             cryptox.InitializationProof    `json:"proof"`
		DeviceSignature   string                         `json:"deviceSignature"`
		RecoverySignature string                         `json:"recoverySignature"`
		Sequence          uint64                         `json:"sequence"`
	}
	if err := json.Unmarshal(body["originalInitialization"], &original); err != nil || original.DeviceSignature == "" {
		return nil, errors.New("actual original dual initialization proof missing")
	}
	switch mode {
	case 2:
		original.Proof.Nonce = cryptox.EncodeBase64(bytes.Repeat([]byte{29}, 32))
	case 3:
		original.RecoverySignature = cryptox.EncodeBase64(make([]byte, 64))
	case 4:
		original.Proposal.IdempotencyKey = "server-forged-original-proposal"
		original.Proof.ProposalHash = environmentValue(original.Proposal.Hash(original.Proof.AccountID, original.Proof.AccountGeneration))
	case 5:
		var grants []cryptox.SignedGrantWire
		var envelopes []cryptox.RecoveryEnvelope
		if json.Unmarshal(body["currentGrants"], &grants) != nil || json.Unmarshal(body["environments"], &envelopes) != nil {
			return nil, errors.New("actual current grant or recovery envelope missing")
		}
		initialIDs := map[string]bool{}
		for _, e := range original.Proposal.Environments {
			initialIDs[e.EnvironmentID] = true
		}
		forgedID := ""
		for _, g := range grants {
			if initialIDs[g.Grant.EnvironmentID] || g.Grant.SubjectDeviceID != original.Proposal.Device.ID {
				continue
			}
			for _, envelope := range envelopes {
				if envelope.EnvironmentID == g.Grant.EnvironmentID {
					original.Proposal.Environments = append(original.Proposal.Environments, cryptox.InitializationEnvironment{EnvironmentID: envelope.EnvironmentID, KeyVersion: envelope.KeyVersion, RecoveryEnvelope: envelope.Envelope, Grant: g})
					forgedID = envelope.EnvironmentID
				}
			}
		}
		if forgedID == "" {
			return nil, errors.New("actual newly created root-signed environment missing")
		}
		// Hash() verifies every reused root grant and the original recovery root;
		// the forged shape is valid before the immutable dual proof is checked.
		hash, err := original.Proposal.Hash(original.Proof.AccountID, original.Proof.AccountGeneration)
		if err != nil {
			return nil, err
		}
		original.Proof.ProposalHash = hash
		var history []recoveryAttackGrantEvent
		if err = json.Unmarshal(body["grantHistory"], &history); err != nil {
			return nil, err
		}
		for i := range history {
			if history[i].Grant.Grant.EnvironmentID == forgedID {
				history[i].Sequence = 1
				history[i].Authorization = nil
			}
		}
		body["grantHistory"], err = json.Marshal(history)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.New("unknown synthetic recovery attack")
	}
	encoded, err := json.Marshal(original)
	if err != nil {
		return nil, err
	}
	body["originalInitialization"] = encoded
	return json.Marshal(body)
}

type recoveryAttackGrantEvent struct {
	Sequence      uint64                   `json:"sequence"`
	Grant         cryptox.SignedGrantWire  `json:"grant"`
	Authorization *cryptox.SignedGrantWire `json:"authorization"`
}

func TestMobileRecoveryFreshContextRejectsMissingForgedOriginalInitialization(t *testing.T) {
	for _, tt := range []struct {
		name string
		mode int32
	}{
		{"missing-original-two-signatures", 1},
		{"changed-original-proof-nonce", 2},
		{"forged-original-recovery-signature", 3},
		{"rehashed-original-proposal-old-device-signature", 4},
		{"new-root-env-relabelled-genesis-with-real-grants", 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newRealRecoveryFixture(t)
			if tt.mode == 5 {
				if _, err := f.root.CreateEnvironment(context.Background(), "不能伪装原初始化的合成环境", "root-later-env-forged-genesis"); err != nil {
					t.Fatal(err)
				}
			}
			fresh := f.freshRecoveryContext()
			f.vaultTamper.Store(tt.mode)
			if _, err := fresh.BeginRecovery(context.Background(), f.code); !errors.Is(err, mobileworkflow.ErrRecoveryEvidence) {
				t.Fatal("uncommitted initialization trusted in a fresh native context", err)
			}
			if info := environmentValue(fresh.RecoveryInfo()); info.State != "none" || info.TrustedDevice {
				t.Fatal("failed original proof retained recovery authority", info)
			}
			if _, err := fresh.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
				t.Fatal("failed original proof exposed decrypted cache", err)
			}
		})
	}
}

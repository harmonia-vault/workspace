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

	"github.com/harmonia-vault/core-go/localkeys"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 故障代理仅替换接受写入后的目录响应，不声称生产服务已发生这个竞态。
func TestNativeManagementLateDirectoryForbiddenIsNotApplied(t *testing.T) {
	for _, tc := range []struct {
		name, role, fault string
		invalid, applied  bool
		failFinal         bool
	}{
		{"revoked", "admin", "device_untrusted", true, false, false},
		{"arbitrary-forbidden", "ro", "request_rejected", false, false, false},
		{"still-admin", "admin", "admin_required", false, false, false},
		{"verified-self-downgrade", "ro", "admin_required", false, true, false},
		{"final-seal-failure", "ro", "admin_required", false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := startFixture(t, "--empty-vault", "--capture-email")
			var accepted, injected atomic.Bool
			var posts atomic.Int64
			var failFinal atomic.Bool
			failFinal.Store(tc.failFinal)
			proxy := httputil.NewSingleHostReverseProxy(environmentValue(url.Parse(fixture.Endpoint)))
			proxy.ModifyResponse = func(r *http.Response) error {
				if r.StatusCode == 200 && r.Request.Method == http.MethodPost && strings.HasSuffix(r.Request.URL.Path, "/grants") {
					accepted.Store(true)
					posts.Add(1)
				}
				if accepted.Load() && r.Request.Method == http.MethodGet && strings.HasSuffix(r.Request.URL.Path, "/grant-management") {
					injected.Store(true)
					_ = r.Body.Close()
					data := environmentValue(json.Marshal(map[string]string{"error": tc.fault}))
					r.StatusCode = http.StatusForbidden
					r.Body = io.NopCloser(bytes.NewReader(data))
					r.ContentLength = int64(len(data))
					r.Header.Set("Content-Length", strconv.Itoa(len(data)))
				}
				return nil
			}
			tls := httptest.NewTLSServer(proxy)
			defer tls.Close()
			keys := environmentValue(localkeys.GenerateDeviceKeys("fault-root-placeholder"))
			key := ed25519.NewKeyFromSeed(keys.SigningSeed)
			defer clear(key)
			seal, load := recoveryNativeSeal(t, "synthetic-management-directory-fault")
			save := func(data []byte) error {
				var state struct {
					Management *struct {
						History map[string]mobileworkflow.ManagementResult `json:"history"`
					} `json:"management"`
				}
				if e := json.Unmarshal(data, &state); e != nil {
					return e
				}
				if state.Management != nil && state.Management.History["directory-fault-grant"].Applied && failFinal.Swap(false) {
					return errors.New("synthetic final seal rejected")
				}
				return seal(data)
			}
			w := environmentValue(mobileworkflow.New(mobileworkflow.Config{Endpoint: tls.URL, HTTPClient: tls.Client(), SigningKey: key, ReceivingPrivateKey: keys.ReceivingPrivate, SaveProtectedState: save}))
			defer w.Close()
			email, password := "fault@example.invalid", "synthetic-directory-fault-password"
			registered := environmentValue(w.Register(ctx, email, password))
			var mails []struct{ To, Text string }
			if callJSON(t, tls.Client(), tls.URL, "/test/emails", "", "", "", nil, &mails) != 200 {
				t.Fatal("synthetic verification mail unavailable")
			}
			var proof mobileworkflow.EmailVerification
			for _, mail := range mails {
				if mail.To == email {
					for _, line := range strings.Split(mail.Text, "\n") {
						if code, ok := strings.CutPrefix(line, "验证码："); ok && len(code) == 8 {
							proof = mobileworkflow.EmailVerification{AccountID: registered.AccountID, AccountGeneration: registered.AccountGeneration, Code: code}
						}
					}
				}
			}
			originMust(t, w.VerifyEmail(ctx, proof))
			originMust(t, w.Login(ctx, email, password))
			code := environmentValue(w.BeginInitialization(ctx, "synthetic-directory-fault", "fault-init"))
			view := environmentValue(w.CompleteInitialization(ctx, code))
			var own struct {
				DeviceID string `json:"deviceId"`
			}
			data := load()
			originMust(t, json.Unmarshal(data, &own))
			clear(data)
			in := syncclient.GrantUpdateIntent{ID: "directory-fault-grant", EnvironmentID: view.Environments[0].ID, SubjectDeviceID: own.DeviceID, Role: tc.role}
			_ = environmentValue(w.PrepareDeviceGrant(ctx, in))
			result, err := w.RetryManagement(ctx, in.ID)
			if !accepted.Load() || !injected.Load() {
				t.Fatal("late directory fault window not exercised")
			}
			if result.Applied != tc.applied || (err == nil) != tc.applied {
				t.Fatal("late directory fault incorrectly reported applied", result.Applied, err)
			}
			if tc.failFinal {
				if !errors.Is(err, syncclient.ErrAcceptedNotApplied) || posts.Load() != 1 {
					t.Fatal("final seal failure lost accepted-not-applied", err)
				}
				done := environmentValue(w.RetryManagement(ctx, in.ID))
				if !done.Applied || posts.Load() != 1 {
					t.Fatal("native completion retry submitted a second grant")
				}
			}
			if tc.invalid {
				if !errors.Is(err, syncclient.ErrTrustInvalidated) {
					t.Fatal("lost trust invalidation sentinel", err)
				}
				var saved struct {
					Root       json.RawMessage `json:"root"`
					Management json.RawMessage `json:"management"`
					Cloud      struct {
						AccountClosed bool `json:"accountClosed"`
					} `json:"cloud"`
				}
				data := load()
				originMust(t, json.Unmarshal(data, &saved))
				clear(data)
				if !saved.Cloud.AccountClosed || len(saved.Root) != 0 && string(saved.Root) != "null" || len(saved.Management) != 0 && string(saved.Management) != "null" {
					t.Fatal("late fault retained protected root or management journal")
				}
			}
		})
	}
}

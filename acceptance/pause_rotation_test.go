package acceptance

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localkeys"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/pairing"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 从空SQLite经真实注册/邮件证明/首次双签初始化建立根，不种入受信设备。
func newPauseOriginFixture(t *testing.T) (*originFixture, *originActor, string) {
	t.Helper()
	ctx := context.Background()
	fixture := startFixture(t, "--empty-vault", "--capture-email")
	if len(fixture.Devices) != 0 || len(fixture.Grants) != 0 {
		t.Fatal("empty-vault unexpectedly seeded trusted devices")
	}
	f := &originFixture{t: t, email: "origin-v3@example.invalid", password: "synthetic-origin-password"}
	backend := environmentValue(url.Parse(fixture.Endpoint))
	handler := httputil.NewSingleHostReverseProxy(backend)
	handler.ModifyResponse = func(response *http.Response) error {
		path := response.Request.URL.Path
		lose := false
		if response.Request.Method == "POST" && response.StatusCode == 200 {
			if strings.HasSuffix(path, "/boot-sessions") {
				f.boots.Add(1)
			}
			if strings.Contains(path, "/pairings-v5/") && strings.HasSuffix(path, "/complete") {
				f.enrollmentPosts.Add(1)
				lose = f.loseEnrollment.Swap(false)
			}
			if strings.HasSuffix(path, "/environment-changes-v4") {
				f.environmentPosts.Add(1)
				lose = f.loseEnvironment.Swap(false)
			}
		}
		if lose {
			_ = response.Body.Close()
			response.StatusCode = 504
			response.Body = io.NopCloser(strings.NewReader(`{"error":"request_rejected"}`))
			response.ContentLength = -1
			response.Header.Del("Content-Length")
		}
		return nil
	}
	f.proxy = httptest.NewTLSServer(handler)
	t.Cleanup(f.proxy.Close)
	rootKeys := environmentValue(localkeys.GenerateDeviceKeys("temporary-root-key-placeholder"))
	rootKey := ed25519.NewKeyFromSeed(rootKeys.SigningSeed)
	t.Cleanup(func() { clear(rootKey) })
	save, _ := recoveryNativeSeal(t, "synthetic-origin-root-native-boundary")
	workflow := environmentValue(mobileworkflow.New(mobileworkflow.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), SigningKey: rootKey, ReceivingPrivateKey: rootKeys.ReceivingPrivate, SaveProtectedState: save}))
	defer workflow.Close()
	registered := environmentValue(workflow.Register(ctx, f.email, f.password))
	f.account = registered.AccountID
	f.generation = registered.AccountGeneration
	if !registered.VerificationRequired || f.generation != "1" {
		t.Fatal("real registration policy/generation mismatch")
	}
	var mail []struct {
		To   string `json:"to"`
		Text string `json:"text"`
	}
	if got := callJSON(t, f.proxy.Client(), f.proxy.URL, "/test/emails", "", "", "", nil, &mail); got != 200 {
		t.Fatal(got)
	}
	var proof mobileworkflow.EmailVerification
	for _, message := range mail {
		if message.To == f.email {
			for _, line := range strings.Split(message.Text, "\n") {
				if code, ok := strings.CutPrefix(line, "验证码："); ok && len(code) == 8 {
					proof = mobileworkflow.EmailVerification{AccountID: registered.AccountID, AccountGeneration: registered.AccountGeneration, Code: code}
				}
			}
		}
	}
	if proof.AccountID != f.account || proof.Code == "" {
		t.Fatal("real registration verification proof missing")
	}
	originMust(t, workflow.VerifyEmail(ctx, proof))
	originMust(t, workflow.Login(ctx, f.email, f.password))
	recoveryCode := environmentValue(workflow.BeginInitialization(ctx, "X_PRIVATE_LABEL_ORIGIN_TEST", "origin-first-init"))
	view := environmentValue(workflow.CompleteInitialization(ctx, recoveryCode))
	recoveryCode = ""
	if len(view.Environments) != 1 {
		t.Fatal("first initialization did not create exactly one environment")
	}
	x := view.Environments[0].ID
	_, e := workflow.SetVariable(ctx, x, "X_PRIVATE_NAME_ORIGIN_TEST", "synthetic-origin-X-value", "origin-X-put")
	originMust(t, e)
	var native struct {
		Initialization     cryptox.OriginalInitialization `json:"initialization"`
		AccountID          string                         `json:"accountId"`
		AccountGeneration  string                         `json:"accountGeneration"`
		DeviceID           string                         `json:"deviceId"`
		Root               *cryptox.TrustRoot             `json:"root"`
		InitialAuthorities []cryptox.SignedGrantWire      `json:"initialAuthorities"`
	}
	exported := environmentValue(workflow.ExportProtectedState())
	originMust(t, json.Unmarshal(exported, &native))
	clear(exported)
	if native.Root == nil || len(native.InitialAuthorities) != 1 {
		t.Fatal("exact accepted genesis evidence missing")
	}
	f.root = *native.Root
	f.initial = native.InitialAuthorities
	rootKeys.DeviceID = native.DeviceID
	a := &originActor{keys: rootKeys, key: rootKey, engine: environmentValue(localstate.New(&originMemoryStore{state: localstate.EmptyState()}))}
	a.verifier = environmentValue(syncclient.NewRootDAGPinnedVerifier(syncclient.PinnedTrust{AccountID: f.account, AccountGeneration: 1, DeviceID: a.keys.DeviceID, DeviceSigningPublicKey: a.keys.SigningPublic, ReceivingPrivateKey: a.keys.ReceivingPrivate}, native.Initialization))
	t.Cleanup(a.verifier.Close)
	f.boot(a)

	f.pull(a)
	return f, a, x
}

type pauseMemoryProvider struct{ values map[string]string }

func (p *pauseMemoryProvider) Snapshot(_ context.Context, names []string) (map[string]string, error) {
	out := map[string]string{}
	for _, name := range names {
		if v, ok := p.values[name]; ok {
			out[name] = v
		}
	}
	return out, nil
}
func (p *pauseMemoryProvider) Apply(_ context.Context, changes []localstate.Change) error {
	for _, change := range changes {
		if change.Value == nil {
			delete(p.values, change.Name)
		} else {
			p.values[change.Name] = *change.Value
		}
	}
	return nil
}

func TestNativePausedRotationRetainsSourceThenRevokeDeleteExpireAndCaps(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("须固定 BoringSSL 原生构建；默认构建不代表 PAKE 验收")
	}
	for _, kind := range []string{"revoke", "delete", "expiry", "caps"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			f, a, env := newPauseOriginFixture(t)
			role, deadline := "ro", int64(0)
			if kind == "caps" {
				role = "admin"
			}
			if kind == "expiry" {
				deadline = time.Now().Unix() + 15
			}
			d := f.enroll(a, "pause-"+kind, env, role, strconv.FormatInt(deadline, 10), false)
			name := "X_PRIVATE_NAME_ORIGIN_TEST"
			provider := &pauseMemoryProvider{values: map[string]string{name: "synthetic-shell-original", "UNRELATED": "synthetic-unrelated"}}
			originMust(t, d.engine.Activate(env, 10, time.Now()))
			originMust(t, d.engine.SetOverride(env, name, "synthetic-pause-override", time.Now()))
			originMust(t, d.engine.Reconcile(ctx, provider, time.Now()))
			originMust(t, d.engine.SetPaused(true))
			before := d.engine.State().Cloud
			if kind == "caps" {
				lowered := environmentValue(a.client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: "pause-lower", EnvironmentID: env, SubjectDeviceID: d.keys.DeviceID, Role: "rw", ExpiresAt: time.Now().Unix() + 3600}, a.key))
				accepted := submitSealedGrant(t, ctx, lowered)
				if !environmentValue(lowered.Confirm(ctx, accepted)).Applied {
					t.Fatal("lower grant did not converge")
				}
				environmentValue(d.client.RefreshAuthorizations(ctx))
			}
			f.rotate(a, env, "synthetic-paused-label", "pause-rotate-"+kind)
			environmentValue(d.client.RefreshAuthorizations(ctx))
			paused := d.engine.State().Cloud
			old, next := before.Environments[env], paused.Environments[env]
			if next.ID != env || next.KeyVersion != old.KeyVersion || next.GrantGeneration != old.GrantGeneration || !reflect.DeepEqual(next.Values, old.Values) || next.Source == nil || old.Source == nil || next.Source.AuthorityHash != old.Source.AuthorityHash || next.Source.Fingerprint != old.Source.Fingerprint || paused.Sequence != before.Sequence || !reflect.DeepEqual(paused.SeenMutations, before.SeenMutations) || paused.AuthorizationSequence <= before.Sequence || f.grant(d, env).Grant.KeyVersion != "2" {
				t.Fatal("paused origin rotation did not retain exact old source/current authorization split")
			}
			originMust(t, d.engine.Reconcile(ctx, provider, time.Now()))
			if provider.values[name] != "synthetic-pause-override" {
				t.Fatal("rotation changed current configuration during pause")
			}
			f.restart(d)
			if !d.engine.State().Paused {
				t.Fatal("sealed restart lost pause")
			}
			switch kind {
			case "revoke":
				transaction := environmentValue(a.client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: "pause-revoke", EnvironmentID: env, SubjectDeviceID: d.keys.DeviceID, Role: "none"}, a.key))
				accepted := submitSealedGrant(t, ctx, transaction)
				if !environmentValue(transaction.Confirm(ctx, accepted)).Applied {
					t.Fatal("signed none not accepted")
				}
				environmentValue(d.client.RefreshAuthorizations(ctx))
			case "delete":
				f.create(a, env, "pause-spare-environment", "synthetic-spare-label", "pause-create-spare", false)
				control := environmentValue(a.client.EnvironmentControl(ctx, env))
				own := f.grant(a, env).Grant
				change := cryptox.EnvironmentChange{AccountID: f.account, AccountGeneration: f.generation, DeviceID: a.keys.DeviceID, EnvironmentID: env, Operation: "delete", AuthorityEnvironmentID: env, AuthorityKeyVersion: own.KeyVersion, AuthorityGrantGeneration: own.GrantGeneration, PreviousKeyVersion: own.KeyVersion, KeyVersion: own.KeyVersion, ExpectedSequence: strconv.FormatUint(control.Sequence, 10), IdempotencyKey: "pause-delete", RecoveryGeneration: f.root.RecoveryGeneration, Grants: []cryptox.SignedGrantWire{}, Mutations: []cryptox.SignedMutationWire{}}
				signed := environmentValue(cryptox.SignEnvironmentChange(change, a.key))
				if !environmentValue(a.client.SubmitEnvironmentChange(ctx, signed)).Applied {
					t.Fatal("signed deletion not accepted")
				}
				environmentValue(d.client.RefreshAuthorizations(ctx))
				if d.engine.State().Cloud.DeletedEnvironments[env] == 0 {
					t.Fatal("signed deletion tombstone missing")
				}
			case "expiry":
				// 离线到期先清本地，再通过实际HTTPS授权端点核对服务端拒绝读授权。
				due := time.Unix(deadline, 0)
				originMust(t, d.engine.Reconcile(ctx, provider, due))
				if len(d.engine.State().Cloud.Environments) != 0 || len(d.engine.State().Overrides) != 0 {
					t.Fatal("offline expiry retained source or override")
				}
				if wait := time.Until(due); wait > 0 {
					timer := time.NewTimer(wait + 50*time.Millisecond)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				environmentValue(d.client.RefreshAuthorizations(ctx))
			case "caps":
				capped := d.engine.State().Cloud.Environments[env]
				if capped.Role != localstate.ReadWrite || capped.ExpiresAt == nil {
					t.Fatal("pause failed lower role/deadline")
				}
				transaction := environmentValue(a.client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: "pause-upgrade", EnvironmentID: env, SubjectDeviceID: d.keys.DeviceID, Role: "admin"}, a.key))
				accepted := submitSealedGrant(t, ctx, transaction)
				if !environmentValue(transaction.Confirm(ctx, accepted)).Applied {
					t.Fatal("signed upgrade not accepted")
				}
				environmentValue(d.client.RefreshAuthorizations(ctx))
				final := d.engine.State().Cloud.Environments[env]
				if final.Role != localstate.ReadWrite || final.ExpiresAt == nil || !final.ExpiresAt.Equal(*capped.ExpiresAt) || final.KeyVersion != old.KeyVersion || final.GrantGeneration != old.GrantGeneration {
					t.Fatal("pause accepted role/expiry expansion")
				}
				f.restart(d)
				originMust(t, d.engine.SetPaused(false))
				f.pull(d)
				resumed := d.engine.State().Cloud.Environments[env]
				if resumed.Role != localstate.Admin || resumed.ExpiresAt != nil || resumed.KeyVersion != 2 || len(resumed.Source.AuthorizationPath) != 1 {
					t.Fatal("full verified resume failed to reset data source/ceilings")
				}
				return
			}
			originMust(t, d.engine.Reconcile(ctx, provider, time.Now()))
			state := d.engine.State()
			if len(state.Cloud.Environments) != 0 || len(state.Overrides) != 0 || len(state.Managed) != 0 || provider.values[name] != "synthetic-shell-original" || provider.values["UNRELATED"] != "synthetic-unrelated" || state.Cloud.Sequence != before.Sequence || !reflect.DeepEqual(state.Cloud.SeenMutations, before.SeenMutations) {
				t.Fatal("paused safety update failed per-key restore or advanced ordinary data")
			}
			originMust(t, d.verifier.ValidateStoredIssuerEvidence(state.Cloud))
		})
	}
}

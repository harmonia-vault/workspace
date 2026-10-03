//go:build harmonia_boringssl

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localkeys"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 此联合主项的native业务adapter，不是手机SDK/Save，也不把grant塞恢复checkedjournal。
type dagManagementBound struct {
	Generation  uint64
	Fingerprint string
}

type dagManagementRecord struct {
	Endpoint          string
	AccountID         string
	AccountGeneration uint64
	DeviceID          string
	Epoch             uint64
	ID                string
	Hash              string
	Packet            []byte
	Highest           syncclient.ManagementControl
	Bounds            map[string]dagManagementBound
	Attempted         bool
}

func TestNativeP4ManagementRolesOriginalReceiptAndPausedNone(t *testing.T) {
	f := newMobileManagerFixture(t)
	var identity struct {
		AccountID string `json:"accountId"`
	}
	raw := f.rootActor.load()
	originMust(t, json.Unmarshal(raw, &identity))
	clear(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	b, code := recoverDAGActor(t, ctx, f, identity.AccountID, f.code, "management-B", false)
	f.root.Close()
	b.verifier.Close()
	c, _ := recoverDAGActor(t, ctx, f, identity.AccountID, code, "management-C", false)
	env := "dag-managed-Z"
	created := dagEnvironmentChange(t, ctx, c, f.initial, env, "create", "management-create-Z")
	if !environmentValue(c.client.SubmitEnvironmentChangeV4(ctx, created)).Applied {
		t.Fatal("create not applied")
	}
	d := dagEnvironmentEnrollRO(t, ctx, f, c, env)
	var highest syncclient.ManagementControl
	var record dagManagementRecord
	bounds := map[string]dagManagementBound{}
	rememberBounds := func(live syncclient.ManagementControl) {
		t.Helper()
		for _, row := range live.Subjects {
			gg := environmentValue(strconv.ParseUint(row.HighestGrantGeneration, 10, 64))
			prior := bounds[row.DeviceID]
			hash := ""
			if row.CurrentGrant != nil {
				hash = environmentValue(syncclient.GrantContentHash(*row.CurrentGrant))
			}
			if gg < prior.Generation || gg == prior.Generation && gg != 0 && hash != prior.Fingerprint {
				t.Fatal("sealed management bounds reset or replaced")
			}
			if gg != 0 {
				bounds[row.DeviceID] = dagManagementBound{gg, hash}
			}
		}
	}

	prepare := func(id, role string, expires int64) *syncclient.GrantUpdateTransaction {
		t.Helper()
		_ = environmentValue(c.client.Pull(ctx))
		live := environmentValue(c.client.ManagementControl(ctx, env))
		if highest.AccountID != "" {
			originMust(t, c.client.CheckManagementControlLowerBounds(live, highest))
		}
		rememberBounds(live)
		highest = live
		tx := environmentValue(c.client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: id, EnvironmentID: env, SubjectDeviceID: d.id, Role: role, ExpiresAt: expires}, c.key))
		record = dagManagementRecord{Endpoint: f.proxy.URL, AccountID: identity.AccountID, AccountGeneration: 1, DeviceID: c.id, Epoch: c.engine.State().SessionEpoch, ID: id, Hash: environmentValue(tx.ContentHash()), Packet: environmentValue(tx.ProtectedBytes()), Highest: highest, Bounds: bounds}
		originMust(t, c.vault.Save("writes-v1", environmentValue(json.Marshal(record))))
		return tx
	}
	barrier := func() error {
		record.Attempted = true
		return c.vault.Save("writes-v1", environmentValue(json.Marshal(record)))
	}
	remember := func() {
		t.Helper()
		live := environmentValue(c.client.ManagementControl(ctx, env))
		originMust(t, c.client.CheckManagementControlLowerBounds(live, highest))
		rememberBounds(live)
		highest = live
		record.Highest = highest
		record.Bounds = bounds
		originMust(t, c.vault.Save("writes-v1", environmentValue(json.Marshal(record))))
	}
	tx := prepare("management-promote-RW", "rw", time.Now().Add(10*time.Minute).Unix())
	var posts atomic.Int64
	lost := atomic.Bool{}
	lost.Store(true)
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/grants") {
			posts.Add(1)
			if r.StatusCode == 200 && lost.Swap(false) {
				_ = r.Body.Close()
				v := []byte(`{"error":"request_rejected"}`)
				r.StatusCode = 504
				r.Body = io.NopCloser(bytes.NewReader(v))
				r.ContentLength = int64(len(v))
				r.Header.Set("Content-Length", strconv.Itoa(len(v)))
			}
		}
		return nil
	}})
	failedBarrier := errors.New("synthetic native business save failed")
	if _, e := tx.SubmitWithBarrier(ctx, func() error { return failedBarrier }); !errors.Is(e, failedBarrier) || posts.Load() != 0 {
		t.Fatal("failed native barrier sent grant")
	}
	if _, e := tx.SubmitWithBarrier(ctx, barrier); e == nil || posts.Load() != 1 {
		t.Fatal("unknown acceptance gate failed")
	}
	f.responseHook.Store(nil)
	directory := c.vault.Directory()
	originMust(t, c.vault.Close())
	c.vault = environmentValue(localkeys.Open(localkeys.Config{Directory: directory, UserID: environmentValue(localkeys.CurrentUserID())}))
	t.Cleanup(func() { _ = c.vault.Close() })
	saved := environmentValue(c.vault.Load("writes-v1"))
	originMust(t, json.Unmarshal(saved, &record))
	clear(saved)
	highest = record.Highest
	bounds = record.Bounds
	if bounds == nil {
		t.Fatal("protected bounds missing after restart")
	}
	c.engine = environmentValue(localstate.New(&dagAcceptanceStore{vault: c.vault}))
	c.client = environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: identity.AccountID, AccountGeneration: 1, DeviceID: c.id, Verifier: c.verifier, Engine: c.engine}))
	c.client = environmentValue(c.client.BootDevice(ctx, c.key))
	if record.Endpoint != f.proxy.URL || record.AccountID != identity.AccountID || record.DeviceID != c.id || record.Epoch != c.engine.State().SessionEpoch || !record.Attempted {
		t.Fatal("protected business tuple changed")
	}
	tx = environmentValue(c.client.RestoreGrantUpdate(record.Packet))
	if environmentValue(tx.ContentHash()) != record.Hash {
		t.Fatal("original grant hash changed")
	}
	status := environmentValue(tx.Status(ctx))
	if !status.Accepted {
		t.Fatal("original accepted receipt missing")
	}
	if !environmentValue(tx.Confirm(ctx, syncclient.Acceptance{Sequence: status.Sequence, Replayed: true})).Applied || posts.Load() != 1 {
		t.Fatal("originalID retry reposted or not applied")
	}
	remember()
	_ = environmentValue(d.client.Pull(ctx))
	if d.engine.State().Cloud.Environments[env].Role != localstate.ReadWrite {
		t.Fatal("fresh signed grant did not change RO to RW")
	}
	writer := environmentValue(syncclient.NewWriter(identity.AccountID, 1, d.id, d.engine.State().SessionEpoch, d.key, dagAcceptanceWriteJournal{d.vault}))
	defer writer.Close()
	if !environmentValue(writer.Execute(ctx, d.client, syncclient.WriteRequest{ID: "management-RW-value", Operation: "put", EnvironmentID: env, Name: "SYNTHETIC_MANAGED", Value: "synthetic-managed-value"})).Applied {
		t.Fatal("RW write not applied")
	}
	dagStage(t, "P4 native barrier0POST/504 sealed restart/originalreceipt/RW HPKE write", nil)
	// 服务器接受后native权威保存失败必须保持未应用，不能用当前role猜成功。
	tx = prepare("management-temporary-admin", "admin", time.Now().Add(9*time.Minute).Unix())
	faultStore := &dagEnvironmentSaveFault{dagAcceptanceStore: &dagAcceptanceStore{vault: c.vault}}
	c.engine = environmentValue(localstate.New(faultStore))
	c.client = environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: identity.AccountID, AccountGeneration: 1, DeviceID: c.id, Verifier: c.verifier, Engine: c.engine}))
	c.client = environmentValue(c.client.BootDevice(ctx, c.key))
	tx = environmentValue(c.client.RestoreGrantUpdate(record.Packet))
	accepted := environmentValue(tx.SubmitWithBarrier(ctx, barrier))
	oldSeq := c.engine.State().Cloud.Sequence
	faultStore.fail.Store(true)
	failed, e := tx.Confirm(ctx, accepted)
	if !errors.Is(e, syncclient.ErrAcceptedNotApplied) || failed.Applied || c.engine.State().Cloud.Sequence != oldSeq {
		t.Fatal("native Save failure claimed applied")
	}
	faultStore.fail.Store(false)
	if !environmentValue(tx.Confirm(ctx, accepted)).Applied {
		t.Fatal("original receipt did not restore final native save")
	}
	remember()
	_ = environmentValue(d.client.Pull(ctx))
	if d.engine.State().Cloud.Environments[env].Role != localstate.Admin {
		t.Fatal("Admin not received")
	}
	// 临时Admin不能对C授永久；这个检查使用真实DAG控制/设备身份和在线状态。
	if _, e = d.client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: "management-no-permanent", EnvironmentID: env, SubjectDeviceID: c.id, Role: "admin", ExpiresAt: 0}, d.key); !errors.Is(e, syncclient.ErrWritePermission) {
		t.Fatal("temporary Admin delegated permanent permission")
	}
	tx = prepare("management-demote-RO", "ro", time.Now().Add(8*time.Minute).Unix())
	accepted = environmentValue(tx.SubmitWithBarrier(ctx, barrier))
	if !environmentValue(tx.Confirm(ctx, accepted)).Applied {
		t.Fatal("RO downgrade not applied")
	}
	remember()
	// 不先刷新D，以旧本地RW/Admin发出的显式mutation仍被服务器逐次拒绝。
	own := dagEnvironmentTarget(t, d, env).Grant
	key := dagEnvironmentKey(t, d, env)
	payload := environmentValue(cryptox.EncryptValue(key, cryptox.ValueContext{AccountID: identity.AccountID, AccountGeneration: "1", EnvironmentID: env, KeyVersion: own.KeyVersion, Name: "SYNTHETIC_DENIED"}, []byte("synthetic-denied")))
	clear(key)
	mutation := environmentValue(cryptox.SignMutation(cryptox.Mutation{AccountID: identity.AccountID, AccountGeneration: "1", DeviceID: d.id, EnvironmentID: env, KeyVersion: own.KeyVersion, GrantGeneration: own.GrantGeneration, Operation: "put", IdempotencyKey: "management-stale-RW-denied", Name: "SYNTHETIC_DENIED", Payload: cryptox.EncodeBase64(payload)}, d.key))
	if _, e = d.client.Submit(ctx, syncclient.SignedMutation{Mutation: mutation.Mutation, Signature: mutation.Signature}); e == nil {
		t.Fatal("server accepted stale local Admin after downgrade")
	}
	_ = environmentValue(d.client.Pull(ctx))
	if d.engine.State().Cloud.Environments[env].Role != localstate.ReadOnly {
		t.Fatal("RO downgrade absent")
	}
	originMust(t, d.engine.Activate(env, 10, time.Now()))
	originMust(t, d.engine.SetOverride(env, "SYNTHETIC_MANAGED", "synthetic-override", time.Now()))
	provider := &pauseMemoryProvider{values: map[string]string{"SYNTHETIC_MANAGED": "synthetic-original", "SYNTHETIC_UNRELATED": "keep"}}
	originMust(t, d.engine.Reconcile(ctx, provider, time.Now()))
	originMust(t, d.engine.SetPaused(true))
	before := d.engine.State().Cloud
	tx = prepare("management-none", "none", 0)
	accepted = environmentValue(tx.SubmitWithBarrier(ctx, barrier))
	if !environmentValue(tx.Confirm(ctx, accepted)).Applied {
		t.Fatal("none not accepted")
	}
	remember()
	_ = environmentValue(d.client.RefreshAuthorizations(ctx))
	originMust(t, d.engine.Reconcile(ctx, provider, time.Now()))
	now := d.engine.State()
	if len(now.Cloud.Environments) != 0 || len(now.Overrides) != 0 || now.Cloud.Sequence != before.Sequence || !bytes.Equal(environmentValue(json.Marshal(now.Cloud.SeenMutations)), environmentValue(json.Marshal(before.SeenMutations))) || provider.values["SYNTHETIC_MANAGED"] != "synthetic-original" || provider.values["SYNTHETIC_UNRELATED"] != "keep" {
		t.Fatal("paused none changed data or failed per-key restore")
	}
	// none之后轮换保留旧KV/最高GG的撤销记录，再明确授新KV/GG+1，不复活旧source。
	rotated := dagEnvironmentChange(t, ctx, c, env, env, "rotate", "management-rotate-after-none")
	if !environmentValue(c.client.SubmitEnvironmentChangeV4(ctx, rotated)).Applied {
		t.Fatal("rotation after none not applied")
	}
	tx = prepare("management-explicit-restore-RO", "ro", time.Now().Add(7*time.Minute).Unix())
	accepted = environmentValue(tx.SubmitWithBarrier(ctx, barrier))
	if !environmentValue(tx.Confirm(ctx, accepted)).Applied {
		t.Fatal("explicit restoration not applied")
	}
	remember()
	originMust(t, d.engine.SetPaused(false))
	_ = environmentValue(d.client.Pull(ctx))
	envState := d.engine.State().Cloud.Environments[env]
	if envState.Role != localstate.ReadOnly || envState.KeyVersion != 2 || envState.GrantGeneration != 6 || envState.Values["SYNTHETIC_MANAGED"] != "synthetic-managed-value" {
		t.Fatal("none/oldKV lost highestGG or true reencrypted data")
	}
	originMust(t, c.verifier.ValidateStoredIssuerEvidence(c.engine.State().Cloud))
	originMust(t, d.verifier.ValidateStoredIssuerEvidence(d.engine.State().Cloud))
	dagStage(t, "temporaryAdmin/RO stalewrite rejection/pausednone/original restore/oldKV highestGG", nil)
}

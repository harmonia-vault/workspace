//go:build harmonia_boringssl

package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localkeys"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/pairing"
	"github.com/harmonia-vault/core-go/syncclient"
)

func dagEnvironmentTarget(t *testing.T, a *dagAcceptanceActor, env string) cryptox.SignedGrantWire {
	t.Helper()
	p := environmentValue(a.client.CurrentIssuerDAGEvidence())
	for _, target := range p.Source.View.Targets {
		if target.EnvironmentID == env {
			for _, n := range p.Source.View.Authorities {
				if environmentValue(cryptox.IssuerAuthorityHash(n.Grant)) == target.AuthorityHash {
					return n.Grant
				}
			}
		}
	}
	t.Fatal("current P4 actor target missing")
	return cryptox.SignedGrantWire{}
}
func dagEnvironmentKey(t *testing.T, a *dagAcceptanceActor, env string) []byte {
	t.Helper()
	g := dagEnvironmentTarget(t, a, env).Grant
	return environmentValue(cryptox.UnwrapEnvironmentKey(a.receiving, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: env, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: a.id, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey}, environmentValue(cryptox.DecodeBase64(g.Envelope, 80, 80))))
}
func dagEnvironmentChange(t *testing.T, ctx context.Context, a *dagAcceptanceActor, authorityEnv, env, operation, id string) cryptox.EnvironmentChangeV2 {
	t.Helper()
	_ = environmentValue(a.client.Pull(ctx))
	control := environmentValue(a.client.EnvironmentControl(ctx, authorityEnv))
	if control.IssuerDAGEvidence == nil {
		t.Fatal("P4 control downgraded")
	}
	actor := dagEnvironmentTarget(t, a, authorityEnv).Grant
	previous, version := "0", "1"
	if operation == "rotate" {
		previous = actor.KeyVersion
		version = strconv.FormatUint(environmentValue(strconv.ParseUint(previous, 10, 64))+1, 10)
	}
	key := environmentValue(cryptox.GenerateEnvironmentKey())
	defer clear(key)
	root := control.IssuerDAGEvidence.Source.View.TrustRoot
	label := environmentValue(cryptox.EncryptEnvironmentLabel(key, cryptox.EnvironmentLabelContext{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, EnvironmentID: env, KeyVersion: version}, []byte("合成P4环境")))
	change := cryptox.EnvironmentChange{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, DeviceID: a.id, EnvironmentID: env, Operation: operation, AuthorityEnvironmentID: authorityEnv, AuthorityKeyVersion: actor.KeyVersion, AuthorityGrantGeneration: actor.GrantGeneration, PreviousKeyVersion: previous, KeyVersion: version, ExpectedSequence: strconv.FormatUint(control.Sequence, 10), IdempotencyKey: id, LabelPayload: cryptox.EncodeBase64(label), RecoveryGeneration: root.RecoveryGeneration, Grants: []cryptox.SignedGrantWire{}, Mutations: []cryptox.SignedMutationWire{}}
	change.RecoveryEnvelope = cryptox.EncodeBase64(environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, EnvironmentID: env, KeyVersion: version, RecipientType: "recovery", RecipientID: actor.AccountID, RecipientGeneration: root.RecoveryGeneration, RecipientPublicKey: root.RecoveryReceivingPublicKey})))
	recipients := control.Grants
	if operation == "create" {
		recipients = []cryptox.SignedGrantWire{{Grant: actor}}
	}
	ownGeneration := ""
	for i, prior := range recipients {
		g := prior.Grant
		g.IssuerDeviceID = a.id
		g.EnvironmentID = env
		g.KeyVersion = version
		g.IdempotencyKey = id + "-g-" + strconv.Itoa(i)
		g.GrantGeneration = "1"
		if operation == "rotate" {
			g.GrantGeneration = strconv.FormatUint(environmentValue(strconv.ParseUint(prior.Grant.GrantGeneration, 10, 64))+1, 10)
		}
		if operation == "create" {
			g.SubjectDeviceID = a.id
			g.Role = "admin"
		}
		g.Envelope = cryptox.EncodeBase64(environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: env, KeyVersion: version, RecipientType: "device", RecipientID: g.SubjectDeviceID, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey})))
		change.Grants = append(change.Grants, cryptox.GrantToWire(environmentValue(cryptox.SignGrant(g, a.key))))
		if g.SubjectDeviceID == a.id {
			ownGeneration = g.GrantGeneration
		}
	}
	if operation == "rotate" {
		values := a.engine.State().Cloud.Environments[env].Values
		names := []string{}
		for n := range values {
			names = append(names, n)
		}
		sort.Strings(names)
		for i, n := range names {
			payload := environmentValue(cryptox.EncryptValue(key, cryptox.ValueContext{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, EnvironmentID: env, KeyVersion: version, Name: n}, []byte(values[n])))
			m := environmentValue(cryptox.SignMutation(cryptox.Mutation{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, DeviceID: a.id, EnvironmentID: env, KeyVersion: version, GrantGeneration: ownGeneration, Operation: "put", IdempotencyKey: id + "-v-" + strconv.Itoa(i), Name: n, Payload: cryptox.EncodeBase64(payload)}, a.key))
			change.Mutations = append(change.Mutations, cryptox.MutationToWire(m))
		}
	}
	signed := environmentValue(cryptox.SignEnvironmentChange(change, a.key))
	return environmentValue(a.client.PrepareEnvironmentChangeV4(ctx, signed, a.key))
}
func dagEnvironmentEnrollRO(t *testing.T, ctx context.Context, f *mobileManagerFixture, c *dagAcceptanceActor, env string) *dagAcceptanceActor {
	t.Helper()
	v := newDAGLabVault(t)
	keys := environmentValue(localkeys.GenerateDeviceKeys("dag-environment-reader"))
	originMust(t, v.SaveDeviceKeys(keys))
	key := ed25519.NewKeyFromSeed(keys.SigningSeed)
	t.Cleanup(func() { clear(key); clear(keys.SigningSeed) })
	sum := sha256.Sum256([]byte(f.password))
	login := environmentValue(syncclient.Login(ctx, syncclient.LoginConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), Email: f.email, Credential: hex.EncodeToString(sum[:])}))
	engine := environmentValue(localstate.New(&dagAcceptanceStore{vault: v}))
	enrollment := environmentValue(syncclient.NewEnrollmentV5(syncclient.EnrollmentConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: login.AccountID, AccountGeneration: 1, DeviceID: keys.DeviceID, LoginToken: login.Token, SigningKey: key, ReceivingPrivateKey: keys.ReceivingPrivate, Engine: engine}))
	defer enrollment.Close()
	code := environmentValue(pairing.GenerateShortCode())
	defer clear(code)
	id := "dag-environment-pair-RO"
	_ = environmentValue(enrollment.Begin(ctx, c.id, id, code))
	approver := environmentValue(c.client.NewApproverV5(id, c.key))
	defer approver.Close()
	finished := make(chan error, 1)
	go func() { _, e := approver.Confirm(ctx, code); finished <- e }()
	for {
		s := environmentValue(enrollment.Advance(ctx))
		if s.Confirmations["initiator"] != "" && s.Confirmations["approver"] != "" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("native PAKE deadline")
		case <-time.After(10 * time.Millisecond):
		}
	}
	originMust(t, <-finished)
	g := dagEnvironmentTarget(t, c, env).Grant
	envKey := dagEnvironmentKey(t, c, env)
	defer clear(envKey)
	g.IssuerDeviceID = c.id
	g.SubjectDeviceID = keys.DeviceID
	g.SubjectSigningPublicKey = cryptox.EncodeBase64(keys.SigningPublic)
	g.SubjectReceivingPublicKey = cryptox.EncodeBase64(keys.ReceivingPublic)
	g.Role = "ro"
	g.ExpiresAt = strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	g.GrantGeneration = "1"
	g.IdempotencyKey = id + "-grant"
	g.Envelope = cryptox.EncodeBase64(environmentValue(cryptox.WrapEnvironmentKey(envKey, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: env, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: g.SubjectDeviceID, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey})))
	approval := environmentValue(approver.PrepareApproval([]cryptox.SignedGrantWire{cryptox.GrantToWire(environmentValue(cryptox.SignGrant(g, c.key)))}))
	_ = environmentValue(approver.Submit(ctx, approval))
	_ = environmentValue(enrollment.Advance(ctx))
	receipt := environmentValue(enrollment.Receipt())
	originMust(t, v.SaveTrustContext(localkeys.TrustContext{Endpoint: f.proxy.URL, AccountID: login.AccountID, AccountGeneration: 1, DeviceID: keys.DeviceID, SigningPublic: keys.SigningPublic, ReceivingPublic: keys.ReceivingPublic, CertificateVersion: "5", PairingProfile: pairing.Profile, EnrollmentCertificate: environmentValue(json.Marshal(receipt)), EnrollmentKey: id, Accepted: false}))
	completed := environmentValue(enrollment.Complete(ctx))
	t.Cleanup(completed.Verifier.Close)
	trust := environmentValue(v.LoadTrustContext())
	trust.Accepted = true
	originMust(t, v.SaveTrustContext(trust))
	client := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: login.AccountID, AccountGeneration: 1, DeviceID: keys.DeviceID, Verifier: completed.Verifier, Engine: engine}))
	client = environmentValue(client.BootDevice(ctx, key))
	_ = environmentValue(client.Pull(ctx))
	if len(engine.State().Cloud.Environments) != 1 || engine.State().Cloud.Environments[env].Role != localstate.ReadOnly {
		t.Fatal("V5 RO reader gained unselected environment")
	}
	return &dagAcceptanceActor{client: client, engine: engine, verifier: completed.Verifier, id: keys.DeviceID, key: key, receiving: keys.ReceivingPrivate, vault: v}
}

// 仅本联合测试的环境业务record：nativeVault加密，不复用recovery-dag-v1 checked journal。
// 它不是移动SDK/Save验收；保存原control/packet/id/hash，只有客户端验证后才确认应用。
type dagEnvironmentBusinessRecord struct {
	Endpoint          string
	AccountID         string
	AccountGeneration uint64
	Epoch             uint64
	Capability        string
	Control           syncclient.EnvironmentControlView
	Packet            cryptox.EnvironmentChangeV2
	Hash              string
}

// 真实nativeVault之上的有限Save故障注入；不模拟手机SDK持久化。
type dagEnvironmentSaveFault struct {
	*dagAcceptanceStore
	fail atomic.Bool
}

func (s *dagEnvironmentSaveFault) Save(state localstate.State) error {
	if s.fail.Load() {
		return errors.New("synthetic native state save failure")
	}
	return s.dagAcceptanceStore.Save(state)
}

func TestNativeP4EnvironmentCRUDAndOriginalBusinessReceipt(t *testing.T) {
	f := newMobileManagerFixture(t)
	var identity struct {
		AccountID string `json:"accountId"`
	}
	raw := f.rootActor.load()
	originMust(t, json.Unmarshal(raw, &identity))
	clear(raw)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	b, codeB := recoverDAGActor(t, ctx, f, identity.AccountID, f.code, "dag-env-B", false)
	f.root.Close()
	b.verifier.Close()
	c, _ := recoverDAGActor(t, ctx, f, identity.AccountID, codeB, "dag-env-C", false)
	env := "dag-created-Z"
	created := dagEnvironmentChange(t, ctx, c, f.initial, env, "create", "dag-env-create")
	var postCount atomic.Int64
	lost := atomic.Bool{}
	lost.Store(true)
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/environment-changes-v4") {
			postCount.Add(1)
			if r.StatusCode == 200 && lost.Swap(false) {
				_ = r.Body.Close()
				data := []byte(`{"error":"request_rejected"}`)
				r.StatusCode = 504
				r.Body = io.NopCloser(bytes.NewReader(data))
				r.ContentLength = int64(len(data))
				r.Header.Set("Content-Length", strconv.Itoa(len(data)))
			}
		}
		return nil
	}})
	saved := dagEnvironmentBusinessRecord{Endpoint: f.proxy.URL, AccountID: identity.AccountID, AccountGeneration: 1, Epoch: c.engine.State().SessionEpoch, Capability: cryptox.RecoveryDAGCapability, Control: environmentValue(c.client.EnvironmentControl(ctx, f.initial)), Packet: created, Hash: environmentValue(cryptox.EnvironmentSubmissionHash(created))}
	// writes-v1仅在此测试独占业务阶段使用；原共享变量writer随后接管前删除此测试record。
	originMust(t, c.vault.Save("writes-v1", environmentValue(json.Marshal(saved))))
	if out, e := c.client.SubmitEnvironmentChangeV4(ctx, created); e == nil || out.Applied {
		t.Fatal("lost response claimed applied")
	}
	f.responseHook.Store(nil)
	originMust(t, c.vault.Close())
	c.vault = environmentValue(localkeys.Open(localkeys.Config{Directory: c.vault.Directory(), UserID: environmentValue(localkeys.CurrentUserID())}))
	t.Cleanup(func() { _ = c.vault.Close() })
	journal := environmentValue(c.vault.Load("writes-v1"))
	var restored dagEnvironmentBusinessRecord
	originMust(t, json.Unmarshal(journal, &restored))
	clear(journal)
	if restored.Endpoint != f.proxy.URL || restored.AccountID != identity.AccountID || restored.Epoch != c.engine.State().SessionEpoch || restored.Capability != cryptox.RecoveryDAGCapability || restored.Hash != environmentValue(cryptox.EnvironmentSubmissionHash(restored.Packet)) {
		t.Fatal("protected business identity/packet changed")
	}
	// 新owner+新Engine+Boot仅查原ID，不生成新的环境钥/封套/ID。
	c.engine = environmentValue(localstate.New(&dagAcceptanceStore{vault: c.vault}))
	c.client = environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: identity.AccountID, AccountGeneration: 1, DeviceID: c.id, Verifier: c.verifier, Engine: c.engine}))
	c.client = environmentValue(c.client.BootDevice(ctx, c.key))
	_, e := c.client.VerifyEnvironmentControl(restored.Control, f.initial, true)
	originMust(t, e)
	status := environmentValue(c.client.EnvironmentStatusV4(ctx, restored.Packet.Change.IdempotencyKey))
	if status.ContentHash != restored.Hash {
		t.Fatal("original status hash mismatch")
	}
	applied := environmentValue(c.client.ConfirmEnvironmentChangeV4(ctx, restored.Packet, syncclient.Acceptance{Sequence: status.Sequence, Replayed: true}))
	if !applied.Applied || postCount.Load() != 1 {
		t.Fatal("unknown response resubmitted as new write")
	}
	originMust(t, c.vault.Delete("writes-v1"))
	dagStage(t, "native encrypted business originalid restart/receipt/full pull", nil)
	writer := environmentValue(syncclient.NewWriter(identity.AccountID, 1, c.id, c.engine.State().SessionEpoch, c.key, dagAcceptanceWriteJournal{c.vault}))
	defer writer.Close()
	out := environmentValue(writer.Execute(ctx, c.client, syncclient.WriteRequest{ID: "dag-env-put", Operation: "put", EnvironmentID: env, Name: "SYNTHETIC_P4_ENV", Value: "synthetic-p4-env"}))
	if !out.Applied {
		t.Fatal("writer bypassed pull")
	}
	d := dagEnvironmentEnrollRO(t, ctx, f, c, env)
	if d.engine.State().Cloud.Environments[env].Values["SYNTHETIC_P4_ENV"] != "synthetic-p4-env" {
		t.Fatal("new V5 reader failed real HPKE/data AEAD")
	}
	before := dagEnvironmentTarget(t, d, env).Grant
	_ = environmentValue(c.client.Pull(ctx))
	control := environmentValue(c.client.EnvironmentControl(ctx, env))
	key := dagEnvironmentKey(t, c, env)
	rename := created.Change
	own := dagEnvironmentTarget(t, c, env).Grant
	rename.Operation = "rename"
	rename.AuthorityEnvironmentID = env
	rename.AuthorityKeyVersion = own.KeyVersion
	rename.AuthorityGrantGeneration = own.GrantGeneration
	rename.PreviousKeyVersion = own.KeyVersion
	rename.KeyVersion = own.KeyVersion
	rename.ExpectedSequence = strconv.FormatUint(control.Sequence, 10)
	rename.IdempotencyKey = "dag-env-rename"
	rename.Grants = []cryptox.SignedGrantWire{}
	rename.Mutations = []cryptox.SignedMutationWire{}
	rename.RecoveryEnvelope = ""
	rename.LabelPayload = cryptox.EncodeBase64(environmentValue(cryptox.EncryptEnvironmentLabel(key, cryptox.EnvironmentLabelContext{AccountID: rename.AccountID, AccountGeneration: "1", EnvironmentID: env, KeyVersion: own.KeyVersion}, []byte("合成重命名"))))
	clear(key)
	result := environmentValue(c.client.SubmitEnvironmentChange(ctx, environmentValue(cryptox.SignEnvironmentChange(rename, c.key))))
	if !result.Applied {
		t.Fatal("rename lacked exact checkpoint")
	}
	rotated := dagEnvironmentChange(t, ctx, c, env, env, "rotate", "dag-env-rotate")
	// 已接受但相同下发流的native Save失败，只能报告accepted-not-applied。
	faultStore := &dagEnvironmentSaveFault{dagAcceptanceStore: &dagAcceptanceStore{vault: c.vault}}
	c.engine = environmentValue(localstate.New(faultStore))
	c.client = environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: identity.AccountID, AccountGeneration: 1, DeviceID: c.id, Verifier: c.verifier, Engine: c.engine}))
	c.client = environmentValue(c.client.BootDevice(ctx, c.key))
	oldDataSequence := c.engine.State().Cloud.Sequence
	faultStore.fail.Store(true)
	failedResult, failedErr := c.client.SubmitEnvironmentChangeV4(ctx, rotated)
	if !errors.Is(failedErr, syncclient.ErrAcceptedNotApplied) || failedResult.Applied || failedResult.Accepted.Sequence == 0 || c.engine.State().Cloud.Sequence != oldDataSequence {
		t.Fatal("native save failure claimed application or changed local authority")
	}
	faultStore.fail.Store(false)
	originalRotation := environmentValue(c.client.EnvironmentStatusV4(ctx, rotated.Change.IdempotencyKey))
	if originalRotation.Sequence != failedResult.Accepted.Sequence || originalRotation.ContentHash != environmentValue(cryptox.EnvironmentSubmissionHash(rotated)) {
		t.Fatal("accepted rotation changed original receipt after save failure")
	}
	result = environmentValue(c.client.ConfirmEnvironmentChangeV4(ctx, rotated, failedResult.Accepted))
	dagStage(t, "accepted rotation/native save failure/only original receipt confirmation", nil)
	if !result.Applied || result.Accepted.Sequence != environmentValue(strconv.ParseUint(rotated.Change.ExpectedSequence, 10, 64))+1+uint64(len(rotated.Change.Mutations)) {
		t.Fatal("rotation wrong accepted tail")
	}
	_ = environmentValue(d.client.Pull(ctx))
	after := dagEnvironmentTarget(t, d, env).Grant
	if after.Role != before.Role || after.ExpiresAt != before.ExpiresAt || after.GrantGeneration != "2" || after.KeyVersion != "2" || d.engine.State().Cloud.Environments[env].Values["SYNTHETIC_P4_ENV"] != "synthetic-p4-env" {
		t.Fatal("rotation changed rights or lost true ciphertext")
	}
	dagStage(t, "C create/rename/rotate and true PAKE V5 RO reader", nil)
	originMust(t, d.engine.Activate(env, 10, time.Now()))
	originMust(t, d.engine.SetOverride(env, "SYNTHETIC_P4_ENV", "synthetic-local-override", time.Now()))
	provider := &pauseMemoryProvider{values: map[string]string{"SYNTHETIC_P4_ENV": "synthetic-original", "SYNTHETIC_UNRELATED": "keep"}}
	originMust(t, d.engine.Reconcile(ctx, provider, time.Now()))
	originMust(t, d.engine.SetPaused(true))
	state := d.engine.State().Cloud
	_ = environmentValue(c.client.Pull(ctx))
	own = dagEnvironmentTarget(t, c, env).Grant
	deleted := rename
	deleted.Operation = "delete"
	deleted.AuthorityKeyVersion = own.KeyVersion
	deleted.AuthorityGrantGeneration = own.GrantGeneration
	deleted.PreviousKeyVersion = own.KeyVersion
	deleted.KeyVersion = own.KeyVersion
	deleted.ExpectedSequence = strconv.FormatUint(c.engine.State().Cloud.Sequence, 10)
	deleted.IdempotencyKey = "dag-env-delete"
	deleted.LabelPayload = ""
	result = environmentValue(c.client.SubmitEnvironmentChange(ctx, environmentValue(cryptox.SignEnvironmentChange(deleted, c.key))))
	if !result.Applied {
		t.Fatal("delete lacked exact tombstone")
	}
	_ = environmentValue(d.client.RefreshAuthorizations(ctx))
	originMust(t, d.engine.Reconcile(ctx, provider, time.Now()))
	current := d.engine.State()
	if len(current.Cloud.Environments) != 0 || len(current.Overrides) != 0 || current.Cloud.Sequence != state.Sequence || !bytes.Equal(environmentValue(json.Marshal(current.Cloud.SeenMutations)), environmentValue(json.Marshal(state.SeenMutations))) {
		t.Fatal("paused delete advanced data or kept revoked cache/override")
	}
	values := provider.values
	if values["SYNTHETIC_P4_ENV"] != "synthetic-original" || values["SYNTHETIC_UNRELATED"] != "keep" {
		t.Fatal("delete did not restore per-key original")
	}
	originMust(t, c.verifier.ValidateStoredIssuerEvidence(c.engine.State().Cloud))
	originMust(t, d.verifier.ValidateStoredIssuerEvidence(current.Cloud))
	dagStage(t, "paused signed tombstone auth-only/per-key restore/restart ledger", nil)
	if _, e := d.client.PrepareEnvironmentChangeV4(ctx, cryptox.SignedEnvironmentChange{}, d.key); e == nil {
		t.Fatal("empty environment packet accepted")
	}
}

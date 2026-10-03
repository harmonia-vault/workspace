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
	"os"
	"sort"
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

type recoveryControlActor struct {
	client    *syncclient.Client
	engine    *localstate.Engine
	verifier  *syncclient.PinnedVerifier
	id        string
	key       ed25519.PrivateKey
	receiving []byte
	store     *localkeys.StateStore
}

func recoveryControlTarget(t *testing.T, a *recoveryControlActor, environment string) cryptox.SignedGrantWire {
	t.Helper()
	p := environmentValue(a.client.CurrentIssuerRecoveryEvidence())
	for _, target := range p.Targets {
		if target.EnvironmentID == environment {
			for _, node := range p.Authorities {
				if environmentValue(cryptox.IssuerAuthorityHash(node.Grant)) == target.AuthorityHash {
					return node.Grant
				}
			}
		}
	}
	t.Fatal("verified current recovery control target absent")
	return cryptox.SignedGrantWire{}
}
func recoveryControlKey(t *testing.T, a *recoveryControlActor, environment string) []byte {
	t.Helper()
	g := recoveryControlTarget(t, a, environment).Grant
	return environmentValue(cryptox.UnwrapEnvironmentKey(a.receiving, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: environment, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: a.id, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey}, environmentValue(cryptox.DecodeBase64(g.Envelope, 80, 80))))
}
func recoveryControlChange(t *testing.T, a *recoveryControlActor, environment, operation, id string) cryptox.EnvironmentChangeV2 {
	t.Helper()
	_ = environmentValue(a.client.Pull(context.Background()))
	authorityEnvironment := environment
	if operation == "create" {
		for env := range a.engine.State().Cloud.Environments {
			if a.engine.State().Cloud.Environments[env].Role == localstate.Admin {
				authorityEnvironment = env
				break
			}
		}
	}
	control := environmentValue(a.client.EnvironmentControl(context.Background(), authorityEnvironment))
	if control.IssuerRecoveryEvidence == nil {
		t.Fatal("recovery control not decoded as explicit Proof3")
	}
	actor := recoveryControlTarget(t, a, authorityEnvironment).Grant
	version := "1"
	previous := "0"
	if operation == "rotate" {
		previous = actor.KeyVersion
		version = strconv.FormatUint(environmentValue(strconv.ParseUint(previous, 10, 64))+1, 10)
	}
	key := environmentValue(cryptox.GenerateEnvironmentKey())
	defer clear(key)
	root := control.IssuerRecoveryEvidence.TrustRoot
	label := environmentValue(cryptox.EncryptEnvironmentLabel(key, cryptox.EnvironmentLabelContext{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, EnvironmentID: environment, KeyVersion: version}, []byte("synthetic-recovered-environment")))
	change := cryptox.EnvironmentChange{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, DeviceID: a.id, EnvironmentID: environment, Operation: operation, AuthorityEnvironmentID: authorityEnvironment, AuthorityKeyVersion: actor.KeyVersion, AuthorityGrantGeneration: actor.GrantGeneration, PreviousKeyVersion: previous, KeyVersion: version, ExpectedSequence: strconv.FormatUint(control.Sequence, 10), IdempotencyKey: id, LabelPayload: cryptox.EncodeBase64(label), RecoveryGeneration: root.RecoveryGeneration, Grants: []cryptox.SignedGrantWire{}, Mutations: []cryptox.SignedMutationWire{}}
	change.RecoveryEnvelope = cryptox.EncodeBase64(environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, EnvironmentID: environment, KeyVersion: version, RecipientType: "recovery", RecipientID: actor.AccountID, RecipientGeneration: root.RecoveryGeneration, RecipientPublicKey: root.RecoveryReceivingPublicKey})))
	recipients := control.Grants
	if operation == "create" {
		recipients = []cryptox.SignedGrantWire{{Grant: actor}}
	}
	ownGeneration := ""
	for i, prior := range recipients {
		g := prior.Grant
		g.IssuerDeviceID = a.id
		g.EnvironmentID = environment
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
		g.Envelope = cryptox.EncodeBase64(environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: environment, KeyVersion: version, RecipientType: "device", RecipientID: g.SubjectDeviceID, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey})))
		change.Grants = append(change.Grants, cryptox.GrantToWire(environmentValue(cryptox.SignGrant(g, a.key))))
		if g.SubjectDeviceID == a.id {
			ownGeneration = g.GrantGeneration
		}
	}
	if operation == "rotate" {
		values := a.engine.State().Cloud.Environments[environment].Values
		names := []string{}
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		for i, name := range names {
			payload := environmentValue(cryptox.EncryptValue(key, cryptox.ValueContext{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, EnvironmentID: environment, KeyVersion: version, Name: name}, []byte(values[name])))
			signed := environmentValue(cryptox.SignMutation(cryptox.Mutation{AccountID: actor.AccountID, AccountGeneration: actor.AccountGeneration, DeviceID: a.id, EnvironmentID: environment, KeyVersion: version, GrantGeneration: ownGeneration, Operation: "put", IdempotencyKey: id + "-v-" + strconv.Itoa(i), Name: name, Payload: cryptox.EncodeBase64(payload)}, a.key))
			change.Mutations = append(change.Mutations, cryptox.MutationToWire(signed))
		}
	}
	signed := environmentValue(cryptox.SignEnvironmentChange(change, a.key))
	return environmentValue(a.client.PrepareEnvironmentChangeV3(context.Background(), signed, a.key))
}
func recoveryControlEnroll(t *testing.T, f *mobileManagerFixture, manager *recoveryControlActor, environment string) *recoveryControlActor {
	t.Helper()
	ctx := context.Background()
	account := manager.engine.State().Cloud.AccountID
	_ = environmentValue(manager.client.Pull(ctx))
	keys := environmentValue(localkeys.GenerateDeviceKeys("recovery-control-F"))
	key := ed25519.NewKeyFromSeed(keys.SigningSeed)
	store, _ := (&originFixture{t: t}).newStore(keys.DeviceID)
	t.Cleanup(func() { _ = store.Close(); clear(key); clear(keys.SigningSeed); clear(keys.ReceivingPrivate) })
	originMust(t, store.Vault().SaveDeviceKeys(keys))
	sum := sha256.Sum256([]byte(f.password))
	login := environmentValue(syncclient.Login(ctx, syncclient.LoginConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), Email: f.email, Credential: hex.EncodeToString(sum[:])}))
	originMust(t, store.Vault().SaveSession(localkeys.LoginSession{Endpoint: f.proxy.URL, AccountID: account, AccountGeneration: 1, Token: login.Token, ExpiresAt: time.Unix(login.ExpiresAt, 0).Format(time.RFC3339Nano)}))
	engine := environmentValue(localstate.New(store))
	enrollment := environmentValue(syncclient.NewEnrollmentV4(syncclient.EnrollmentConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, DeviceID: keys.DeviceID, LoginToken: login.Token, SigningKey: key, ReceivingPrivateKey: keys.ReceivingPrivate, Engine: engine}))
	defer enrollment.Close()
	code := environmentValue(pairing.GenerateShortCode())
	defer clear(code)
	_ = environmentValue(enrollment.Begin(ctx, manager.id, "recovery-control-pair-F", code))
	approver := environmentValue(manager.client.NewApproverV4("recovery-control-pair-F", manager.key))
	defer approver.Close()
	type confirmedResult struct {
		anchor cryptox.ConfirmedEnrollmentAnchor
		err    error
	}
	confirmed := make(chan confirmedResult, 1)
	go func() { anchor, err := approver.Confirm(ctx, code); confirmed <- confirmedResult{anchor, err} }()
	for {
		status := environmentValue(enrollment.Advance(ctx))
		if status.Confirmations["approver"] != "" && status.Confirmations["initiator"] != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	result := <-confirmed
	originMust(t, result.err)
	g := recoveryControlTarget(t, manager, environment).Grant
	envKey := recoveryControlKey(t, manager, environment)
	defer clear(envKey)
	g.IssuerDeviceID = manager.id
	g.SubjectDeviceID = keys.DeviceID
	g.SubjectSigningPublicKey = cryptox.EncodeBase64(keys.SigningPublic)
	g.SubjectReceivingPublicKey = cryptox.EncodeBase64(keys.ReceivingPublic)
	g.GrantGeneration = "1"
	g.Role = "rw"
	g.IdempotencyKey = "recovery-control-grant-F"
	g.Envelope = cryptox.EncodeBase64(environmentValue(cryptox.WrapEnvironmentKey(envKey, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: environment, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: keys.DeviceID, RecipientGeneration: "1", RecipientPublicKey: g.SubjectReceivingPublicKey})))
	approval := environmentValue(approver.PrepareApproval([]cryptox.SignedGrantWire{cryptox.GrantToWire(environmentValue(cryptox.SignGrant(g, manager.key)))}))
	save, load := recoveryNativeSeal(t, "synthetic-control-approved-original")
	originMust(t, save(environmentValue(json.Marshal(approval))))
	var saved cryptox.EnrollmentApprovalV4
	data := load()
	originMust(t, json.Unmarshal(data, &saved))
	clear(data)
	_ = environmentValue(approver.Submit(ctx, saved))
	_ = environmentValue(enrollment.Advance(ctx))
	receipt := environmentValue(enrollment.Receipt())
	trust := localkeys.TrustContext{Endpoint: f.proxy.URL, AccountID: account, AccountGeneration: 1, DeviceID: keys.DeviceID, SigningPublic: keys.SigningPublic, ReceivingPublic: keys.ReceivingPublic, CertificateVersion: "4", PairingProfile: pairing.Profile, EnrollmentCertificate: environmentValue(json.Marshal(receipt)), EnrollmentKey: receipt.IdempotencyKey, Accepted: false}
	originMust(t, store.Vault().SaveTrustContext(trust))
	completed := environmentValue(enrollment.Complete(ctx))
	t.Cleanup(completed.Verifier.Close)
	trust.Accepted = true
	originMust(t, store.Vault().SaveTrustContext(trust))
	client := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, DeviceID: keys.DeviceID, Verifier: completed.Verifier, Engine: engine}))
	client = environmentValue(client.BootDevice(ctx, key))
	_ = environmentValue(client.Pull(ctx))
	return &recoveryControlActor{client: client, engine: engine, verifier: completed.Verifier, id: keys.DeviceID, key: key, receiving: keys.ReceivingPrivate, store: store}
}

type recoveryControlJournal struct {
	save   func([]byte) error
	load   func() []byte
	exists bool
}

func (j *recoveryControlJournal) Load() ([]byte, error) {
	if !j.exists {
		return nil, os.ErrNotExist
	}
	return j.load(), nil
}
func (j *recoveryControlJournal) Save(v []byte) error {
	if e := j.save(v); e != nil {
		return e
	}
	j.exists = true
	return nil
}
func recoveryControlPut(t *testing.T, a *recoveryControlActor, environment, name, value, id string) {
	t.Helper()
	save, load := recoveryNativeSeal(t, "synthetic-control-write-journal")
	writer := environmentValue(syncclient.NewWriter(a.engine.State().Cloud.AccountID, 1, a.id, a.engine.State().SessionEpoch, a.key, &recoveryControlJournal{save: save, load: load}))
	defer writer.Close()
	out := environmentValue(writer.Execute(context.Background(), a.client, syncclient.WriteRequest{ID: id, Operation: "put", EnvironmentID: environment, Name: name, Value: value}))
	if !out.Applied || a.engine.State().Cloud.Environments[environment].Values[name] != value {
		t.Fatal("recovery writer accepted without verified pull")
	}
}

// 实际注册/初始化→高层连续恢复E→真实原生PAKE F；无fixture信任目录替代入网。
func TestNativeRecoveredEnvironmentControlRotationAndGrantManagement(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("实际SPAKE2需原生BoringSSL tag")
	}
	ctx := context.Background()
	f := newMobileManagerFixture(t)
	e := newMobileManagerActor(t, f)
	originMust(t, e.workflow.Login(ctx, f.email, f.password))
	old, _, err := e.workflow.BeginRecoveryAuthoritySession(ctx, f.code)
	originMust(t, err)
	defer old.Close()
	code := environmentValue(e.workflow.BeginRecoveryAuthorityTransition(ctx, "recovery-control-transition"))
	fresh, _, err := e.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
	code = ""
	originMust(t, err)
	defer fresh.Close()
	_ = environmentValue(e.workflow.RegisterRecoveredDevice(ctx, "recovery-control-register-E", []mobileworkflow.RecoveryDeviceSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: "0"}}))
	_, verifier, id := recoveredCLIActor(t, f, e)
	var state struct {
		Cloud localstate.State `json:"cloud"`
	}
	data := e.load()
	originMust(t, json.Unmarshal(data, &state))
	clear(data)
	account := state.Cloud.Cloud.AccountID
	engine := environmentValue(localstate.New(&originMemoryStore{state: state.Cloud}))
	client := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, DeviceID: id, Verifier: verifier, Engine: engine}))
	client = environmentValue(client.BootDevice(ctx, e.config.SigningKey))
	_ = environmentValue(client.Pull(ctx))
	a := &recoveryControlActor{client: client, engine: engine, verifier: verifier, id: id, key: e.config.SigningKey, receiving: e.config.ReceivingPrivateKey}
	const environment = "recovery-control-Z"
	create := recoveryControlChange(t, a, environment, "create", "recovery-control-create-Z")
	seal, load := recoveryNativeSeal(t, "synthetic-original-control-packet")
	originMust(t, seal(environmentValue(json.Marshal(create))))
	var lost atomic.Bool
	lost.Store(true)
	var posts atomic.Int64
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/environment-changes-v3") && r.StatusCode == 200 {
			posts.Add(1)
			if lost.Swap(false) {
				_ = r.Body.Close()
				b := []byte(`{"error":"request_rejected"}`)
				r.StatusCode = 504
				r.Body = io.NopCloser(bytes.NewReader(b))
				r.ContentLength = int64(len(b))
				r.Header.Set("Content-Length", strconv.Itoa(len(b)))
			}
		}
		return nil
	}})
	if _, err = client.SubmitEnvironmentChangeV3(ctx, create); err == nil {
		t.Fatal("lost environment response reported success")
	}
	f.responseHook.Store(nil)
	var original cryptox.EnvironmentChangeV2
	data = load()
	originMust(t, json.Unmarshal(data, &original))
	clear(data)
	status := environmentValue(client.EnvironmentStatusV3(ctx, original.Change.IdempotencyKey))
	if status.State != "complete" || status.ContentHash != environmentValue(cryptox.EnvironmentSubmissionHash(original)) {
		t.Fatal("lost response not bound to exact original environment packet")
	}
	confirmed := environmentValue(client.ConfirmEnvironmentChangeV3(ctx, original, syncclient.Acceptance{Sequence: status.Sequence, Replayed: true}))
	if !confirmed.Applied || posts.Load() != 1 {
		t.Fatal("environment create replay generated new write")
	}
	recoveryControlPut(t, a, environment, "SYNTHETIC_CONTROL_VALUE", "synthetic-before-rotation", "recovery-control-E-put")
	child := recoveryControlEnroll(t, f, a, environment)
	if child.engine.State().Cloud.Environments[environment].Values["SYNTHETIC_CONTROL_VALUE"] != "synthetic-before-rotation" {
		t.Fatal("real paired F failed HPKE decrypt")
	}
	recoveryControlPut(t, child, environment, "SYNTHETIC_CONTROL_VALUE", "synthetic-written-F", "recovery-control-F-put")
	rotate := recoveryControlChange(t, a, environment, "rotate", "recovery-control-rotate-Z")
	result := environmentValue(client.SubmitEnvironmentChangeV3(ctx, rotate))
	if !result.Applied {
		t.Fatal("rotation not applied")
	}
	_ = environmentValue(child.client.Pull(ctx))
	if child.engine.State().Cloud.Environments[environment].KeyVersion != 2 || child.engine.State().Cloud.Environments[environment].Values["SYNTHETIC_CONTROL_VALUE"] != "synthetic-written-F" {
		t.Fatal("rotation omitted live value or exact recipient")
	}
	recoveryControlPut(t, child, environment, "SYNTHETIC_CONTROL_VALUE", "synthetic-after-rotation", "recovery-control-F-put-KV2")
	_ = environmentValue(client.Pull(ctx))
	for i, role := range []string{"ro", "none"} {
		tx, prepareErr := client.PrepareGrantUpdate(ctx, syncclient.GrantUpdateIntent{ID: "recovery-control-management-" + strconv.Itoa(i), EnvironmentID: environment, SubjectDeviceID: child.id, Role: role}, a.key)
		originMust(t, prepareErr)
		protected := environmentValue(tx.ProtectedBytes())
		originMust(t, seal(protected))
		clear(protected)
		tx = environmentValue(client.RestoreGrantUpdate(load()))
		acceptance := environmentValue(tx.SubmitWithBarrier(ctx, func() error { return seal(environmentValue(tx.ProtectedBytes())) }))
		if !environmentValue(tx.Confirm(ctx, acceptance)).Applied {
			t.Fatal("explicit grant update accepted without applied pull")
		}
		_ = environmentValue(child.client.Pull(ctx))
		if role == "ro" && child.engine.State().Cloud.Environments[environment].Role != localstate.ReadOnly {
			t.Fatal("paired device failed explicit role downgrade")
		}
		if role == "none" && len(child.engine.State().Cloud.Environments) != 0 {
			t.Fatal("grant none failed cache removal")
		}
		if role == "ro" {
			save, load := recoveryNativeSeal(t, "synthetic-readonly-control-journal")
			writer := environmentValue(syncclient.NewWriter(account, 1, child.id, child.engine.State().SessionEpoch, child.key, &recoveryControlJournal{save: save, load: load}))
			_, writeErr := writer.Execute(ctx, child.client, syncclient.WriteRequest{ID: "recovery-control-denied-RO", Operation: "put", EnvironmentID: environment, Name: "SYNTHETIC_DENIED", Value: "synthetic-never-uploaded"})
			writer.Close()
			if !errors.Is(writeErr, syncclient.ErrWritePermission) {
				t.Fatal("explicit role downgrade did not prevent subsequent writing")
			}
		}
	}
	revocation := environmentValue(client.PrepareOtherRevocation(ctx, "recovery-control-global-revoke-F", child.id, environment, a.key))
	protected := environmentValue(revocation.ProtectedBytes())
	originMust(t, seal(protected))
	clear(protected)
	revocation = environmentValue(client.RestoreOtherRevocation(load()))
	accepted := environmentValue(revocation.SubmitWithBarrier(ctx, func() error { return seal(environmentValue(revocation.ProtectedBytes())) }))
	if !environmentValue(revocation.ConfirmThrough(ctx, client, accepted)).Applied {
		t.Fatal("other device revoke accepted without owner pull")
	}
	boot := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, DeviceID: child.id, Verifier: child.verifier, Engine: child.engine}))
	if _, err = boot.BootDevice(ctx, child.key); !errors.Is(err, syncclient.ErrTrustInvalidated) || !child.engine.State().AccountClosed {
		t.Fatal("globally revoked device remained trusted at boot")
	}
	t.Log("真实恢复E创建Z/丢响应原包确认→原生PAKE F读写→Z轮换全部值/接收者→F新KV读写→RO拒写/none清缓存→全局撤设备持钥启动拒绝通过")
}

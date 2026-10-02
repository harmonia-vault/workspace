package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localipc"
	"github.com/harmonia-vault/core-go/localkeys"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/pairing"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 仅在合成 Go 测试中承接已完成初始化的原生 AES 上下文；不模拟手机认证。
type originMemoryStore struct{ state localstate.State }

func (s *originMemoryStore) Load() (localstate.State, error) { return originClone(s.state), nil }
func (s *originMemoryStore) Save(v localstate.State) error   { s.state = originClone(v); return nil }
func originClone[T any](v T) T {
	b := environmentValue(json.Marshal(v))
	var out T
	if json.Unmarshal(b, &out) != nil {
		panic("synthetic JSON clone failed")
	}
	return out
}
func originMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

type originActor struct {
	keys      localkeys.DeviceKeys
	key       ed25519.PrivateKey
	engine    *localstate.Engine
	verifier  *syncclient.PinnedVerifier
	client    *syncclient.Client
	session   syncclient.DeviceSession
	store     *localkeys.StateStore
	directory string
	receipt   syncclient.EnrollmentReceiptV3
}
type originFixture struct {
	boots                                atomic.Int64
	t                                    *testing.T
	proxy                                *httptest.Server
	account, generation, email, password string
	root                                 cryptox.TrustRoot
	initial                              []cryptox.SignedGrantWire
	loseEnrollment, loseEnvironment      atomic.Bool
	enrollmentPosts, environmentPosts    atomic.Int64
}

func (f *originFixture) base() string { return "/v1/accounts/" + f.account }
func (f *originFixture) raw(actor *originActor, path string, body, out any) int {
	f.t.Helper()
	method := http.MethodGet
	var reader io.Reader
	if body != nil {
		method = http.MethodPost
		reader = bytes.NewReader(environmentValue(json.Marshal(body)))
	}
	req := environmentValue(http.NewRequestWithContext(context.Background(), method, f.proxy.URL+f.base()+path, reader))
	req.Header.Set("Authorization", "Bearer "+actor.session.Token)
	req.Header.Set("X-Harmonia-Device-Id", actor.keys.DeviceID)
	req.Header.Set("X-Harmonia-Account-Generation", f.generation)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, e := f.proxy.Client().Do(req)
	originMust(f.t, e)
	defer response.Body.Close()
	if out != nil {
		originMust(f.t, json.NewDecoder(io.LimitReader(response.Body, cryptox.MaxIssuerProofV2Bytes+4096)).Decode(out))
	}
	return response.StatusCode
}
func (f *originFixture) boot(a *originActor) {
	t := f.t
	t.Helper()
	var challenge syncclient.DeviceChallenge
	if got := callJSON(t, f.proxy.Client(), f.proxy.URL, f.base()+"/boot-challenges", "", "", "", map[string]string{"deviceId": a.keys.DeviceID, "accountGeneration": f.generation}, &challenge); got != 200 {
		t.Fatal("boot challenge status", got)
	}
	p := environmentValue(cryptox.NewDeviceBootProof(f.account, f.generation, a.keys.DeviceID, a.keys.SigningPublic, a.keys.ReceivingPublic, challenge.ChallengeID, challenge.Nonce, strconv.FormatInt(challenge.ExpiresAt, 10)))
	var fields []string
	originMust(t, json.Unmarshal(environmentValue(p.SigningBytes()), &fields))
	if !reflect.DeepEqual(fields, challenge.SigningPayload) || challenge.ExpiresAt <= time.Now().Unix() {
		t.Fatal("boot payload was not locally bound")
	}
	if got := callJSON(t, f.proxy.Client(), f.proxy.URL, f.base()+"/boot-sessions", "", "", "", map[string]string{"deviceId": a.keys.DeviceID, "accountGeneration": f.generation, "challengeId": challenge.ChallengeID, "signature": environmentValue(cryptox.SignDeviceBootProof(p, a.key))}, &a.session); got != 200 {
		t.Fatal("boot session status", got)
	}
	if a.store != nil {
		originMust(t, a.store.Vault().SaveSession(localkeys.LoginSession{Endpoint: f.proxy.URL, AccountID: f.account, AccountGeneration: 1, Token: a.session.Token, ExpiresAt: time.Unix(a.session.ExpiresAt, 0).Format(time.RFC3339Nano)}))
	}
	a.client = environmentValue(syncclient.New(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: f.account, AccountGeneration: 1, DeviceID: a.keys.DeviceID, Token: a.session.Token, Engine: a.engine, Verifier: a.verifier}))
}
func (f *originFixture) pull(a *originActor) syncclient.Pull {
	f.t.Helper()
	p, e := a.client.Pull(context.Background())
	originMust(f.t, e)
	return p
}
func (f *originFixture) grant(a *originActor, env string) cryptox.SignedGrantWire {
	p := environmentValue(a.client.CurrentIssuerEvidence())
	for _, target := range p.Targets {
		if target.EnvironmentID == env {
			for _, node := range p.Authorities {
				h := environmentValue(cryptox.IssuerAuthorityHash(node.Grant))
				if h == target.AuthorityHash {
					return node.Grant
				}
			}
		}
	}
	f.t.Fatal("verified current grant missing")
	return cryptox.SignedGrantWire{}
}
func (f *originFixture) unwrap(a *originActor, env string) []byte {
	g := f.grant(a, env).Grant
	packet := environmentValue(cryptox.DecodeBase64(g.Envelope, 80, 80))
	return environmentValue(cryptox.UnwrapEnvironmentKey(a.keys.ReceivingPrivate, cryptox.EnvelopeContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: env, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: a.keys.DeviceID, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey}, packet))
}
func (f *originFixture) put(a *originActor, env, name, value, id string) cryptox.SignedMutation {
	g := f.grant(a, env).Grant
	k := f.unwrap(a, env)
	defer clear(k)
	packet := environmentValue(cryptox.EncryptValue(k, cryptox.ValueContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: env, KeyVersion: g.KeyVersion, Name: name}, []byte(value)))
	m := environmentValue(cryptox.SignMutation(cryptox.Mutation{AccountID: f.account, AccountGeneration: f.generation, DeviceID: a.keys.DeviceID, EnvironmentID: env, KeyVersion: g.KeyVersion, GrantGeneration: g.GrantGeneration, Operation: "put", IdempotencyKey: id, Name: name, Payload: cryptox.EncodeBase64(packet)}, a.key))
	result, e := a.client.Submit(context.Background(), syncclient.SignedMutation{Mutation: m.Mutation, Signature: m.Signature})
	originMust(f.t, e)
	if !result.Applied {
		f.t.Fatal("accepted mutation bypassed verified pull")
	}
	return m
}
func (f *originFixture) newStore(id string) (*localkeys.StateStore, string) {
	t := f.t
	t.Helper()
	base := environmentValue(os.MkdirTemp("/tmp", "harmonia-origin-"))
	base = environmentValue(filepath.EvalSymlinks(base))
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	directory := filepath.Join(base, id)
	uid := environmentValue(localkeys.CurrentUserID())
	store := environmentValue(localkeys.OpenEncryptedStateStore(localkeys.Config{Directory: directory, UserID: uid}))
	return store, directory
}

// 两端都用固定 BoringSSL 的真实 SPAKE2；证书签名前核验双向确认和精确管理钥。
func (f *originFixture) enroll(manager *originActor, id, env, role, expiry string, lose bool) *originActor {
	t := f.t
	t.Helper()
	ctx := context.Background()
	f.pull(manager)
	keys := environmentValue(localkeys.GenerateDeviceKeys(id))
	key := ed25519.NewKeyFromSeed(keys.SigningSeed)
	store, directory := f.newStore(id)
	a := &originActor{keys: keys, key: key, store: store, directory: directory}
	t.Cleanup(func() {
		if a.store != nil {
			_ = a.store.Close()
		}
		if a.verifier != nil {
			a.verifier.Close()
		}
		clear(a.key)
		clear(a.keys.SigningSeed)
		clear(a.keys.ReceivingPrivate)
	})
	originMust(t, store.Vault().SaveDeviceKeys(keys))
	sum := sha256.Sum256([]byte(f.password))
	login := environmentValue(syncclient.Login(ctx, syncclient.LoginConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), Email: f.email, Credential: hex.EncodeToString(sum[:])}))
	originMust(t, store.Vault().SaveSession(localkeys.LoginSession{Endpoint: f.proxy.URL, AccountID: f.account, AccountGeneration: 1, Token: login.Token, ExpiresAt: time.Unix(login.ExpiresAt, 0).Format(time.RFC3339Nano)}))
	a.engine = environmentValue(localstate.New(store))
	config := syncclient.EnrollmentConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: f.account, AccountGeneration: 1, DeviceID: id, LoginToken: login.Token, SigningKey: key, ReceivingPrivateKey: keys.ReceivingPrivate, Engine: a.engine}
	enrollment := environmentValue(syncclient.NewEnrollmentV3(config))
	defer enrollment.Close()
	short := environmentValue(pairing.GenerateShortCode())
	defer clear(short)
	status := environmentValue(enrollment.Begin(ctx, manager.keys.DeviceID, "origin-enroll-"+id, short))
	if status.Context.ApproverSigningPublicKey != cryptox.EncodeBase64(manager.keys.SigningPublic) || status.Context.ApproverReceivingPublicKey != cryptox.EncodeBase64(manager.keys.ReceivingPublic) {
		t.Fatal("PAKE approver differs from local manager")
	}
	pake, message, e := pairing.NewApprover(status.Context, short)
	originMust(t, e)
	defer pake.Close()
	relay := func(kind string, payload []byte) syncclient.PairingStatusV3 {
		t.Helper()
		c := status.Context
		p := cryptox.PairingRelay{AccountID: f.account, AccountGeneration: f.generation, SessionID: c.SessionID, ChallengeNonce: c.ChallengeNonce, Side: "approver", Kind: kind, Payload: cryptox.EncodeBase64(payload)}
		var out syncclient.PairingStatusV3
		if got := f.raw(manager, "/pairings-v3/origin-enroll-"+id+"/relay", cryptox.PairingRelayRequest{Side: p.Side, Kind: kind, Payload: p.Payload, Signature: environmentValue(cryptox.SignPairingRelay(p, manager.key))}, &out); got != 200 {
			t.Fatal("PAKE relay status", got)
		}
		return out
	}
	status = relay("message", message)
	peer := environmentValue(cryptox.DecodeBase64(status.Messages["initiator"], 32, 32))
	confirmation := environmentValue(pake.Complete(peer))
	status = relay("confirmation", confirmation)
	status = environmentValue(enrollment.Advance(ctx))
	originMust(t, pake.VerifyPeerConfirmation(environmentValue(cryptox.DecodeBase64(status.Confirmations["initiator"], 32, 32))))
	transcript := environmentValue(pake.TranscriptHash())
	parent := f.grant(manager, env).Grant
	k := f.unwrap(manager, env)
	defer clear(k)
	g := parent
	g.IssuerDeviceID = manager.keys.DeviceID
	g.SubjectDeviceID = id
	g.SubjectSigningPublicKey = cryptox.EncodeBase64(keys.SigningPublic)
	g.SubjectReceivingPublicKey = cryptox.EncodeBase64(keys.ReceivingPublic)
	g.GrantGeneration = "1"
	g.Role = role
	g.ExpiresAt = expiry
	g.IdempotencyKey = "origin-grant-" + id
	g.Envelope = cryptox.EncodeBase64(environmentValue(cryptox.WrapEnvironmentKey(k, cryptox.EnvelopeContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: env, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: id, RecipientGeneration: "1", RecipientPublicKey: g.SubjectReceivingPublicKey})))
	proof, pin, initial, e := manager.client.PrepareEnrollmentProofV3([]string{env})
	originMust(t, e)
	approval := environmentValue(cryptox.SignEnrollmentApprovalV3(cryptox.EnrollmentApprovalV3{CertificateVersion: "3", Context: status.Context.EnrollmentContext(), PairingProfile: pairing.Profile, TranscriptHash: transcript, Grants: []cryptox.SignedGrantWire{cryptox.GrantToWire(environmentValue(cryptox.SignGrant(g, manager.key)))}, IssuerProof: proof}, pin, cryptox.ConfirmedEnrollmentAnchor{Context: status.Context.EnrollmentContext(), TranscriptHash: transcript}, manager.key, time.Now(), initial...))
	var approvalResponse struct {
		syncclient.PairingStatusV3
		Error string `json:"error"`
	}
	if got := f.raw(manager, "/pairings-v3/origin-enroll-"+id+"/approve", map[string]any{"certificateVersion": "3", "capabilities": []string{cryptox.EnvironmentOriginCapability}, "grants": approval.Grants, "transcriptHash": transcript, "issuerProof": approval.IssuerProof, "signature": approval.ApproverSignature}, &approvalResponse); got != 200 {
		safe := map[string]bool{"fields_invalid": true, "issuer_origin_capability_required": true, "issuer_evidence_invalid": true, "issuer_origin_invalid": true, "issuer_proof_invalid": true, "signature_invalid": true, "json_invalid": true, "array_invalid": true}
		classification := "unclassified"
		if safe[approvalResponse.Error] {
			classification = approvalResponse.Error
		}
		t.Fatal("cert3 approval status", got, classification)
	}
	status = approvalResponse.PairingStatusV3
	_, e = enrollment.Advance(ctx)
	originMust(t, e)
	a.receipt = environmentValue(enrollment.Receipt())
	receiptBytes := environmentValue(json.Marshal(a.receipt))
	trust := localkeys.TrustContext{Endpoint: f.proxy.URL, AccountID: f.account, AccountGeneration: 1, DeviceID: id, SigningPublic: keys.SigningPublic, ReceivingPublic: keys.ReceivingPublic, CertificateVersion: "3", PairingProfile: pairing.Profile, EnrollmentCertificate: receiptBytes, EnrollmentKey: a.receipt.IdempotencyKey, Accepted: false}
	originMust(t, store.Vault().SaveTrustContext(trust))
	sealed := environmentValue(os.ReadFile(filepath.Join(directory, "trust.v1.enc")))
	if bytes.Contains(sealed, receiptBytes) || bytes.Contains(sealed, short) || bytes.Contains(sealed, []byte(login.Token)) {
		t.Fatal("receipt or ephemeral enrollment secret stored in plaintext")
	}
	if a.engine.State().Cloud.AccountID != "" {
		t.Fatal("approval updated authoritative cache before pull")
	}
	var result syncclient.EnrollmentResultV3
	if lose {
		before := f.enrollmentPosts.Load()
		f.loseEnrollment.Store(true)
		if _, e = enrollment.Complete(ctx); !errors.Is(e, syncclient.ErrEnrollmentPending) {
			t.Fatal("accepted enrollment response loss was not pending", e)
		}
		enrollment.Close()
		originMust(t, store.Close())
		a.store = nil
		a.store = environmentValue(localkeys.OpenEncryptedStateStore(localkeys.Config{Directory: directory, UserID: environmentValue(localkeys.CurrentUserID())}))
		a.engine = environmentValue(localstate.New(a.store))
		saved := environmentValue(a.store.Vault().LoadTrustContext())
		if saved.Accepted {
			t.Fatal("uncertain acknowledgment prematurely accepted")
		}
		r := environmentValue(syncclient.DecodeEnrollmentReceiptV3(saved.EnrollmentCertificate))
		if !reflect.DeepEqual(r, a.receipt) {
			t.Fatal("sealed original enrollment receipt changed")
		}
		config.Engine = a.engine
		resumed := environmentValue(syncclient.ResumeEnrollmentV3(config, r))
		result = environmentValue(resumed.Complete(ctx))
		resumed.Close()
		if f.enrollmentPosts.Load() != before+1 {
			t.Fatal("lost acknowledgment repeated accepted enrollment write")
		}
	} else {
		result = environmentValue(enrollment.Complete(ctx))
	}
	trust.Accepted = true
	originMust(t, a.store.Vault().SaveTrustContext(trust))
	a.verifier = result.Verifier
	f.boot(a)
	f.pull(a)
	if a.engine.State().Cloud.Sequence < result.Sequence {
		t.Fatal("completed device did not pull durable enrollment sequence")
	}
	return a
}

func (f *originFixture) create(manager *originActor, authorityEnv, newEnv, label, id string, lose bool) cryptox.EnvironmentChangeV2 {
	t := f.t
	t.Helper()
	ctx := context.Background()
	f.pull(manager)
	control := environmentValue(manager.client.EnvironmentControl(ctx, authorityEnv))
	parent := f.grant(manager, authorityEnv).Grant
	key := environmentValue(cryptox.GenerateEnvironmentKey())
	defer clear(key)
	g := parent
	g.EnvironmentID = newEnv
	g.KeyVersion = "1"
	g.GrantGeneration = "1"
	g.IssuerDeviceID = manager.keys.DeviceID
	g.SubjectDeviceID = manager.keys.DeviceID
	g.IdempotencyKey = id + "-grant"
	g.Envelope = cryptox.EncodeBase64(environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: newEnv, KeyVersion: "1", RecipientType: "device", RecipientID: manager.keys.DeviceID, RecipientGeneration: "1", RecipientPublicKey: g.SubjectReceivingPublicKey})))
	recovery := environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: newEnv, KeyVersion: "1", RecipientType: "recovery", RecipientID: f.account, RecipientGeneration: f.root.RecoveryGeneration, RecipientPublicKey: f.root.RecoveryReceivingPublicKey}))
	encryptedLabel := environmentValue(cryptox.EncryptEnvironmentLabel(key, cryptox.EnvironmentLabelContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: newEnv, KeyVersion: "1"}, []byte(label)))
	change := cryptox.EnvironmentChange{AccountID: f.account, AccountGeneration: f.generation, DeviceID: manager.keys.DeviceID, EnvironmentID: newEnv, Operation: "create", AuthorityEnvironmentID: authorityEnv, AuthorityKeyVersion: parent.KeyVersion, AuthorityGrantGeneration: parent.GrantGeneration, PreviousKeyVersion: "0", KeyVersion: "1", ExpectedSequence: strconv.FormatUint(control.Sequence, 10), IdempotencyKey: id, LabelPayload: cryptox.EncodeBase64(encryptedLabel), RecoveryGeneration: f.root.RecoveryGeneration, RecoveryEnvelope: cryptox.EncodeBase64(recovery), Grants: []cryptox.SignedGrantWire{cryptox.GrantToWire(environmentValue(cryptox.SignGrant(g, manager.key)))}}
	packet := environmentValue(manager.client.PrepareEnvironmentChangeV2(ctx, environmentValue(cryptox.SignEnvironmentChange(change, manager.key)), manager.key))
	if lose {
		before := f.environmentPosts.Load()
		old := manager.engine.State().Cloud.Sequence
		f.loseEnvironment.Store(true)
		if _, e := manager.client.SubmitEnvironmentChangeV2(ctx, packet); e == nil {
			t.Fatal("lost accepted environment response reported success")
		}
		if manager.engine.State().Cloud.Sequence != old {
			t.Fatal("unknown environment submit optimistically changed local cache")
		}
		status := environmentValue(manager.client.EnvironmentStatusV2(ctx, id))
		hash := environmentValue(cryptox.EnvironmentSubmissionHash(packet))
		if status.State != "complete" || status.ContentHash != hash {
			t.Fatal("accepted operation status not bound to original packet")
		}
		result := environmentValue(manager.client.ConfirmEnvironmentChangeV2(ctx, packet, syncclient.Acceptance{Sequence: status.Sequence, Replayed: true}))
		if !result.Applied || f.environmentPosts.Load() != before+1 {
			t.Fatal("status retry made a new environment write")
		}
	} else {
		if !environmentValue(manager.client.SubmitEnvironmentChangeV2(ctx, packet)).Applied {
			t.Fatal("new environment did not follow verified pull")
		}
	}
	return packet
}
func (f *originFixture) rotate(manager *originActor, env, label, id string) cryptox.EnvironmentChangeV2 {
	t := f.t
	t.Helper()
	ctx := context.Background()
	f.pull(manager)
	control := environmentValue(manager.client.EnvironmentControl(ctx, env))
	parent := f.grant(manager, env).Grant
	key := environmentValue(cryptox.GenerateEnvironmentKey())
	defer clear(key)
	nextKV := strconv.FormatUint(environmentValue(strconv.ParseUint(parent.KeyVersion, 10, 64))+1, 10)
	grants := []cryptox.SignedGrantWire{}
	for _, old := range control.Grants {
		g := old.Grant
		g.IssuerDeviceID = manager.keys.DeviceID
		g.KeyVersion = nextKV
		g.GrantGeneration = strconv.FormatUint(environmentValue(strconv.ParseUint(g.GrantGeneration, 10, 64))+1, 10)
		g.IdempotencyKey = id + "-" + g.SubjectDeviceID
		g.Envelope = cryptox.EncodeBase64(environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: env, KeyVersion: nextKV, RecipientType: "device", RecipientID: g.SubjectDeviceID, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey})))
		grants = append(grants, cryptox.GrantToWire(environmentValue(cryptox.SignGrant(g, manager.key))))
	}
	recovery := environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: env, KeyVersion: nextKV, RecipientType: "recovery", RecipientID: f.account, RecipientGeneration: f.root.RecoveryGeneration, RecipientPublicKey: f.root.RecoveryReceivingPublicKey}))
	encryptedLabel := environmentValue(cryptox.EncryptEnvironmentLabel(key, cryptox.EnvironmentLabelContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: env, KeyVersion: nextKV}, []byte(label)))
	change := cryptox.EnvironmentChange{AccountID: f.account, AccountGeneration: f.generation, DeviceID: manager.keys.DeviceID, EnvironmentID: env, Operation: "rotate", AuthorityEnvironmentID: env, AuthorityKeyVersion: parent.KeyVersion, AuthorityGrantGeneration: parent.GrantGeneration, PreviousKeyVersion: parent.KeyVersion, KeyVersion: nextKV, ExpectedSequence: strconv.FormatUint(control.Sequence, 10), IdempotencyKey: id, LabelPayload: cryptox.EncodeBase64(encryptedLabel), RecoveryGeneration: f.root.RecoveryGeneration, RecoveryEnvelope: cryptox.EncodeBase64(recovery), Grants: grants}
	nextGG := ""
	for _, g := range grants {
		if g.Grant.SubjectDeviceID == manager.keys.DeviceID {
			nextGG = g.Grant.GrantGeneration
		}
	}
	for name, value := range manager.engine.State().Cloud.Environments[env].Values {
		cipher := environmentValue(cryptox.EncryptValue(key, cryptox.ValueContext{AccountID: f.account, AccountGeneration: f.generation, EnvironmentID: env, KeyVersion: nextKV, Name: name}, []byte(value)))
		m := environmentValue(cryptox.SignMutation(cryptox.Mutation{AccountID: f.account, AccountGeneration: f.generation, DeviceID: manager.keys.DeviceID, EnvironmentID: env, KeyVersion: nextKV, GrantGeneration: nextGG, Operation: "put", IdempotencyKey: id + "-" + name, Name: name, Payload: cryptox.EncodeBase64(cipher)}, manager.key))
		change.Mutations = append(change.Mutations, cryptox.MutationToWire(m))
	}
	packet := environmentValue(manager.client.PrepareEnvironmentChangeV2(ctx, environmentValue(cryptox.SignEnvironmentChange(change, manager.key)), manager.key))
	result, submitErr := manager.client.SubmitEnvironmentChangeV2(ctx, packet)
	if submitErr != nil {
		var raw syncclient.Pull
		got := f.raw(manager, "/pull?after=0&capability="+cryptox.EnvironmentOriginCapability, nil, &raw)
		candidateOK := false
		canonicalOK := false
		if raw.IssuerEvidence != nil {
			_, canonicalErr := raw.IssuerEvidence.CanonicalBytes()
			canonicalOK = canonicalErr == nil
			_, proofErr := cryptox.VerifyIssuerEvidenceV2(cryptox.PinnedIssuerRoot{AccountID: f.account, AccountGeneration: f.generation, DeviceID: f.root.RootDeviceID, SigningPublicKey: f.root.RootSigningPublicKey, ReceivingPublicKey: f.root.RootReceivingPublicKey}, *raw.IssuerEvidence, f.initial...)
			candidateOK = proofErr == nil
		}
		t.Fatalf("rotation accepted/pull failed; HTTP=%d candidateCanonical=%t candidateProof=%t failure=%v", got, canonicalOK, candidateOK, submitErr)
	}
	if !result.Applied {
		t.Fatal("rotation failed to converge through verified pull")
	}

	return packet
}
func (f *originFixture) restart(a *originActor) {
	t := f.t
	t.Helper()
	original := environmentValue(json.Marshal(a.receipt))
	old := a.engine.State().Cloud
	directory := a.directory
	originMust(t, a.store.Close())
	a.store = nil
	a.verifier.Close()
	a.store = environmentValue(localkeys.OpenEncryptedStateStore(localkeys.Config{Directory: directory, UserID: environmentValue(localkeys.CurrentUserID())}))
	a.engine = environmentValue(localstate.New(a.store))
	trust := environmentValue(a.store.Vault().LoadTrustContext())
	if !trust.Accepted || !bytes.Equal(trust.EnrollmentCertificate, original) {
		t.Fatal("dynamic ledger replaced original certificate receipt")
	}
	receipt := environmentValue(syncclient.DecodeEnrollmentReceiptV3(trust.EnrollmentCertificate))
	a.verifier = environmentValue(syncclient.NewPinnedVerifierV3(syncclient.IssuerOriginPinnedTrust{AccountID: f.account, AccountGeneration: 1, DeviceID: a.keys.DeviceID, DeviceSigningPublicKey: a.keys.SigningPublic, ReceivingPrivateKey: a.keys.ReceivingPrivate, Receipt: receipt}))
	originMust(t, a.verifier.ValidateStoredIssuerEvidence(a.engine.State().Cloud))
	missingLedger := originClone(a.engine.State().Cloud)
	missingLedger.IssuerEvidence = nil
	if err := a.verifier.ValidateStoredIssuerEvidence(missingLedger); err == nil {
		t.Fatal("cert3 cached data trusted after removal of the sealed issuer ledger")
	}
	if !reflect.DeepEqual(old, a.engine.State().Cloud) || len(old.IssuerEvidence) == 0 {
		t.Fatal("sealed state did not preserve atomic evidence/cache")
	}
	sealed := environmentValue(os.ReadFile(filepath.Join(directory, "state.v1.enc")))
	if bytes.Contains(sealed, old.IssuerEvidence) || bytes.Contains(sealed, []byte("synthetic-origin-X-value")) {
		t.Fatal("issuer ledger or value saved outside AEAD")
	}
	f.boot(a)
}

func TestNativeSPAKE2EnvironmentOriginsV3PrivacyRotationAndSealedResume(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("须启用固定 BoringSSL 原生构建；默认构建不代表实际 PAKE 验收")
	}
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
			if strings.Contains(path, "/pairings-v3/") && strings.HasSuffix(path, "/complete") {
				f.enrollmentPosts.Add(1)
				lose = f.loseEnrollment.Swap(false)
			}
			if strings.HasSuffix(path, "/environment-changes-v2") {
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
	defer f.proxy.Close()
	rootKeys := environmentValue(localkeys.GenerateDeviceKeys("temporary-root-key-placeholder"))
	rootKey := ed25519.NewKeyFromSeed(rootKeys.SigningSeed)
	defer clear(rootKey)
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
	var proof mobileworkflow.EmailProof
	for _, message := range mail {
		if message.To == f.email {
			for _, line := range strings.Split(message.Text, "\n") {
				if strings.HasPrefix(line, "{") {
					originMust(t, json.Unmarshal([]byte(line), &proof))
				}
			}
		}
	}
	if proof.AccountID != f.account || proof.Token == "" {
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
		AccountID          string                    `json:"accountId"`
		AccountGeneration  string                    `json:"accountGeneration"`
		DeviceID           string                    `json:"deviceId"`
		Root               *cryptox.TrustRoot        `json:"root"`
		InitialAuthorities []cryptox.SignedGrantWire `json:"initialAuthorities"`
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
	a.verifier = environmentValue(syncclient.NewRootPinnedVerifierWithOrigins(syncclient.OriginRootPinnedTrust{Trust: syncclient.PinnedTrust{AccountID: f.account, AccountGeneration: 1, DeviceID: a.keys.DeviceID, DeviceSigningPublicKey: a.keys.SigningPublic, ReceivingPrivateKey: a.keys.ReceivingPrivate}, Root: f.root, InitialAuthorities: f.initial}))
	defer a.verifier.Close()
	f.boot(a)
	rootPull := f.pull(a)
	privateCipher := ""
	privateLabelCipher := ""
	for _, event := range rootPull.Events {
		if event.Mutation.Mutation.Name == "X_PRIVATE_NAME_ORIGIN_TEST" {
			privateCipher = event.Mutation.Mutation.Payload
		}
	}
	for _, event := range rootPull.EnvironmentEvents {
		if event.Change.Change.EnvironmentID == x {
			privateLabelCipher = event.Change.Change.LabelPayload
		}
	}
	if privateCipher == "" || privateLabelCipher == "" {
		t.Fatal("privacy control samples missing from real root pull")
	}
	expiry := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	b := f.enroll(a, "origin-B", x, "admin", expiry, true)
	t.Log("真实 A→B 临时 Admin cert3 + 提交后504 + encryptedStore重启原ID重查通过")
	d := f.enroll(a, "origin-D", x, "ro", "0", false)
	t.Log("真实 A→D 永久只读 cert3 通过")
	f.pull(b)
	created := f.create(b, x, "origin-Y", "Y 合成环境", "origin-create-Y", true)
	if f.grant(b, "origin-Y").Grant.ExpiresAt != expiry {
		t.Fatal("new Y authority extended temporary manager expiry")
	}
	t.Log("B源签名创建Y、提交后504、原ID/hash查询+普通pull通过")
	f.put(b, "origin-Y", "Y_ONLY_NAME", "synthetic-origin-Y-value", "origin-Y-put")
	c := f.enroll(b, "origin-C", "origin-Y", "ro", expiry, false)
	t.Log("真实 B→C 仅Y只读 cert3 通过")
	if len(c.engine.State().Cloud.Environments) != 1 || c.engine.State().Cloud.Environments["origin-Y"].Values["Y_ONLY_NAME"] != "synthetic-origin-Y-value" {
		t.Fatal("C did not read only selected Y")
	}
	var cPull syncclient.Pull
	if got := f.raw(c, "/pull?after=0&capability="+cryptox.EnvironmentOriginCapability, nil, &cPull); got != 200 {
		t.Fatal("C actual pull status", got)
	}
	candidate := environmentValue(json.Marshal(cPull))
	receipt := environmentValue(json.Marshal(c.receipt))
	for _, secret := range []string{"X_PRIVATE_LABEL_ORIGIN_TEST", "X_PRIVATE_NAME_ORIGIN_TEST", "synthetic-origin-X-value", privateCipher, privateLabelCipher} {
		if secret != "" && (bytes.Contains(candidate, []byte(secret)) || bytes.Contains(receipt, []byte(secret))) {
			t.Fatal("C Y-only proof/pull leaked unrelated X data")
		}
	}
	if cPull.IssuerEvidence == nil || len(cPull.IssuerEvidence.Origins) != 1 {
		t.Fatal("C lacks real Y creation origin")
	}
	beforeC := c.engine.State().Cloud
	oldProfile := originClone(cPull)
	oldProfile.IssuerEvidence.Profile = "harmonia/issuer-proof/v1"
	if _, err := c.verifier.VerifyPull(ctx, oldProfile, beforeC); err == nil {
		t.Fatal("old evidence profile accepted")
	}

	missing := originClone(cPull)
	missing.IssuerEvidence.Origins = []cryptox.SignedEnvironmentOrigin{}
	if _, err := missing.IssuerEvidence.CanonicalBytes(); err != nil {
		t.Fatal("missing-origin negative fixture was not structurally canonical", err)
	}
	if _, err := c.verifier.VerifyPull(ctx, missing, beforeC); err == nil {
		t.Fatal("missing creation origin accepted")
	}
	if !reflect.DeepEqual(beforeC, c.engine.State().Cloud) {
		t.Fatal("invalid proof changed authoritative state")
	}
	originMust(t, d.engine.SetPaused(true))
	pausedBefore := d.engine.State().Cloud
	rotation := f.rotate(b, x, "X_PRIVATE_ROTATED_LABEL_ORIGIN_TEST", "origin-rotate-X")
	f.pull(a)
	if a.engine.State().Cloud.Environments[x].ExpiresAt != nil || a.engine.State().Cloud.Environments[x].Role != localstate.Admin {
		t.Fatal("root dynamic verifier lost permanent authority")
	}
	for _, g := range rotation.Change.Grants {
		switch g.Grant.SubjectDeviceID {
		case a.keys.DeviceID:
			if g.Grant.Role != "admin" || g.Grant.ExpiresAt != "0" {
				t.Fatal("temporary B shortened permanent root authority")
			}
		case b.keys.DeviceID:
			if g.Grant.Role != "admin" || g.Grant.ExpiresAt != expiry {
				t.Fatal("temporary B rotation extended expiry")
			}
		case d.keys.DeviceID:
			if g.Grant.Role != "ro" || g.Grant.ExpiresAt != "0" {
				t.Fatal("reader right changed during rotation")
			}
		}
	}
	if len(rotation.Change.Grants) != 3 || len(created.Origin.Origin.After) != 1 {
		t.Fatal("rotation/create complete rights set mismatch")
	}
	auth := environmentValue(d.client.RefreshAuthorizations(ctx))
	pausedAfter := d.engine.State().Cloud
	if auth.Scope != "authorizations" || len(auth.Events) != 0 || pausedAfter.Sequence != pausedBefore.Sequence || !reflect.DeepEqual(pausedAfter.SeenMutations, pausedBefore.SeenMutations) || pausedAfter.AuthorizationSequence <= pausedBefore.Sequence {
		t.Fatal("paused authorization refresh advanced data history")
	}
	if bytes.Equal(pausedBefore.IssuerEvidence, pausedAfter.IssuerEvidence) {
		t.Fatal("paused current origin evidence did not persist")
	}
	if pausedAfter.Environments[x].Values["X_PRIVATE_NAME_ORIGIN_TEST"] != "" {
		t.Fatal("paused new-key authority retained invalid old-key cache")
	}
	originMust(t, d.verifier.ValidateStoredIssuerEvidence(pausedAfter))
	f.restart(d)
	originMust(t, d.engine.SetPaused(false))
	f.pull(d)
	if d.engine.State().Cloud.Environments[x].KeyVersion != 2 || d.engine.State().Cloud.Environments[x].Values["X_PRIVATE_NAME_ORIGIN_TEST"] != "synthetic-origin-X-value" {
		t.Fatal("reader failed verified full catch-up after paused rotation")
	}
	f.restart(b)
	f.pull(b)
	f.restart(c)
	f.pull(c)
	var latestC syncclient.Pull
	if got := f.raw(c, "/pull?after=0&capability="+cryptox.EnvironmentOriginCapability, nil, &latestC); got != 200 {
		t.Fatal("C rotated issuer proof pull failed", got)
	}
	latestBytes := environmentValue(json.Marshal(latestC))
	if len(rotation.Change.Mutations) == 0 {
		t.Fatal("real rotation did not include the live X value")
	}
	for _, secret := range []string{"X_PRIVATE_ROTATED_LABEL_ORIGIN_TEST", rotation.Change.LabelPayload, rotation.Change.Mutations[0].Mutation.Name, rotation.Change.Mutations[0].Mutation.Payload} {
		if bytes.Contains(latestBytes, []byte(secret)) {
			t.Fatal("C origin provenance leaked rotated X data")
		}
	}
	// v2入口强制携带与原数据操作绑定的来源证明，缺失不能改走旧验证。
	var missingOriginResponse struct {
		Error string `json:"error"`
	}
	if got := f.raw(b, "/environment-changes-v2", cryptox.SignedEnvironmentChange{Change: created.Change, Signature: created.Signature}, &missingOriginResponse); got != 400 {
		t.Fatal("v2 endpoint accepted missing origin", got)
	}
	if len(c.engine.State().Cloud.Environments) != 1 || c.engine.State().Cloud.Environments[x].ID != "" {
		t.Fatal("C gained X after issuer rotation/restart")
	}

	// 实际 HTTP 协商不能把旧 profile 静默当作 v3。
	oldKeys := environmentValue(localkeys.GenerateDeviceKeys("origin-old-profile"))
	sum := sha256.Sum256([]byte(f.password))
	oldLogin := environmentValue(syncclient.Login(ctx, syncclient.LoginConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), Email: f.email, Credential: hex.EncodeToString(sum[:])}))
	var unsupported any
	if got := callJSON(t, f.proxy.Client(), f.proxy.URL, f.base()+"/pairings-v3", oldLogin.Token, oldKeys.DeviceID, f.generation, map[string]any{"idempotencyKey": "origin-old-profile", "deviceId": oldKeys.DeviceID, "signingPublicKey": cryptox.EncodeBase64(oldKeys.SigningPublic), "receivingPublicKey": cryptox.EncodeBase64(oldKeys.ReceivingPublic), "approverDeviceId": b.keys.DeviceID, "certificateVersion": "2", "capabilities": []string{"issuer-proof-v1"}}, &unsupported); got != 400 {
		t.Fatal("old capability accepted by cert3 route", got)
	}
	f.formalDaemon(c)
	t.Log("真实注册/init、原生双向 PAKE cert3、临时 Admin 创建/轮换、Y-only 隐私、暂停授权账本、原收据加密重启、丢回应原 ID 查询及正式 daemon boot/IPC 通过")
}

// 真正已入网目录交给独立 CLI daemon；删除会话后须经 HTTPS 持钥 boot，不能用 fixture 模式。
func (f *originFixture) formalDaemon(a *originActor) {
	t := f.t
	t.Helper()
	uid := environmentValue(localkeys.CurrentUserID())
	directory := a.directory
	private := filepath.Dir(directory)
	originMust(t, a.store.Vault().Delete("session-v1"))
	originMust(t, a.store.Close())
	a.store = nil
	ca := filepath.Join(private, "origin-ca.pem")
	originMust(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.proxy.Certificate().Raw}), 0600))
	binary := filepath.Join(private, "origin-harmonia")
	goEnv := exec.Command("go", "env", "GOCACHE", "GOMODCACHE", "GOPATH")
	goEnv.Env = []string{"PATH=" + os.Getenv("PATH")}
	for _, key := range []string{"HOME", "USERPROFILE", "GOCACHE", "GOMODCACHE", "GOPATH"} {
		if value := os.Getenv(key); value != "" {
			goEnv.Env = append(goEnv.Env, key+"="+value)
		}
	}
	caches := strings.Split(strings.TrimSpace(string(environmentValue(goEnv.Output()))), "\n")
	if len(caches) != 3 {
		t.Fatal("Go cache tool result invalid")
	}
	build := exec.Command("go", "build", "-o", binary, "./cmd/harmonia")
	build.Dir = "../core-go"
	build.Env = []string{"PATH=" + os.Getenv("PATH"), "GOCACHE=" + caches[0], "GOMODCACHE=" + caches[1], "GOPATH=" + caches[2], "CGO_ENABLED=0"}
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("正式 CLI 编译失败：%v\n%s", err, output)
	}
	logfile := environmentValue(os.OpenFile(filepath.Join(private, "origin-daemon.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600))
	defer logfile.Close()
	before := f.boots.Load()
	expected := a.engine.State().Cloud.Sequence
	process := exec.Command(binary, "daemon", "--local-directory", directory, "--local-user", uid, "--ca-file", ca, "--platform-fragment", filepath.Join(directory, "environment.sh"), "--interval", "20ms", "--sync-interval", "1s")
	process.Env = []string{"PATH=" + os.Getenv("PATH")}
	process.Stdout, process.Stderr = io.Discard, logfile
	originMust(t, process.Start())
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	defer func() {
		_ = process.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			originMust(t, err)
		case <-time.After(5 * time.Second):
			_ = process.Process.Kill()
			<-done
			t.Error("正式 cert3 daemon 未在 SIGTERM 后正常退出")
		}
	}()
	cli := func(command string, args ...string) ([]byte, error) {
		request := exec.Command(binary, append([]string{command, "--local-directory", directory, "--local-user", uid}, args...)...)
		request.Env = []string{"PATH=" + os.Getenv("PATH")}
		return request.CombinedOutput()
	}
	ready := false
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		output, err := cli("status")
		var status localipc.Status
		if err == nil && json.Unmarshal(output, &status) == nil && status.AccountGeneration == 1 && status.Environments == 1 && status.Sequence >= expected && f.boots.Load() > before {
			ready = true
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !ready {
		data, _ := os.ReadFile(logfile.Name())
		t.Fatalf("正式 cert3 daemon 未收敛；固定诊断 closed=%t evidence=%t cancel=%t", bytes.Contains(data, []byte("local account session changed")), bytes.Contains(data, []byte("evidence")), bytes.Contains(data, []byte("context canceled")))
	}
	if _, err := cli("activate", "--environment", "origin-Y", "--priority", "10"); err != nil {
		t.Fatal("正式 cert3 activation IPC 失败")
	}
	output, err := cli("export")
	originMust(t, err)
	if string(output) != "export Y_ONLY_NAME='synthetic-origin-Y-value'\n" {
		t.Fatal("正式 cert3 export 未限定已授权 Y 值")
	}
	if _, err := cli("put", "--environment", "origin-Y", "--name", "SYNTHETIC_DENIED", "--value-stdin"); err == nil {
		t.Fatal("RO cert3 CLI 未拒绝共享写")
	}
}

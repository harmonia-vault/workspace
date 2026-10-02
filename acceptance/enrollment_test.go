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
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localkeys"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/pairing"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 真实原生 SPAKE2、真实 TS/SQLite，所有钥匙、账户和配置均为独立合成测试。
// 默认构建明确跳过；原生库按 pairing/boringssl.lock.json 构建后运行 acceptance-native。
func TestNativeSPAKE2FirstVaultEnrollmentBootAndSync(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("未启用固定 BoringSSL 原生实现；须运行 mise run acceptance-native")
	}
	ctx := context.Background()
	f := startFixture(t, "--empty-vault")
	if len(f.Devices) != 0 || len(f.Grants) != 0 {
		t.Fatal("首次初始化仍携带夹具权限")
	}
	backend, _ := url.Parse(f.Endpoint)
	transport := httputil.NewSingleHostReverseProxy(backend)
	var loseCompletion atomic.Bool
	var loseMutation atomic.Bool
	var completedBoots atomic.Int64
	transport.ModifyResponse = func(response *http.Response) error {
		if response.Request.URL.Path == "/v1/accounts/"+f.AccountID+"/boot-sessions" && response.StatusCode == 200 {
			completedBoots.Add(1)
		}
		if (response.Request.URL.Path == "/v1/accounts/"+f.AccountID+"/pairings/enroll-native-e2e/complete" && loseCompletion.Swap(false) || response.Request.URL.Path == "/v1/accounts/"+f.AccountID+"/mutations" && loseMutation.Swap(false)) && response.StatusCode == 200 {
			_ = response.Body.Close()
			response.StatusCode = 504
			response.Body = io.NopCloser(bytes.NewBufferString(`{"error":"request_rejected"}`))
			response.ContentLength = -1
			response.Header.Del("Content-Length")
		}
		return nil
	}
	proxy := httptest.NewTLSServer(transport)
	defer proxy.Close()
	httpClient := proxy.Client()
	httpClient.Timeout = 15 * time.Second
	base := "/v1/accounts/" + f.AccountID
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var login struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expiresAt"`
	}
	if got := callJSON(t, httpClient, proxy.URL, "/v1/login", "", "", "", map[string]string{"email": f.Email, "credential": f.Credential}, &login); got != 200 {
		t.Fatalf("登录HTTP %d", got)
	}
	manager, err := localkeys.GenerateDeviceKeys("first-phone")
	must(err)
	managerPrivate := ed25519.NewKeyFromSeed(manager.SigningSeed)
	defer clear(managerPrivate)
	recoverySeed, err := cryptox.GenerateRecoverySeed()
	must(err)
	defer clear(recoverySeed)
	recovery, err := cryptox.DeriveRecoveryKeys(recoverySeed, f.AccountID, "1", "1")
	must(err)
	envKey, err := cryptox.GenerateEnvironmentKey()
	must(err)
	defer clear(envKey)
	root, err := cryptox.SignTrustRoot(f.AccountID, "1", cryptox.TrustRoot{RootDeviceID: manager.DeviceID, RootSigningPublicKey: cryptox.EncodeBase64(manager.SigningPublic), RootReceivingPublicKey: cryptox.EncodeBase64(manager.ReceivingPublic), RecoveryGeneration: "1", RecoverySigningPublicKey: cryptox.EncodeBase64(recovery.SigningPublic), RecoveryReceivingPublicKey: cryptox.EncodeBase64(recovery.ReceivingPublic)}, recovery.SigningPrivate)
	must(err)
	deviceEnvelope, err := cryptox.WrapEnvironmentKey(envKey, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", RecipientType: "device", RecipientID: manager.DeviceID, RecipientGeneration: "1", RecipientPublicKey: root.RootReceivingPublicKey})
	must(err)
	recoveryEnvelope, err := cryptox.WrapEnvironmentKey(envKey, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", RecipientType: "recovery", RecipientID: f.AccountID, RecipientGeneration: "1", RecipientPublicKey: root.RecoveryReceivingPublicKey})
	must(err)
	grant := cryptox.Grant{AccountID: f.AccountID, AccountGeneration: "1", IssuerDeviceID: manager.DeviceID, SubjectDeviceID: manager.DeviceID, SubjectSigningPublicKey: root.RootSigningPublicKey, SubjectReceivingPublicKey: root.RootReceivingPublicKey, EnvironmentID: "dev", KeyVersion: "1", GrantGeneration: "1", Role: "admin", ExpiresAt: "0", IdempotencyKey: "first-self-admin", Envelope: cryptox.EncodeBase64(deviceEnvelope)}
	signedSelf, err := cryptox.SignGrant(grant, managerPrivate)
	must(err)
	proposal := cryptox.InitializationProposal{IdempotencyKey: "first-vault-e2e", Device: cryptox.InitializationDevice{ID: manager.DeviceID, SigningPublicKey: root.RootSigningPublicKey, ReceivingPublicKey: root.RootReceivingPublicKey}, RecoveryGeneration: "1", RecoverySigningPublicKey: root.RecoverySigningPublicKey, RecoveryReceivingPublicKey: root.RecoveryReceivingPublicKey, TrustRootSignature: root.Signature, Environments: []cryptox.InitializationEnvironment{{EnvironmentID: "dev", KeyVersion: "1", RecoveryEnvelope: cryptox.EncodeBase64(recoveryEnvelope), Grant: cryptox.GrantToWire(signedSelf)}}}
	proposalHash, err := proposal.Hash(f.AccountID, "1")
	must(err)
	var initial struct {
		State          string   `json:"state"`
		ChallengeID    string   `json:"challengeId"`
		Nonce          string   `json:"nonce"`
		ExpiresAt      int64    `json:"expiresAt"`
		ProposalHash   string   `json:"proposalHash"`
		SigningPayload []string `json:"signingPayload"`
		Sequence       uint64   `json:"sequence"`
	}
	if got := callJSON(t, httpClient, proxy.URL, base+"/vault-initializations", login.Token, "", "1", proposal, &initial); got != 200 {
		t.Fatalf("初始化提案HTTP %d", got)
	}
	proof, err := cryptox.NewInitializationProof(f.AccountID, "1", login.Token, initial.ChallengeID, initial.Nonce, strconv.FormatInt(initial.ExpiresAt, 10), proposalHash)
	must(err)
	proofBytes, err := proof.SigningBytes()
	must(err)
	var expected []string
	must(json.Unmarshal(proofBytes, &expected))
	if initial.ProposalHash != proposalHash || !reflect.DeepEqual(initial.SigningPayload, expected) || initial.ExpiresAt <= time.Now().Unix() || initial.ExpiresAt > time.Now().Unix()+125 {
		t.Fatal("初始化挑战未绑定本地提案和会话")
	}
	deviceSig, err := cryptox.SignInitializationProof(proof, managerPrivate)
	must(err)
	recoverySig, err := cryptox.SignInitializationProof(proof, recovery.SigningPrivate)
	must(err)
	finish := map[string]string{"challengeId": initial.ChallengeID, "deviceSignature": deviceSig, "recoverySignature": recoverySig}
	if got := callJSON(t, httpClient, proxy.URL, base+"/vault-initializations/first-vault-e2e/complete", login.Token, "", "1", finish, &initial); got != 200 {
		t.Fatalf("双签首次初始化HTTP %d", got)
	}
	if initial.State != "complete" || initial.Sequence != 1 {
		t.Fatal("首次初始化未原子完成")
	}
	// 首次管理手机也用持钥 boot 取得绑定会话，不把登录 token 当设备凭据。
	var boot recoveryChallenge
	if got := callJSON(t, httpClient, proxy.URL, base+"/boot-challenges", "", "", "", map[string]string{"deviceId": manager.DeviceID, "accountGeneration": "1"}, &boot); got != 200 {
		t.Fatalf("首手机boot HTTP %d", got)
	}
	bootProof, err := cryptox.NewDeviceBootProof(f.AccountID, "1", manager.DeviceID, manager.SigningPublic, manager.ReceivingPublic, boot.ChallengeID, boot.Nonce, strconv.FormatInt(boot.ExpiresAt, 10))
	must(err)
	bootBytes, err := bootProof.SigningBytes()
	must(err)
	must(json.Unmarshal(bootBytes, &expected))
	if !reflect.DeepEqual(expected, boot.SigningPayload) {
		t.Fatal("首手机boot挑战公钥不匹配")
	}
	bootSig, err := cryptox.SignDeviceBootProof(bootProof, managerPrivate)
	must(err)
	var managerSession struct {
		Token string `json:"token"`
	}
	if got := callJSON(t, httpClient, proxy.URL, base+"/boot-sessions", "", "", "", map[string]string{"deviceId": manager.DeviceID, "accountGeneration": "1", "challengeId": boot.ChallengeID, "signature": bootSig}, &managerSession); got != 200 {
		t.Fatalf("首手机boot完成HTTP %d", got)
	}
	temporary, err := os.MkdirTemp("/tmp", "harmonia-enroll-")
	must(err)
	t.Cleanup(func() { _ = os.RemoveAll(temporary) })
	temporary, err = filepath.EvalSymlinks(temporary)
	must(err)
	uid, err := localkeys.CurrentUserID()
	must(err)
	store, err := localkeys.OpenEncryptedStateStore(localkeys.Config{Directory: filepath.Join(temporary, "protected"), UserID: uid})
	must(err)
	defer store.Close()
	device, err := localkeys.GenerateDeviceKeys("new-cli")
	must(err)
	must(store.Vault().SaveDeviceKeys(device))
	private := ed25519.NewKeyFromSeed(device.SigningSeed)
	defer clear(private)
	must(store.Vault().SaveSession(localkeys.LoginSession{Endpoint: proxy.URL, AccountID: f.AccountID, AccountGeneration: 1, Token: login.Token, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339Nano)}))
	engine, err := localstate.New(store)
	must(err)
	config := syncclient.EnrollmentConfig{Endpoint: proxy.URL, HTTPClient: httpClient, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: device.DeviceID, LoginToken: login.Token, SigningKey: private, ReceivingPrivateKey: device.ReceivingPrivate, Engine: engine}
	enrollment, err := syncclient.NewEnrollment(config)
	must(err)
	defer enrollment.Close()
	code, err := pairing.GenerateShortCode()
	must(err)
	defer clear(code)
	status, err := enrollment.Begin(ctx, manager.DeviceID, "enroll-native-e2e", code)
	must(err)
	if status.Context.ApproverSigningPublicKey != root.RootSigningPublicKey || status.Context.ApproverReceivingPublicKey != root.RootReceivingPublicKey {
		t.Fatal("手机端配对上下文不匹配已持有钥匙")
	}
	managerPAKE, message, err := pairing.NewApprover(status.Context, code)
	must(err)
	defer managerPAKE.Close()
	relay := func(kind string, payload []byte) syncclient.PairingStatus {
		t.Helper()
		c := status.Context
		r := cryptox.PairingRelay{AccountID: c.AccountID, AccountGeneration: c.AccountGeneration, SessionID: c.SessionID, ChallengeNonce: c.ChallengeNonce, Side: "approver", Kind: kind, Payload: cryptox.EncodeBase64(payload)}
		signature, err := cryptox.SignPairingRelay(r, managerPrivate)
		must(err)
		var out syncclient.PairingStatus
		if got := callJSON(t, httpClient, proxy.URL, base+"/pairings/enroll-native-e2e/relay", managerSession.Token, manager.DeviceID, "1", cryptox.PairingRelayRequest{Side: "approver", Kind: kind, Payload: r.Payload, Signature: signature}, &out); got != 200 {
			t.Fatalf("管理手机中继HTTP %d", got)
		}
		return out
	}
	status = relay("message", message)
	peer, err := cryptox.DecodeBase64(status.Messages["initiator"], 32, 32)
	must(err)
	confirmation, err := managerPAKE.Complete(peer)
	must(err)
	status = relay("confirmation", confirmation)
	status, err = enrollment.Advance(ctx)
	must(err)
	peerMAC, err := cryptox.DecodeBase64(status.Confirmations["initiator"], 32, 32)
	must(err)
	must(managerPAKE.VerifyPeerConfirmation(peerMAC))
	transcript, err := managerPAKE.TranscriptHash()
	must(err)
	newEnvelope, err := cryptox.WrapEnvironmentKey(envKey, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", RecipientType: "device", RecipientID: device.DeviceID, RecipientGeneration: "1", RecipientPublicKey: cryptox.EncodeBase64(device.ReceivingPublic)})
	must(err)
	grant.SubjectDeviceID = device.DeviceID
	grant.SubjectSigningPublicKey = cryptox.EncodeBase64(device.SigningPublic)
	grant.SubjectReceivingPublicKey = cryptox.EncodeBase64(device.ReceivingPublic)
	grant.Role = "rw"
	grant.ExpiresAt = strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	grant.IdempotencyKey = "enroll-native-grant"
	grant.Envelope = cryptox.EncodeBase64(newEnvelope)
	signedGrant, err := cryptox.SignGrant(grant, managerPrivate)
	must(err)
	approval := cryptox.EnrollmentApproval{Context: status.Context.EnrollmentContext(), PairingProfile: pairing.Profile, TranscriptHash: transcript, Grants: []cryptox.SignedGrantWire{cryptox.GrantToWire(signedGrant)}}
	certificate, err := approval.Certificate()
	must(err)
	signature, err := cryptox.SignEnrollmentCertificate(certificate, managerPrivate)
	must(err)
	if got := callJSON(t, httpClient, proxy.URL, base+"/pairings/enroll-native-e2e/approve", managerSession.Token, manager.DeviceID, "1", map[string]any{"grants": approval.Grants, "transcriptHash": transcript, "signature": signature}, &status); got != 200 {
		t.Fatalf("手机双向确认后批准HTTP %d", got)
	}
	_, err = enrollment.Advance(ctx)
	must(err)
	receipt, err := enrollment.Receipt()
	must(err)
	if engine.State().Cloud.AccountID != "" {
		t.Fatal("入网未经普通pull便写入云权威缓存")
	}
	receiptJSON, err := json.Marshal(receipt)
	must(err)
	trust := localkeys.TrustContext{Endpoint: proxy.URL, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: device.DeviceID, SigningPublic: device.SigningPublic, ReceivingPublic: device.ReceivingPublic, Managers: map[string][]byte{manager.DeviceID: manager.SigningPublic}, PairingProfile: pairing.Profile, EnrollmentCertificate: receiptJSON, EnrollmentKey: receipt.IdempotencyKey, Accepted: false}
	must(store.Vault().SaveTrustContext(trust))
	encryptedReceipt, err := os.ReadFile(filepath.Join(temporary, "protected", "trust.v1.enc"))
	must(err)
	if bytes.Contains(encryptedReceipt, receiptJSON) || bytes.Contains(encryptedReceipt, []byte(login.Token)) || bytes.Contains(encryptedReceipt, code) {
		t.Fatal("待完成资料未加密")
	}
	// 故意丢掉已提交响应：不能重复配对，重启后先按原 key 查询完成状态。
	loseCompletion.Store(true)
	if _, err = enrollment.Complete(ctx); !errors.Is(err, syncclient.ErrEnrollmentPending) {
		t.Fatal("提交后不明结果未保留待完成状态", err)
	}
	enrollment.Close()
	saved, err := store.Vault().LoadTrustContext()
	must(err)
	if saved.Accepted {
		t.Fatal("不明结果被本地标为已完成")
	}
	var savedReceipt syncclient.EnrollmentReceipt
	must(json.Unmarshal(saved.EnrollmentCertificate, &savedReceipt))
	resumed, err := syncclient.ResumeEnrollment(config, savedReceipt)
	must(err)
	defer resumed.Close()
	result, err := resumed.Complete(ctx)
	must(err)
	if result.Sequence != 2 {
		t.Fatal("重查入网结果增加新写或序号异常")
	}
	trust.Accepted = true
	must(store.Vault().SaveTrustContext(trust))
	bootClient, err := syncclient.NewForBoot(syncclient.Config{Endpoint: proxy.URL, HTTPClient: httpClient, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: device.DeviceID, Verifier: result.Verifier, Engine: engine})
	must(err)
	synced, err := bootClient.BootDevice(ctx, private)
	must(err)
	_, err = synced.Pull(ctx)
	must(err)
	must(engine.Activate("dev", 10, time.Now()))
	packet, err := cryptox.EncryptValue(envKey, cryptox.ValueContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", Name: "NATIVE_SYNTHETIC"}, []byte("native-paired-value"))
	must(err)
	mutation := cryptox.Mutation{AccountID: f.AccountID, AccountGeneration: "1", DeviceID: device.DeviceID, EnvironmentID: "dev", KeyVersion: "1", GrantGeneration: "1", Operation: "put", IdempotencyKey: "native-enrolled-write", Name: "NATIVE_SYNTHETIC", Payload: cryptox.EncodeBase64(packet)}
	written, err := synced.Submit(ctx, wireMutation(t, mutation, private))
	must(err)
	if !written.Applied || written.Accepted.Sequence != 3 {
		t.Fatal("真实入网设备未经正式下发完成写入")
	}
	effective, err := engine.Effective(time.Now())
	must(err)
	if effective[mutation.Name] != "native-paired-value" {
		t.Fatal("跨语言签名/HPKE/AEAD值不匹配")
	}
	onDisk, err := os.ReadFile(filepath.Join(temporary, "protected", "state.v1.enc"))
	must(err)
	if bytes.Contains(onDisk, []byte("native-paired-value")) {
		t.Fatal("真实下发缓存出现明文")
	}
	grant.GrantGeneration = "2"
	grant.Role = "none"
	grant.ExpiresAt = "0"
	grant.Envelope = ""
	grant.IdempotencyKey = "native-enrolled-revoke"
	// 后台启动只持已入网设备钥和双签回执；删除旧登录 session 后仍须真实 boot。
	must(store.Vault().Delete("session-v1"))
	must(store.Close())
	bootBeforeDaemon := completedBoots.Load()
	exerciseProtectedDaemonProcess(t, filepath.Join(temporary, "protected"), uid, proxy.Certificate(), func() bool {
		return completedBoots.Load() > bootBeforeDaemon
	}, func() { loseMutation.Store(true) }, func() {
		if got := callJSON(t, httpClient, proxy.URL, base+"/grants", managerSession.Token, manager.DeviceID, "1", wireGrant(t, grant, managerPrivate), nil); got != 200 {
			t.Fatalf("正式已入网设备撤销HTTP %d", got)
		}
	})
}

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
func TestNativeSPAKE2TwoManagersAndHistoricalIssuerProof(t *testing.T) {
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
	var loseV2Completion atomic.Bool
	var loseMutation atomic.Bool
	var completedBoots atomic.Int64
	transport.ModifyResponse = func(response *http.Response) error {
		if response.Request.URL.Path == "/v1/accounts/"+f.AccountID+"/boot-sessions" && response.StatusCode == 200 {
			completedBoots.Add(1)
		}
		if (response.Request.URL.Path == "/v1/accounts/"+f.AccountID+"/pairings-v2/enroll-C-v2/complete" && loseV2Completion.Swap(false) || response.Request.URL.Path == "/v1/accounts/"+f.AccountID+"/pairings/enroll-native-e2e/complete" && loseCompletion.Swap(false) || response.Request.URL.Path == "/v1/accounts/"+f.AccountID+"/mutations" && loseMutation.Swap(false)) && response.StatusCode == 200 {
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
	grant.Role = "admin"
	grant.ExpiresAt = "0"
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

	// A 是真实初始化根；B 已经通过真实 v1 PAKE/双签获得 Admin。
	// 此后 C 只确认 B，却须解密并验证 A/B 的历史共享写入。
	bootClient, err := syncclient.NewForBoot(syncclient.Config{Endpoint: proxy.URL, HTTPClient: httpClient, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: device.DeviceID, Verifier: result.Verifier, Engine: engine})
	must(err)
	clientB, err := bootClient.BootDevice(ctx, private)
	must(err)
	_, err = clientB.Pull(ctx)
	must(err)
	must(engine.Activate("dev", 10, time.Now()))
	rootPacket, err := cryptox.EncryptValue(envKey, cryptox.ValueContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", Name: "HISTORICAL_A"}, []byte("historical-a-synthetic"))
	must(err)
	rootMutation := cryptox.Mutation{AccountID: f.AccountID, AccountGeneration: "1", DeviceID: manager.DeviceID, EnvironmentID: "dev", KeyVersion: "1", GrantGeneration: "1", Operation: "put", IdempotencyKey: "historical-A", Name: "HISTORICAL_A", Payload: cryptox.EncodeBase64(rootPacket)}
	if got := callJSON(t, httpClient, proxy.URL, base+"/mutations", managerSession.Token, manager.DeviceID, "1", wireMutation(t, rootMutation, managerPrivate), nil); got != 200 {
		t.Fatalf("根 A 历史写HTTP %d", got)
	}
	bPacket, err := cryptox.EncryptValue(envKey, cryptox.ValueContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", Name: "HISTORICAL_B"}, []byte("historical-b-synthetic"))
	must(err)
	bMutation := cryptox.Mutation{AccountID: f.AccountID, AccountGeneration: "1", DeviceID: device.DeviceID, EnvironmentID: "dev", KeyVersion: "1", GrantGeneration: "1", Operation: "put", IdempotencyKey: "historical-B", Name: "HISTORICAL_B", Payload: cryptox.EncodeBase64(bPacket)}
	bWritten, err := clientB.Submit(ctx, wireMutation(t, bMutation, private))
	must(err)
	if !bWritten.Applied || bWritten.Accepted.Sequence != 4 {
		t.Fatal("B Admin未经正常下发取得历史", bWritten)
	}

	deviceC, err := localkeys.GenerateDeviceKeys("new-C")
	must(err)
	defer clear(deviceC.SigningSeed)
	defer clear(deviceC.ReceivingPrivate)
	privateC := ed25519.NewKeyFromSeed(deviceC.SigningSeed)
	defer clear(privateC)
	storeC, err := localkeys.OpenEncryptedStateStore(localkeys.Config{Directory: filepath.Join(temporary, "protected-C"), UserID: uid})
	must(err)
	defer storeC.Close()
	must(storeC.Vault().SaveDeviceKeys(deviceC))
	engineC, err := localstate.New(storeC)
	must(err)
	configC := syncclient.EnrollmentConfig{Endpoint: proxy.URL, HTTPClient: httpClient, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: deviceC.DeviceID, LoginToken: login.Token, SigningKey: privateC, ReceivingPrivateKey: deviceC.ReceivingPrivate, Engine: engineC}
	enrollmentC, err := syncclient.NewEnrollmentV2(configC)
	must(err)
	defer enrollmentC.Close()
	codeC, err := pairing.GenerateShortCode()
	must(err)
	defer clear(codeC)
	statusC, err := enrollmentC.Begin(ctx, device.DeviceID, "enroll-C-v2", codeC)
	must(err)
	if statusC.CertificateVersion != "2" || !reflect.DeepEqual(statusC.Capabilities, []string{cryptox.IssuerProofCapability}) || statusC.Context.ApproverSigningPublicKey != cryptox.EncodeBase64(device.SigningPublic) || statusC.Context.ApproverReceivingPublicKey != cryptox.EncodeBase64(device.ReceivingPublic) {
		t.Fatal("v2能力/已确认B双公钥未精确绑定")
	}
	pakeB, messageB, err := pairing.NewApprover(statusC.Context, codeC)
	must(err)
	defer pakeB.Close()
	var sessionB struct {
		Token string `json:"token"`
	}
	relayB := func(kind string, payload []byte) syncclient.PairingStatusV2 {
		t.Helper()
		c := statusC.Context
		signedRelay, err := cryptox.SignPairingRelay(cryptox.PairingRelay{AccountID: c.AccountID, AccountGeneration: c.AccountGeneration, SessionID: c.SessionID, ChallengeNonce: c.ChallengeNonce, Side: "approver", Kind: kind, Payload: cryptox.EncodeBase64(payload)}, private)
		must(err)
		var out syncclient.PairingStatusV2
		// B 的设备绑定 token 来自刚才的真实 boot；token 只在测试内存。
		// 正式原生批准层同样只使用自己的已验证会话。
		if got := callJSON(t, httpClient, proxy.URL, base+"/pairings-v2/enroll-C-v2/relay", sessionB.Token, device.DeviceID, "1", cryptox.PairingRelayRequest{Side: "approver", Kind: kind, Payload: cryptox.EncodeBase64(payload), Signature: signedRelay}, &out); got != 200 {
			t.Fatalf("B v2中继HTTP %d", got)
		}
		return out
	}
	// 独立 boot 取 B 的管理 API 会话，不能复用账号登录 token 冒充 B。
	var challengeB recoveryChallenge
	if got := callJSON(t, httpClient, proxy.URL, base+"/boot-challenges", "", "", "", map[string]string{"deviceId": device.DeviceID, "accountGeneration": "1"}, &challengeB); got != 200 {
		t.Fatalf("B新bootHTTP %d", got)
	}
	proofB, err := cryptox.NewDeviceBootProof(f.AccountID, "1", device.DeviceID, device.SigningPublic, device.ReceivingPublic, challengeB.ChallengeID, challengeB.Nonce, strconv.FormatInt(challengeB.ExpiresAt, 10))
	must(err)
	bootSignatureB, err := cryptox.SignDeviceBootProof(proofB, private)
	must(err)
	if got := callJSON(t, httpClient, proxy.URL, base+"/boot-sessions", "", "", "", map[string]string{"deviceId": device.DeviceID, "accountGeneration": "1", "challengeId": challengeB.ChallengeID, "signature": bootSignatureB}, &sessionB); got != 200 {
		t.Fatalf("B管理boot-sessionHTTP %d", got)
	}
	statusC = relayB("message", messageB)
	peerC, err := cryptox.DecodeBase64(statusC.Messages["initiator"], 32, 32)
	must(err)
	confirmationB, err := pakeB.Complete(peerC)
	must(err)
	statusC = relayB("confirmation", confirmationB)
	statusC, err = enrollmentC.Advance(ctx)
	must(err)
	peerConfirmationC, err := cryptox.DecodeBase64(statusC.Confirmations["initiator"], 32, 32)
	must(err)
	must(pakeB.VerifyPeerConfirmation(peerConfirmationC))
	transcriptC, err := pakeB.TranscriptHash()
	must(err)
	envelopeC, err := cryptox.WrapEnvironmentKey(envKey, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", RecipientType: "device", RecipientID: deviceC.DeviceID, RecipientGeneration: "1", RecipientPublicKey: cryptox.EncodeBase64(deviceC.ReceivingPublic)})
	must(err)
	grantC := cryptox.Grant{AccountID: f.AccountID, AccountGeneration: "1", IssuerDeviceID: device.DeviceID, SubjectDeviceID: deviceC.DeviceID, SubjectSigningPublicKey: cryptox.EncodeBase64(deviceC.SigningPublic), SubjectReceivingPublicKey: cryptox.EncodeBase64(deviceC.ReceivingPublic), EnvironmentID: "dev", KeyVersion: "1", GrantGeneration: "1", Role: "ro", ExpiresAt: strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10), IdempotencyKey: "C-read-v2", Envelope: cryptox.EncodeBase64(envelopeC)}
	signedC, err := cryptox.SignGrant(grantC, private)
	must(err)
	rootAuthorityHash, err := cryptox.IssuerAuthorityHash(cryptox.GrantToWire(signedSelf))
	must(err)
	bAuthorityHash, err := cryptox.IssuerAuthorityHash(cryptox.GrantToWire(signedGrant))
	must(err)
	archivedB := cryptox.EnrollmentApproval{Context: result.Receipt.Approval.Context.EnrollmentContext(), PairingProfile: result.Receipt.Approval.PairingProfile, TranscriptHash: result.Receipt.Approval.TranscriptHash, Grants: result.Receipt.Approval.Grants, ApproverSignature: result.Receipt.Approval.ApproverSignature, InitiatorSignature: result.Receipt.Approval.InitiatorSignature}
	issuerProof := cryptox.IssuerProof{Profile: cryptox.IssuerProofProfile, AccountID: f.AccountID, AccountGeneration: "1", TrustRoot: root, Path: []cryptox.IssuerEnrollment{{CertificateVersion: "1", IssuerProofHash: "", Approval: archivedB}}, Authorities: []cryptox.IssuerAuthority{{Grant: cryptox.GrantToWire(signedSelf)}, {Grant: cryptox.GrantToWire(signedGrant), ParentHash: rootAuthorityHash}}, Targets: []cryptox.IssuerTarget{{EnvironmentID: "dev", AuthorityHash: bAuthorityHash}}}
	v2Approval, err := cryptox.SignEnrollmentApprovalV2(cryptox.EnrollmentApprovalV2{CertificateVersion: "2", Context: statusC.Context.EnrollmentContext(), PairingProfile: pairing.Profile, TranscriptHash: transcriptC, Grants: []cryptox.SignedGrantWire{cryptox.GrantToWire(signedC)}, IssuerProof: issuerProof}, cryptox.PinnedIssuerRoot{AccountID: f.AccountID, AccountGeneration: "1", DeviceID: manager.DeviceID, SigningPublicKey: root.RootSigningPublicKey, ReceivingPublicKey: root.RootReceivingPublicKey}, cryptox.ConfirmedEnrollmentAnchor{Context: statusC.Context.EnrollmentContext(), TranscriptHash: transcriptC}, private, time.Now())
	must(err)
	if got := callJSON(t, httpClient, proxy.URL, base+"/pairings-v2/enroll-C-v2/approve", sessionB.Token, device.DeviceID, "1", map[string]any{"certificateVersion": "2", "capabilities": []string{cryptox.IssuerProofCapability}, "grants": v2Approval.Grants, "transcriptHash": transcriptC, "issuerProof": issuerProof, "signature": v2Approval.ApproverSignature}, &statusC); got != 200 {
		t.Fatalf("B真实PAKE v2批准HTTP %d", got)
	}
	_, err = enrollmentC.Advance(ctx)
	must(err)
	receiptC, err := enrollmentC.Receipt()
	must(err)
	receiptCJSON, err := json.Marshal(receiptC)
	must(err)
	trustC := localkeys.TrustContext{Endpoint: proxy.URL, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: deviceC.DeviceID, SigningPublic: deviceC.SigningPublic, ReceivingPublic: deviceC.ReceivingPublic, PairingProfile: pairing.Profile, CertificateVersion: "2", EnrollmentCertificate: receiptCJSON, EnrollmentKey: receiptC.IdempotencyKey, Accepted: false}
	must(storeC.Vault().SaveTrustContext(trustC))
	loseV2Completion.Store(true)
	if _, err = enrollmentC.Complete(ctx); !errors.Is(err, syncclient.ErrEnrollmentPending) {
		t.Fatal("v2丢接受回应未保留原receipt", err)
	}
	enrollmentC.Close()
	protectedTrustC, err := storeC.Vault().LoadTrustContext()
	must(err)
	if protectedTrustC.Accepted || len(protectedTrustC.Managers) != 0 {
		t.Fatal("v2pending伪完成或保存了全局Managers")
	}
	resumedReceiptC, err := syncclient.DecodeEnrollmentReceiptV2(protectedTrustC.EnrollmentCertificate)
	must(err)
	resumedC, err := syncclient.ResumeEnrollmentV2(configC, resumedReceiptC)
	must(err)
	defer resumedC.Close()
	resultC, err := resumedC.Complete(ctx)
	must(err)
	if resultC.Sequence != 5 {
		t.Fatal("v2原receipt查询成为新接受序号", resultC.Sequence)
	}
	trustC.Accepted = true
	must(storeC.Vault().SaveTrustContext(trustC))
	// 从受保护双签收据重新建立逐环境来源，不能从server目录补pin。
	restartedVerifierC, err := syncclient.NewPinnedVerifierV2(syncclient.IssuerPinnedTrust{AccountID: f.AccountID, AccountGeneration: 1, DeviceID: deviceC.DeviceID, DeviceSigningPublicKey: deviceC.SigningPublic, ReceivingPrivateKey: deviceC.ReceivingPrivate, Receipt: resumedReceiptC})
	must(err)
	defer restartedVerifierC.Close()
	cBoot, err := syncclient.NewForBoot(syncclient.Config{Endpoint: proxy.URL, HTTPClient: httpClient, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: deviceC.DeviceID, Verifier: restartedVerifierC, Engine: engineC})
	must(err)
	clientC, err := cBoot.BootDevice(ctx, privateC)
	must(err)
	_, err = clientC.Pull(ctx)
	must(err)
	must(engineC.Activate("dev", 20, time.Now()))
	effectiveC, err := engineC.Effective(time.Now())
	must(err)
	if effectiveC["HISTORICAL_A"] != "historical-a-synthetic" || effectiveC["HISTORICAL_B"] != "historical-b-synthetic" {
		t.Fatal("只确认B的C未验证A/B两历史来源")
	}
	// 关闭原 Store owner，独立正式 daemon 必须从 v2 加密回执重验，
	// 无登录 session 或 fixture pin；再正常停服务并重开恢复本测试 owner。
	if _, err = storeC.Vault().LoadSession(); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("v2 daemon验收意外留下登录session", err)
	}
	must(storeC.Close())
	bootBeforeV2Daemon := completedBoots.Load()
	exerciseV2DaemonProcess(t, filepath.Join(temporary, "protected-C"), uid, proxy.Certificate(), func() bool { return completedBoots.Load() > bootBeforeV2Daemon })
	storeC, err = localkeys.OpenEncryptedStateStore(localkeys.Config{Directory: filepath.Join(temporary, "protected-C"), UserID: uid})
	must(err)
	defer storeC.Close()
	engineC, err = localstate.New(storeC)
	must(err)
	cBoot, err = syncclient.NewForBoot(syncclient.Config{Endpoint: proxy.URL, HTTPClient: httpClient, AccountID: f.AccountID, AccountGeneration: 1, DeviceID: deviceC.DeviceID, Verifier: restartedVerifierC, Engine: engineC})
	must(err)
	clientC, err = cBoot.BootDevice(ctx, privateC)
	must(err)
	cPacket, err := cryptox.EncryptValue(envKey, cryptox.ValueContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", Name: "RO_FORGED"}, []byte("readonly-can-encrypt-synthetic"))
	must(err)
	roAttempt := cryptox.Mutation{AccountID: f.AccountID, AccountGeneration: "1", DeviceID: deviceC.DeviceID, EnvironmentID: "dev", KeyVersion: "1", GrantGeneration: "1", Operation: "put", IdempotencyKey: "C-ro-denied", Name: "RO_FORGED", Payload: cryptox.EncodeBase64(cPacket)}
	if _, err = clientC.Submit(ctx, wireMutation(t, roAttempt, privateC)); err == nil {
		t.Fatal("RO有环境对称钥却被允许签共享写")
	}
	if engineC.State().Cloud.Sequence != 5 {
		t.Fatal("RO拒写改变已验证序号")
	}
	// A 降 B 权限立即禁止 B 新写，C的既有历史来源仍可以验证。
	downgradedB := grant
	downgradedB.Role = "ro"
	downgradedB.GrantGeneration = "2"
	downgradedB.IdempotencyKey = "B-downgraded"
	downEnvelope, err := cryptox.WrapEnvironmentKey(envKey, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", RecipientType: "device", RecipientID: device.DeviceID, RecipientGeneration: "2", RecipientPublicKey: cryptox.EncodeBase64(device.ReceivingPublic)})
	must(err)
	downgradedB.Envelope = cryptox.EncodeBase64(downEnvelope)
	if got := callJSON(t, httpClient, proxy.URL, base+"/grants", managerSession.Token, manager.DeviceID, "1", wireGrant(t, downgradedB, managerPrivate), nil); got != 200 {
		t.Fatalf("B降权HTTP %d", got)
	}
	bMutation.IdempotencyKey = "B-after-downgrade"
	if _, err = clientB.Submit(ctx, wireMutation(t, bMutation, private)); err == nil {
		t.Fatal("B被降权后旧session仍写入")
	}
	_, err = clientC.Pull(ctx)
	must(err)
	effectiveC, err = engineC.Effective(time.Now())
	must(err)
	if effectiveC["HISTORICAL_A"] != "historical-a-synthetic" || effectiveC["HISTORICAL_B"] != "historical-b-synthetic" {
		t.Fatal("历史公钥因B失去当前Admin被错误删除")
	}
	revokedC := grantC
	revokedC.IssuerDeviceID = manager.DeviceID
	revokedC.GrantGeneration = "2"
	revokedC.Role = "none"
	revokedC.ExpiresAt = "0"
	revokedC.Envelope = ""
	revokedC.IdempotencyKey = "C-revoked-by-A"
	if got := callJSON(t, httpClient, proxy.URL, base+"/grants", managerSession.Token, manager.DeviceID, "1", wireGrant(t, revokedC, managerPrivate), nil); got != 200 {
		t.Fatalf("A撤销C HTTP %d", got)
	}
	_, err = clientC.RefreshAuthorizations(ctx)
	must(err)
	effectiveC, err = engineC.Effective(time.Now())
	must(err)
	if len(effectiveC) != 0 {
		t.Fatal("历史A来源被误当当前授权，撤销后仍生效")
	}
	diskC, err := os.ReadFile(filepath.Join(temporary, "protected-C", "state.v1.enc"))
	must(err)
	if bytes.Contains(diskC, []byte("historical-a-synthetic")) || bytes.Contains(diskC, []byte("historical-b-synthetic")) {
		t.Fatal("v2缓存出现明文")
	}
	t.Log("通过：真实firstroot A→PAKE B Admin→PAKE v2 C RO；B本地根pin预验签，C原封存双签回执未知结果查询重建逐环境来源，真实HPKE/AEAD读取A/B历史；RO造密文拒写、B降权立即拒新写，历史可验但A撤销C后停止生效。没有服务器目录TOFU或全局Managers扩权。")
}

package acceptance

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/syncclient"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"
)

type recoveryChallenge struct {
	ChallengeID        string   `json:"challengeId"`
	Nonce              string   `json:"nonce"`
	ExpiresAt          int64    `json:"expiresAt"`
	RecoveryGeneration string   `json:"recoveryGeneration"`
	SigningPayload     []string `json:"signingPayload"`
}
type recoverySession struct {
	Token            string `json:"token"`
	RotationRequired bool   `json:"rotationRequired"`
}
type recoveryEnvironment struct {
	EnvironmentID string `json:"environmentId"`
	KeyVersion    string `json:"keyVersion"`
	Envelope      string `json:"envelope"`
}
type recoveryVault struct {
	RecoveryGeneration string                `json:"recoveryGeneration"`
	RotationRequired   bool                  `json:"rotationRequired"`
	TrustRoot          cryptox.TrustRoot     `json:"trustRoot"`
	Environments       []recoveryEnvironment `json:"environments"`
	Events             []syncclient.Event    `json:"events"`
}
type rotationProposal struct {
	IdempotencyKey                string                `json:"idempotencyKey"`
	NewRecoveryGeneration         string                `json:"newRecoveryGeneration"`
	NewRecoverySigningPublicKey   string                `json:"newRecoverySigningPublicKey"`
	NewRecoveryReceivingPublicKey string                `json:"newRecoveryReceivingPublicKey"`
	Envelopes                     []recoveryEnvironment `json:"envelopes"`
	NewTrustRoot                  cryptox.TrustRoot     `json:"newTrustRoot"`
}
type rotationView struct {
	State          string   `json:"state"`
	ChallengeID    string   `json:"challengeId"`
	Nonce          string   `json:"nonce"`
	ExpiresAt      int64    `json:"expiresAt"`
	Sequence       uint64   `json:"sequence"`
	Replayed       bool     `json:"replayed"`
	SigningPayload []string `json:"signingPayload"`
}

func shaHex(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func signFields(fields []string, key ed25519.PrivateKey) string {
	data, _ := json.Marshal(fields)
	return cryptox.EncodeBase64(ed25519.Sign(key, data))
}

func TestGoRecoveryCodeReentryAtomicRotation(t *testing.T) {
	seed := bytes.Repeat([]byte{5}, 32)
	recovery, err := cryptox.DeriveRecoveryKeys(seed, "synthetic-account", "1", "1")
	if err != nil {
		t.Fatal(err)
	}
	environmentKey, err := cryptox.GenerateEnvironmentKey()
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := cryptox.WrapEnvironmentKey(environmentKey, cryptox.EnvelopeContext{AccountID: "synthetic-account", AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", RecipientType: "recovery", RecipientID: "synthetic-account", RecipientGeneration: "1", RecipientPublicKey: cryptox.EncodeBase64(recovery.ReceivingPublic)})
	if err != nil {
		t.Fatal(err)
	}
	inputPath := filepath.Join(t.TempDir(), "synthetic-envelopes.json")
	inputs, _ := json.Marshal([]map[string]string{{"environmentId": "dev", "envelope": cryptox.EncodeBase64(envelope)}})
	if err = os.WriteFile(inputPath, inputs, 0600); err != nil {
		t.Fatal(err)
	}
	f := startFixture(t, "--with-trust-root", "--recovery-envelopes", inputPath)
	backend, _ := url.Parse(f.Endpoint)
	proxy := httptest.NewTLSServer(httputil.NewSingleHostReverseProxy(backend))
	defer proxy.Close()
	client := proxy.Client()
	client.Timeout = 15 * time.Second
	base := "/v1/accounts/" + f.AccountID
	if err = cryptox.VerifyTrustRoot(f.AccountID, f.AccountGeneration, f.TrustRoot, recovery.SigningPublic); err != nil {
		t.Fatal("恢复种子未能验证跨语言根声明", err)
	}
	// 用真实设备签名写入密文；不经过任何宿主环境变量。
	var boot recoveryChallenge
	if got := callJSON(t, client, proxy.URL, base+"/boot-challenges", "", "", "", map[string]string{"deviceId": "writer", "accountGeneration": "1"}, &boot); got != 200 {
		t.Fatalf("boot HTTP %d", got)
	}
	expectedBoot := []string{"harmonia/device-boot/v1", f.AccountID, "1", "writer", f.Devices["writer"].SigningPublicKey, f.Devices["writer"].ReceivingPublicKey, boot.ChallengeID, boot.Nonce, strconv.FormatInt(boot.ExpiresAt, 10)}
	if !reflect.DeepEqual(expectedBoot, boot.SigningPayload) {
		t.Fatal("boot未绑定本地已知公钥")
	}
	var writer struct {
		Token string `json:"token"`
	}
	if got := callJSON(t, client, proxy.URL, base+"/boot-sessions", "", "", "", map[string]string{"deviceId": "writer", "accountGeneration": "1", "challengeId": boot.ChallengeID, "signature": signFields(expectedBoot, keyFor(t, f, "writer"))}, &writer); got != 200 {
		t.Fatalf("boot完成 HTTP %d", got)
	}
	packet, err := cryptox.EncryptValue(environmentKey, cryptox.ValueContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", Name: "RECOVERY_SYNTHETIC"}, []byte("synthetic-recoverable-value"))
	if err != nil {
		t.Fatal(err)
	}
	mutation := cryptox.Mutation{AccountID: f.AccountID, AccountGeneration: "1", DeviceID: "writer", EnvironmentID: "dev", KeyVersion: "1", GrantGeneration: "1", Operation: "put", IdempotencyKey: "recovery-e2e-value", Name: "RECOVERY_SYNTHETIC", Payload: cryptox.EncodeBase64(packet)}
	if got := callJSON(t, client, proxy.URL, base+"/mutations", writer.Token, "writer", "1", wireMutation(t, mutation, keyFor(t, f, "writer")), nil); got != 200 {
		t.Fatalf("恢复测试密文写入HTTP %d", got)
	}
	requestRecovery := func(key ed25519.PrivateKey) (string, recoveryChallenge) {
		t.Helper()
		var c recoveryChallenge
		if got := callJSON(t, client, proxy.URL, base+"/recovery-challenges", "", "", "", map[string]string{"accountGeneration": "1"}, &c); got != 200 {
			t.Fatalf("恢复挑战HTTP %d", got)
		}
		expected := []string{"harmonia/recovery-proof/v1", f.AccountID, "1", c.RecoveryGeneration, c.ChallengeID, c.Nonce, strconv.FormatInt(c.ExpiresAt, 10)}
		if !reflect.DeepEqual(expected, c.SigningPayload) || c.ExpiresAt <= time.Now().Unix() || c.ExpiresAt > time.Now().Unix()+125 {
			t.Fatal("恢复挑战上下文不匹配")
		}
		proof := map[string]string{"accountGeneration": "1", "challengeId": c.ChallengeID, "signature": signFields(expected, key)}
		var session recoverySession
		if got := callJSON(t, client, proxy.URL, base+"/recovery-sessions", "", "", "", proof, &session); got != 200 {
			t.Fatalf("恢复持钥证明HTTP %d", got)
		}
		if !session.RotationRequired {
			t.Fatal("恢复成功未处于强制轮换限制")
		}
		if got := callJSON(t, client, proxy.URL, base+"/recovery-sessions", "", "", "", proof, nil); got != 403 {
			t.Fatal("恢复nonce重放未拒绝")
		}
		return session.Token, c
	}
	token, _ := requestRecovery(recovery.SigningPrivate)
	oldOtherToken, _ := requestRecovery(recovery.SigningPrivate)
	if got := callJSON(t, client, proxy.URL, base+"/pull?after=0", token, "writer", "1", nil, nil); got != 401 {
		t.Fatalf("受限恢复token冒用普通device HTTP %d", got)
	}
	var vault recoveryVault
	if got := callJSON(t, client, proxy.URL, base+"/recovery-vault", token, "", "1", nil, &vault); got != 200 {
		t.Fatalf("受限恢复vault HTTP %d", got)
	}
	verifyRecovered := func(v recoveryVault, keys cryptox.RecoveryKeys, expectedGeneration string) {
		t.Helper()
		if v.RecoveryGeneration != expectedGeneration {
			t.Fatal("恢复代际不匹配")
		}
		if err := cryptox.VerifyTrustRoot(f.AccountID, "1", v.TrustRoot, keys.SigningPublic); err != nil {
			t.Fatal("不能信任服务端根公钥", err)
		}
		if len(v.Environments) != 1 || len(v.Events) != 1 {
			t.Fatal("恢复内容缺失")
		}
		e := v.Environments[0]
		packet, err := cryptox.DecodeBase64(e.Envelope, 80, 80)
		if err != nil {
			t.Fatal(err)
		}
		key, err := cryptox.UnwrapEnvironmentKey(keys.ReceivingPrivate, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: e.EnvironmentID, KeyVersion: e.KeyVersion, RecipientType: "recovery", RecipientID: f.AccountID, RecipientGeneration: expectedGeneration, RecipientPublicKey: cryptox.EncodeBase64(keys.ReceivingPublic)}, packet)
		if err != nil || !bytes.Equal(key, environmentKey) {
			t.Fatal("真实恢复HPKE封套解开失败")
		}
		event := v.Events[0]
		if event.Authorization == nil {
			t.Fatal("恢复写入缺少历史签授权")
		}
		g := event.Authorization.Grant
		rootPub, err := cryptox.DecodeBase64(v.TrustRoot.RootSigningPublicKey, 32, 32)
		if err != nil {
			t.Fatal(err)
		}
		if g.IssuerDeviceID != v.TrustRoot.RootDeviceID || g.Role != "rw" || g.SubjectDeviceID != event.Mutation.Mutation.DeviceID || g.GrantGeneration != event.Mutation.Mutation.GrantGeneration {
			t.Fatal("历史writer权限未绑定")
		}
		if err = cryptox.VerifyGrant(cryptox.SignedGrant{Grant: g, Signature: event.Authorization.Signature}, ed25519.PublicKey(rootPub)); err != nil {
			t.Fatal(err)
		}
		pub, err := cryptox.DecodeBase64(g.SubjectSigningPublicKey, 32, 32)
		if err != nil {
			t.Fatal(err)
		}
		m := event.Mutation.Mutation
		if err = cryptox.VerifyMutation(cryptox.SignedMutation{Mutation: m, Signature: event.Mutation.Signature}, ed25519.PublicKey(pub)); err != nil {
			t.Fatal(err)
		}
		ciphertext, err := cryptox.DecodeBase64(m.Payload, 40, 65576)
		if err != nil {
			t.Fatal(err)
		}
		plaintext, err := cryptox.DecryptValue(key, cryptox.ValueContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: e.EnvironmentID, KeyVersion: e.KeyVersion, Name: m.Name}, ciphertext)
		if err != nil || string(plaintext) != "synthetic-recoverable-value" {
			t.Fatal("验签与权限核对后恢复密文失败")
		}
	}
	verifyRecovered(vault, recovery, "1")
	// 模拟完整新码重输：签名私钥只从重输的完整规范码重新派生。
	newSeed, err := cryptox.GenerateRecoverySeed()
	if err != nil {
		t.Fatal(err)
	}
	displayedCode, err := cryptox.EncodeRecoveryCode(newSeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cryptox.DecodeRecoveryCode(displayedCode[:len(displayedCode)-1]); err == nil {
		t.Fatal("不完整新码被接受")
	}
	reenteredSeed, err := cryptox.DecodeRecoveryCode(displayedCode)
	if err != nil {
		t.Fatal(err)
	}
	newKeys, err := cryptox.DeriveRecoveryKeys(reenteredSeed, f.AccountID, "1", "2")
	if err != nil {
		t.Fatal(err)
	}
	newRoot := f.TrustRoot
	newRoot.RecoveryGeneration = "2"
	newRoot.RecoverySigningPublicKey = cryptox.EncodeBase64(newKeys.SigningPublic)
	newRoot.RecoveryReceivingPublicKey = cryptox.EncodeBase64(newKeys.ReceivingPublic)
	newRoot, err = cryptox.SignTrustRoot(f.AccountID, "1", newRoot, newKeys.SigningPrivate)
	if err != nil {
		t.Fatal(err)
	}
	newEnvelope, err := cryptox.WrapEnvironmentKey(environmentKey, cryptox.EnvelopeContext{AccountID: f.AccountID, AccountGeneration: "1", EnvironmentID: "dev", KeyVersion: "1", RecipientType: "recovery", RecipientID: f.AccountID, RecipientGeneration: "2", RecipientPublicKey: cryptox.EncodeBase64(newKeys.ReceivingPublic)})
	if err != nil {
		t.Fatal(err)
	}
	proposal := rotationProposal{IdempotencyKey: "recovery-rotation-e2e", NewRecoveryGeneration: "2", NewRecoverySigningPublicKey: cryptox.EncodeBase64(newKeys.SigningPublic), NewRecoveryReceivingPublicKey: cryptox.EncodeBase64(newKeys.ReceivingPublic), Envelopes: []recoveryEnvironment{{"dev", "1", cryptox.EncodeBase64(newEnvelope)}}, NewTrustRoot: newRoot}
	incomplete := proposal
	incomplete.IdempotencyKey = "incomplete-recovery-e2e"
	incomplete.Envelopes = []recoveryEnvironment{}
	if got := callJSON(t, client, proxy.URL, base+"/recovery-rotations", token, "", "1", incomplete, nil); got != 409 {
		t.Fatalf("遗漏环境封套应拒绝HTTP %d", got)
	}
	var c rotationView
	if got := callJSON(t, client, proxy.URL, base+"/recovery-rotations", token, "", "1", proposal, &c); got != 200 {
		t.Fatalf("轮换初始化HTTP %d", got)
	}
	envelopeItems, _ := json.Marshal([][]string{{"dev", "1", cryptox.EncodeBase64(newEnvelope)}})
	rootBytes, _ := newRoot.SigningBytes(f.AccountID, "1")
	var rootFields []string
	_ = json.Unmarshal(rootBytes, &rootFields)
	rootHashBytes, _ := json.Marshal(append(rootFields, newRoot.Signature))
	expected := []string{"harmonia/recovery-rotation/v1", f.AccountID, "1", shaHex([]byte(token)), "1", c.ChallengeID, c.Nonce, strconv.FormatInt(c.ExpiresAt, 10), "2", proposal.NewRecoverySigningPublicKey, proposal.NewRecoveryReceivingPublicKey, shaHex(envelopeItems), shaHex(rootHashBytes)}
	if !reflect.DeepEqual(expected, c.SigningPayload) {
		t.Fatal("轮换挑战未绑定完整新公钥、根声明与全部封套")
	}
	path := base + "/recovery-rotations/" + proposal.IdempotencyKey
	if got := callJSON(t, client, proxy.URL, path+"/complete", token, "", "1", map[string]string{"challengeId": c.ChallengeID, "signature": signFields(expected, recovery.SigningPrivate)}, nil); got != 403 {
		t.Fatal("旧码签名被当作新码重输证明")
	}
	if got := callJSON(t, client, proxy.URL, base+"/recovery-vault", token, "", "1", nil, &vault); got != 200 {
		t.Fatal("失败轮换破坏旧vault")
	}
	verifyRecovered(vault, recovery, "1")
	proof := map[string]string{"challengeId": c.ChallengeID, "signature": signFields(expected, newKeys.SigningPrivate)}
	var completed rotationView
	if got := callJSON(t, client, proxy.URL, path+"/complete", token, "", "1", proof, &completed); got != 200 || completed.State != "complete" {
		t.Fatalf("原子轮换完成HTTP %d", got)
	}
	var status rotationView
	if got := callJSON(t, client, proxy.URL, path, token, "", "1", nil, &status); got != 200 || status.State != "complete" || status.Sequence != completed.Sequence {
		t.Fatal("断网结果不明查询未返回持久完成状态")
	}
	var retry rotationView
	if got := callJSON(t, client, proxy.URL, path+"/complete", token, "", "1", proof, &retry); got != 200 || !retry.Replayed || retry.Sequence != completed.Sequence {
		t.Fatal("相同轮换重试产生新写")
	}
	if got := callJSON(t, client, proxy.URL, base+"/recovery-vault", oldOtherToken, "", "1", nil, nil); got != 401 {
		t.Fatalf("旧受限恢复会话未失效HTTP %d", got)
	}
	if got := callJSON(t, client, proxy.URL, base+"/recovery-vault", token, "", "1", nil, &vault); got != 200 || vault.RotationRequired {
		t.Fatal("轮换后恢复能力状态错误")
	}
	verifyRecovered(vault, newKeys, "2")
	if got := callJSON(t, client, proxy.URL, base+"/pull?after=0", token, "writer", "1", nil, nil); got != 401 {
		t.Fatal("轮换不应偷偷注册可信设备")
	}
	var oldChallenge recoveryChallenge
	if got := callJSON(t, client, proxy.URL, base+"/recovery-challenges", "", "", "", map[string]string{"accountGeneration": "1"}, &oldChallenge); got != 200 {
		t.Fatal("轮换后挑战失败")
	}
	oldFields := []string{"harmonia/recovery-proof/v1", f.AccountID, "1", "2", oldChallenge.ChallengeID, oldChallenge.Nonce, strconv.FormatInt(oldChallenge.ExpiresAt, 10)}
	if got := callJSON(t, client, proxy.URL, base+"/recovery-sessions", "", "", "", map[string]string{"accountGeneration": "1", "challengeId": oldChallenge.ChallengeID, "signature": signFields(oldFields, recovery.SigningPrivate)}, nil); got != 403 {
		t.Fatal("旧码仍可恢复新代vault")
	}
	t.Log("通过：真实Go恢复HPKE/签授权/AEAD解密，完整新码重输签一次nonce，TS SQLite原子替换全部封套和恢复根；旧码/旧会话失效，状态查询与幂等重试。")
}

package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

// 这个helper只接高层真正完成的原生恢复记录；不从fixture目录构造受信设备。
func recoveredCLIActor(t *testing.T, f *mobileManagerFixture, e *mobileManagerActor) (*syncclient.Client, *syncclient.PinnedVerifier, string) {
	t.Helper()
	var native struct {
		AccountID  string           `json:"accountId"`
		Generation string           `json:"accountGeneration"`
		DeviceID   string           `json:"deviceId"`
		Cloud      localstate.State `json:"cloud"`
		Recovered  *struct {
			Pin         cryptox.PinnedIssuerRoot             `json:"pin"`
			Original    cryptox.OriginalInitialization       `json:"originalInitialization"`
			Transitions []cryptox.AcceptedRecoveryTransition `json:"transitions"`
			Packet      cryptox.RecoveredDeviceSubmission    `json:"packet"`
			Sequence    uint64                               `json:"acceptedSequence"`
			Applied     bool                                 `json:"applied"`
		} `json:"recoveredDevice"`
	}
	data := e.load()
	originMust(t, json.Unmarshal(data, &native))
	clear(data)
	if native.Recovered == nil || !native.Recovered.Applied || native.Recovered.Sequence == 0 {
		t.Fatal("actual recovered high-level device not trusted yet")
	}
	r := native.Recovered
	accepted := cryptox.AcceptedRecoveredDevice{Submission: r.Packet, Sequence: r.Sequence}
	proof := environmentValue(cryptox.BuildRecoveredDeviceIssuerEvidence(r.Pin, r.Original, r.Transitions, accepted))
	verifier := environmentValue(syncclient.NewRecoveredDevicePinnedVerifier(syncclient.RecoveredDevicePinnedTrust{Trust: syncclient.PinnedTrust{AccountID: native.AccountID, AccountGeneration: environmentValue(strconv.ParseUint(native.Generation, 10, 64)), DeviceID: native.DeviceID, DeviceSigningPublicKey: e.config.SigningKey.Public().(ed25519.PublicKey), ReceivingPrivateKey: e.config.ReceivingPrivateKey}, Pin: r.Pin, Evidence: proof, Accepted: accepted}))
	t.Cleanup(verifier.Close)
	engine := environmentValue(localstate.New(&originMemoryStore{state: native.Cloud}))
	originMust(t, verifier.ValidateStoredIssuerEvidence(engine.State().Cloud))
	client := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: native.AccountID, AccountGeneration: 1, DeviceID: native.DeviceID, Verifier: verifier, Engine: engine}))
	client = environmentValue(client.BootDevice(context.Background(), e.config.SigningKey))
	_ = environmentValue(client.Pull(context.Background()))
	return client, verifier, native.DeviceID
}

func buildRecoveryCLI4(t *testing.T, directory string, native bool) string {
	t.Helper()
	name := "harmonia-cert4-daemon"
	if native {
		name = "harmonia-cert4-pair"
	}
	binary := filepath.Join(directory, name)
	goEnv := exec.Command("go", "env", "GOCACHE", "GOMODCACHE", "GOPATH")
	goEnv.Env = []string{"PATH=" + os.Getenv("PATH")}
	for _, name := range []string{"HOME", "USERPROFILE", "GOCACHE", "GOMODCACHE", "GOPATH"} {
		if value := os.Getenv(name); value != "" {
			goEnv.Env = append(goEnv.Env, name+"="+value)
		}
	}
	caches := strings.Split(strings.TrimSpace(string(environmentValue(goEnv.Output()))), "\n")
	if len(caches) != 3 {
		t.Fatal("Go cache tool result invalid")
	}
	args := []string{"build", "-o", binary, "./cmd/harmonia"}
	cgo := "0"
	if native {
		args = []string{"build", "-tags", "harmonia_boringssl", "-o", binary, "./cmd/harmonia"}
		cgo = "1"
	}
	build := exec.Command("go", args...)
	build.Dir = "../core-go"
	build.Env = []string{"PATH=" + os.Getenv("PATH"), "CGO_ENABLED=" + cgo, "GOCACHE=" + caches[0], "GOMODCACHE=" + caches[1], "GOPATH=" + caches[2]}
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compiled native cert4 CLI failed: %v\n%s", err, output)
	}
	return binary
}

// 正式程序登录/本机短码/真实PAKE/原收据重试/daemon全程只使用合成账号和临时目录。
func actualRecoveryCLIPairAndDaemon(t *testing.T, f *mobileManagerFixture, e *mobileManagerActor, environment, role string, lose bool, approve ...func(context.Context, string, []byte)) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	manager, _, managerID := recoveredCLIActor(t, f, e)
	uid := environmentValue(localkeys.CurrentUserID())
	base := environmentValue(os.MkdirTemp("/tmp", "h4-"))
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	private := environmentValue(filepath.EvalSymlinks(base))
	directory := filepath.Join(private, "device")
	ca := filepath.Join(private, "synthetic-ca.pem")
	originMust(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.proxy.Certificate().Raw}), 0600))
	binary := buildRecoveryCLI4(t, private, true)
	localArgs := []string{"--local-directory", directory, "--local-user", uid, "--ca-file", ca}
	run := func(command, input string, args ...string) ([]byte, error) {
		request := exec.CommandContext(ctx, binary, append(append([]string{command}, localArgs...), args...)...)
		request.Env = []string{"PATH=" + os.Getenv("PATH")}
		request.Stdin = strings.NewReader(input)
		return request.CombinedOutput()
	}
	if _, err := run("login", f.password+"\n", "--server", f.proxy.URL, "--email", f.email, "--password-stdin"); err != nil {
		t.Fatal("actual compiled cert4 CLI login failed", err)
	}
	var lost atomic.Bool
	lost.Store(lose)
	var completes, boots atomic.Int64
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/boot-sessions") {
			boots.Add(1)
		}
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.Contains(r.Request.URL.Path, "/pairings-v4/") && strings.HasSuffix(r.Request.URL.Path, "/complete") {
			completes.Add(1)
			if lost.Swap(false) {
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
	defer f.responseHook.Store(nil)
	pair := exec.CommandContext(ctx, binary, append(append([]string{"pair"}, localArgs...), "--certificate-version", "4", "--approver", managerID)...)
	pair.Env = []string{"PATH=" + os.Getenv("PATH")}
	pipe := environmentValue(pair.StdoutPipe())
	var stderr bytes.Buffer
	pair.Stderr = &stderr
	originMust(t, pair.Start())
	type otp struct {
		id   string
		code []byte
	}
	ready := make(chan otp, 1)
	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		scanner := bufio.NewScanner(pipe)
		id := ""
		sent := false
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "配对申请：") {
				id = strings.TrimSpace(strings.TrimPrefix(line, "配对申请："))
			}
			if !sent && id != "" && strings.HasPrefix(line, "请在管理手机输入本机短码：") {
				code := []byte(strings.TrimSpace(strings.TrimPrefix(line, "请在管理手机输入本机短码：")))
				if len(code) == 8 {
					ready <- otp{id, code}
					sent = true
				}
			}
		}
	}()
	var local otp
	select {
	case local = <-ready:
	case <-ctx.Done():
		_ = pair.Process.Kill()
		_ = pair.Wait()
		t.Fatal("actual compiled cert4 CLI never created native PAKE")
	}
	defer clear(local.code)
	if len(approve) > 1 || len(approve) == 1 && approve[0] == nil {
		t.Fatal("invalid formal CLI4 approval callback")
	}
	if len(approve) == 1 {
		approve[0](ctx, local.id, local.code)
	} else {
		approver := environmentValue(manager.NewApproverV4(local.id, e.config.SigningKey))
		defer approver.Close()
		confirmed := environmentValue(approver.Confirm(ctx, local.code))
		proof := environmentValue(manager.CurrentIssuerRecoveryEvidence())
		var own cryptox.SignedGrantWire
		for _, target := range proof.Targets {
			if target.EnvironmentID == environment {
				for _, node := range proof.Authorities {
					h := environmentValue(cryptox.IssuerAuthorityHash(node.Grant))
					if h == target.AuthorityHash {
						own = node.Grant
					}
				}
			}
		}
		if own.Grant.Role != "admin" {
			t.Fatal("recovered manager lacks exact selected current Admin")
		}
		g := own.Grant
		packet := environmentValue(cryptox.DecodeBase64(g.Envelope, 80, 80))
		key := environmentValue(cryptox.UnwrapEnvironmentKey(e.config.ReceivingPrivateKey, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: g.EnvironmentID, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: g.SubjectDeviceID, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey}, packet))
		defer clear(key)
		g.IssuerDeviceID = managerID
		g.SubjectDeviceID = confirmed.Context.InitiatorDeviceID
		g.SubjectSigningPublicKey = confirmed.Context.InitiatorSigningPublicKey
		g.SubjectReceivingPublicKey = confirmed.Context.InitiatorReceivingPublicKey
		g.GrantGeneration = "1"
		g.Role = role
		g.IdempotencyKey = local.id + "-grant"
		envelope := environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: g.EnvironmentID, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: g.SubjectDeviceID, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey}))
		g.Envelope = cryptox.EncodeBase64(envelope)
		signed := environmentValue(cryptox.SignGrant(g, e.config.SigningKey))
		approval := environmentValue(approver.PrepareApproval([]cryptox.SignedGrantWire{cryptox.GrantToWire(signed)}))
		seal, load := recoveryNativeSeal(t, "synthetic-native-cert4-manager-original")
		originMust(t, seal(environmentValue(json.Marshal(approval))))
		var original cryptox.EnrollmentApprovalV4
		data := load()
		originMust(t, json.Unmarshal(data, &original))
		clear(data)
		_ = environmentValue(approver.Submit(ctx, original))
	}
	<-scanDone
	pairErr := pair.Wait()
	if lose && pairErr == nil || !lose && pairErr != nil {
		t.Fatal("actual compiled pair result mismatch", pairErr)
	}
	if completes.Load() != 1 {
		t.Fatal("actual cert4 dual-signed completion not submitted exactly once")
	}
	if lose {
		// 两次访问原加密receipt，不能以当前同名授权猜接受，也不能重生成短码。
		vault := environmentValue(localkeys.Open(localkeys.Config{Directory: directory, UserID: uid}))
		trust := environmentValue(vault.LoadTrustContext())
		if trust.Accepted || trust.CertificateVersion != "4" {
			t.Fatal("unknown accepted response reported trusted")
		}
		raw := bytes.Clone(trust.EnrollmentCertificate)
		originMust(t, vault.Close())
		if _, err := run("pair", "", "--certificate-version", "4"); err != nil {
			t.Fatal("compiled cert4 original receipt resume failed", err)
		}
		vault = environmentValue(localkeys.Open(localkeys.Config{Directory: directory, UserID: uid}))
		trust = environmentValue(vault.LoadTrustContext())
		if !trust.Accepted || !bytes.Equal(raw, trust.EnrollmentCertificate) || completes.Load() != 1 {
			t.Fatal("compiled cert4 resume changed original receipt or rewrote completion")
		}
		clear(raw)
		originMust(t, vault.Close())
	}
	// 断开所有CLI后启动普通CGO0后台；原session-v1由pair删除，必须持钥boot。
	binary = buildRecoveryCLI4(t, private, false)
	log := environmentValue(os.OpenFile(filepath.Join(private, "daemon.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600))
	defer log.Close()
	before := boots.Load()
	daemon := exec.CommandContext(ctx, binary, append(append([]string{"daemon"}, localArgs...), "--platform-fragment", filepath.Join(directory, "environment.sh"), "--interval", "20ms", "--sync-interval", "1s")...)
	daemon.Env = []string{"PATH=" + os.Getenv("PATH")}
	daemon.Stdout = io.Discard
	daemon.Stderr = log
	originMust(t, daemon.Start())
	done := make(chan error, 1)
	go func() { done <- daemon.Wait() }()
	defer func() {
		_ = daemon.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			originMust(t, err)
		case <-time.After(5 * time.Second):
			_ = daemon.Process.Kill()
			<-done
			t.Error("actual cert4 daemon did not stop normally")
		}
	}()
	readyDaemon := false
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		b, err := run("status", "")
		var status localipc.Status
		if err == nil && json.Unmarshal(b, &status) == nil && status.Environments == 1 && status.Sequence > 0 && boots.Load() > before {
			readyDaemon = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !readyDaemon {
		b, _ := os.ReadFile(log.Name())
		classes := []string{}
		for _, label := range []string{"evidence", "session", "binding", "identity", "permission", "grant", "checkpoint", "source", "signature", "fragment", "corrupt", "protocol", "invalid", "ledger", "locked", "owner", "generation", "native", "unavailable", "v4", "closed", "type", "mapping"} {
			if bytes.Contains(bytes.ToLower(b), []byte(label)) {
				classes = append(classes, label)
			}
		}
		t.Fatalf("actual cert4 daemon not ready: fixedClasses=%v bootSucceeded=%t", classes, boots.Load() > before)
	}
	if _, err := run("activate", "", "--environment", environment, "--priority", "10"); err != nil {
		t.Fatal("compiled cert4 activate IPC failed", err)
	}
	output, err := run("export", "")
	originMust(t, err)
	if len(output) == 0 {
		t.Fatal("compiled cert4 trusted read produced no synthetic values")
	}
	_, err = run("put", "synthetic-cert4-CLI-written", "--environment", environment, "--name", "SYNTHETIC_CERT4_WRITTEN", "--value-stdin", "--request-id", "cert4-shared-write")
	if role == "ro" {
		if err == nil {
			t.Fatal("compiled RO cert4 device wrote shared data")
		}
		return
	}
	originMust(t, err)
	output, err = run("export", "")
	originMust(t, err)
	if !bytes.Contains(output, []byte("SYNTHETIC_CERT4_WRITTEN='synthetic-cert4-CLI-written'")) {
		t.Fatal("compiled RW cert4 write bypassed or failed common pull")
	}
	if _, err = run("delete", "", "--environment", environment, "--name", "SYNTHETIC_CERT4_WRITTEN", "--request-id", "cert4-shared-delete"); err != nil {
		t.Fatal("compiled RW cert4 delete failed", err)
	}
	output, err = run("export", "")
	originMust(t, err)
	if bytes.Contains(output, []byte("SYNTHETIC_CERT4_WRITTEN")) {
		t.Fatal("compiled delete retained managed source")
	}
}

// 高层实际登记E后批准两个独立CLI；所有包/会话均来自真实业务，不seed可信目录。
func TestNativeRecoveredManagerApprovesFormalCLI4(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("真实SPAKE2未包含于默认构建，需固定BoringSSL原生tag")
	}
	f := newMobileManagerFixture(t)
	e := newMobileManagerActor(t, f)
	ctx := context.Background()
	originMust(t, e.workflow.Login(ctx, f.email, f.password))
	old, _, err := e.workflow.BeginRecoveryAuthoritySession(ctx, f.code)
	originMust(t, err)
	defer old.Close()
	code := environmentValue(e.workflow.BeginRecoveryAuthorityTransition(ctx, "cert4-original-authority-transition"))
	fresh, info, err := e.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
	code = ""
	originMust(t, err)
	defer fresh.Close()
	if info.TrustedDevice {
		t.Fatal("rotation promoted code holder to trusted device before explicit enrollment")
	}
	registered := environmentValue(e.workflow.RegisterRecoveredDevice(ctx, "cert4-recovered-manager", []mobileworkflow.RecoveryDeviceSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: "0"}}))
	_ = registered
	actualRecoveryCLIPairAndDaemon(t, f, e, f.initial, "ro", true)
	actualRecoveryCLIPairAndDaemon(t, f, e, f.initial, "rw", false)
	t.Log("真实连续恢复E→原生SPAKE2 CLI4双签→504原收据恢复→纯Go daemon持钥启动→RO拒写/RW在线写删通过")
}

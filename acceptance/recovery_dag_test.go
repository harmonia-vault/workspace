//go:build harmonia_boringssl

package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"encoding/pem"
	"errors"
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
	"github.com/harmonia-vault/core-go/syncclient"
)

// 仅本测试的原生加密回调Store；不开放正式客户端的入网或信任门槛。
type dagAcceptanceStore struct{ vault *localkeys.Vault }

func (s *dagAcceptanceStore) Load() (localstate.State, error) {
	b, e := s.vault.Load("state-v1")
	if errors.Is(e, os.ErrNotExist) {
		return localstate.EmptyState(), nil
	}
	if e != nil {
		return localstate.State{}, e
	}
	defer clear(b)
	var out localstate.State
	e = json.Unmarshal(b, &out)
	return out, e
}
func (s *dagAcceptanceStore) Save(out localstate.State) error {
	b, e := json.Marshal(out)
	if e != nil {
		return e
	}
	defer clear(b)
	return s.vault.Save("state-v1", b)
}

type dagAcceptanceWriteJournal struct{ vault *localkeys.Vault }

func (j dagAcceptanceWriteJournal) Load() ([]byte, error) { return j.vault.Load("writes-v1") }
func (j dagAcceptanceWriteJournal) Save(b []byte) error   { return j.vault.Save("writes-v1", b) }

type dagAcceptanceActor struct {
	client    *syncclient.Client
	verifier  *syncclient.PinnedVerifier
	engine    *localstate.Engine
	key       ed25519.PrivateKey
	receiving []byte
	id        string
	vault     *localkeys.Vault
	result    syncclient.DAGRecoveredResult
}

func newDAGLabVault(t *testing.T) *localkeys.Vault {
	t.Helper()
	base := environmentValue(filepath.EvalSymlinks(t.TempDir()))
	uid := environmentValue(localkeys.CurrentUserID())
	v := environmentValue(localkeys.Open(localkeys.Config{Directory: filepath.Join(base, "owner"), UserID: uid}))
	t.Cleanup(func() { _ = v.Close() })
	return v
}
func dagStage(t *testing.T, name string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("DAG actual stage %s failed: %v", name, err)
	}
	t.Log("DAG actual stage", name, "PASS")
}
func recoverDAGActor(t *testing.T, ctx context.Context, f *mobileManagerFixture, account, oldCode, id string, lose bool) (*dagAcceptanceActor, string) {
	t.Helper()
	v := newDAGLabVault(t)
	j := environmentValue(syncclient.NewVaultDAGJournal(v, f.proxy.URL, account, 1))
	session := environmentValue(syncclient.OpenDAGRecoverySession(ctx, syncclient.DAGRecoveryConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, Journal: j}, oldCode))
	defer session.Close()
	info := environmentValue(session.Info())
	if info.TrustedDevice || !info.RotationRequired {
		t.Fatal("fresh recovery session escaped restricted gate")
	}
	code := environmentValue(session.BeginTransition(ctx, id+"-transition"))
	_, err := session.SealTransition(ctx, code)
	dagStage(t, id+" seal new code", err)
	var lost atomic.Bool
	lost.Store(lose)
	var posts atomic.Int64
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovery-authority-transitions-v2") {
			posts.Add(1)
			if r.StatusCode == 200 && lost.Swap(false) {
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
	info, err = session.RetryTransition(ctx)
	if lose {
		if !errors.Is(err, syncclient.ErrEnrollmentPending) {
			t.Fatalf("unknown transition result expected pending: %v", err)
		}
		original := environmentValue(j.Load())
		receipt := environmentValue(session.QueryOriginalOperation(ctx, original))
		if !receipt.Accepted || receipt.ContentHash != original.ContentHash {
			t.Fatal("unknown transition did not resolve original hash")
		}
		fresh, openErr := syncclient.OpenDAGRecoverySession(ctx, syncclient.DAGRecoveryConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, Journal: j}, code)
		dagStage(t, id+" new RAM session loads original unknown journal", openErr)
		saved := environmentValue(fresh.PendingOperation())
		if saved.OperationID != original.OperationID || saved.ContentHash != original.ContentHash {
			t.Fatal("restart changed original operation")
		}
		if _, err = fresh.BeginTransition(ctx, "must-not-replace-unknown"); !errors.Is(err, syncclient.ErrDAGRecoveryState) {
			t.Fatal("unknown old journal allowed a new nonce/operation")
		}
		fresh.Close()
		info, err = session.RetryTransition(ctx)
	}
	f.responseHook.Store(nil)
	dagStage(t, id+" transition atomic/receipt", err)
	if posts.Load() != 1 || info.RotationRequired || info.TrustedDevice {
		t.Fatal("transition retried as new write or granted trusted device")
	}
	if lose {
		if old, err := syncclient.OpenDAGRecoverySession(ctx, syncclient.DAGRecoveryConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, Journal: j}, oldCode); err == nil {
			old.Close()
			t.Fatal("old code survived atomic public key/envelope switch")
		}
		fresh := environmentValue(syncclient.OpenDAGRecoverySession(ctx, syncclient.DAGRecoveryConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, Journal: j}, code))
		receipt := environmentValue(fresh.ResolveOriginalOperation(ctx))
		if !receipt.Accepted || receipt.ContentHash != environmentValue(j.Load()).ContentHash {
			t.Fatal("fresh session failed original receipt resolution")
		}
		restricted := environmentValue(fresh.Info())
		if !restricted.RotationRequired || restricted.TrustedDevice {
			t.Fatal("old receipt unlocked a new restricted session")
		}
		fresh.Close()
	}
	keys := environmentValue(localkeys.GenerateDeviceKeys(id))
	originMust(t, v.SaveDeviceKeys(keys))
	key := ed25519.NewKeyFromSeed(keys.SigningSeed)
	t.Cleanup(func() { clear(key) })
	_, err = session.SealRecoveredDevice(ctx, id+"-register", id, key, keys.ReceivingPrivate, []cryptox.RecoveredDeviceRight{{EnvironmentID: f.initial, KeyVersion: "1", Role: "admin", ExpiresAt: "0"}})
	dagStage(t, id+" explicit rights/HPKE countersign", err)
	result, err := session.RetryRecoveredDevice(ctx)
	dagStage(t, id+" accepted original device packet", err)
	verifier := environmentValue(syncclient.NewRecoveredDAGPinnedVerifier(syncclient.RecoveredDAGPinnedTrust{Trust: syncclient.PinnedTrust{AccountID: account, AccountGeneration: 1, DeviceID: id, DeviceSigningPublicKey: keys.SigningPublic, ReceivingPrivateKey: keys.ReceivingPrivate}, Pin: result.Pin, Evidence: result.Evidence, Accepted: result.Accepted}))
	t.Cleanup(verifier.Close)
	store := &dagAcceptanceStore{vault: v}
	engine := environmentValue(localstate.New(store))
	client := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, DeviceID: id, Verifier: verifier, Engine: engine}))
	client, err = client.BootDevice(ctx, key)
	dagStage(t, id+" device-bound boot", err)
	_, err = client.Pull(ctx)
	dagStage(t, id+" verified P4 pull/final native save", err)
	if engine.State().Cloud.Environments[f.initial].Values["SYNTHETIC_X"] != "synthetic-x-value" {
		t.Fatal("actual recovered device failed HPKE/AEAD plaintext")
	}
	loaded := environmentValue(store.Load())
	originMust(t, verifier.ValidateStoredIssuerEvidence(loaded.Cloud))
	engine = environmentValue(localstate.New(store))
	client = environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, DeviceID: id, Verifier: verifier, Engine: engine}))
	client = environmentValue(client.BootDevice(ctx, key))
	_ = environmentValue(client.Pull(ctx))
	dagStage(t, id+" protected-state restart reverify", nil)
	return &dagAcceptanceActor{client: client, verifier: verifier, engine: engine, key: key, receiving: keys.ReceivingPrivate, id: id, vault: v, result: result}, code
}

func TestNativeRepeatedRecoveryDAGAndFormalCLI5(t *testing.T) {
	f := newMobileManagerFixture(t)
	var identity struct {
		AccountID string `json:"accountId"`
	}
	raw := f.rootActor.load()
	originMust(t, json.Unmarshal(raw, &identity))
	clear(raw)
	account := identity.AccountID
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	b, codeB := recoverDAGActor(t, ctx, f, account, f.code, "dag-recovered-B", true)
	// 旧管理设备全部已丢失的主恢复语义不依赖它们在线；本轮先关闭本机旧controller。
	f.root.Close()
	b.verifier.Close()
	c, codeC := recoverDAGActor(t, ctx, f, account, codeB, "dag-recovered-C", false)
	if codeB == codeC {
		t.Fatal("second recovery reused first code")
	}
	writer := environmentValue(syncclient.NewWriter(account, 1, c.id, c.engine.State().SessionEpoch, c.key, dagAcceptanceWriteJournal{c.vault}))
	defer writer.Close()
	result, err := writer.Execute(ctx, c.client, syncclient.WriteRequest{ID: "dag-C-shared-write", Operation: "put", EnvironmentID: f.initial, Name: "SYNTHETIC_DAG_C", Value: "synthetic-dag-c"})
	dagStage(t, "C shared write via receipt and same pull", err)
	if !result.Applied || c.engine.State().Cloud.Environments[f.initial].Values["SYNTHETIC_DAG_C"] != "synthetic-dag-c" {
		t.Fatal("C write did not apply via verified data flow")
	}
	for _, role := range []string{"ro", "rw"} {
		t.Run(role, func(t *testing.T) { actualDAGCLI5(t, ctx, f, c, role, role == "ro") })
	}
	// 唯一Vault owner关闭后，RAM会话不可继续使用旧身份或重写原journal。
	v := newDAGLabVault(t)
	journal := environmentValue(syncclient.NewVaultDAGJournal(v, f.proxy.URL, account, 1))
	s := environmentValue(syncclient.OpenDAGRecoverySession(ctx, syncclient.DAGRecoveryConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: account, AccountGeneration: 1, Journal: journal}, codeC))
	originMust(t, v.Close())
	if _, err = s.Info(); !errors.Is(err, syncclient.ErrDAGRecoveryState) {
		t.Fatal("closed native owner retained recovery authority")
	}
	s.Close()
}

func actualDAGCLI5(t *testing.T, parent context.Context, f *mobileManagerFixture, c *dagAcceptanceActor, role string, lose bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(parent, 35*time.Second)
	defer cancel()
	base := environmentValue(os.MkdirTemp("/tmp", "h5-"))
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	base = environmentValue(filepath.EvalSymlinks(base))
	directory := filepath.Join(base, "device")
	uid := environmentValue(localkeys.CurrentUserID())
	ca := filepath.Join(base, "ca.pem")
	originMust(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.proxy.Certificate().Raw}), 0600))
	binary := buildRecoveryCLI4(t, base, true)
	args := []string{"--local-directory", directory, "--local-user", uid, "--ca-file", ca}
	run := func(command, input string, extra ...string) ([]byte, error) {
		p := exec.CommandContext(ctx, binary, append(append([]string{command}, args...), extra...)...)
		p.Env = []string{"PATH=" + os.Getenv("PATH")}
		p.Stdin = strings.NewReader(input)
		return p.CombinedOutput()
	}
	_, err := run("login", f.password+"\n", "--server", f.proxy.URL, "--email", f.email, "--password-stdin")
	dagStage(t, "formal CLI5 login", err)
	var lost atomic.Bool
	lost.Store(lose)
	var completions, boots atomic.Int64
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/boot-sessions") {
			boots.Add(1)
		}
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.Contains(r.Request.URL.Path, "/pairings-v5/") && strings.HasSuffix(r.Request.URL.Path, "/complete") {
			completions.Add(1)
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
	defer f.responseHook.Store(nil)
	pair := exec.CommandContext(ctx, binary, append(append([]string{"pair"}, args...), "--certificate-version", "5", "--approver", c.id)...)
	pair.Env = []string{"PATH=" + os.Getenv("PATH")}
	stdout := environmentValue(pair.StdoutPipe())
	pair.Stderr = io.Discard
	originMust(t, pair.Start())
	type otp struct {
		id   string
		code []byte
	}
	ready := make(chan otp, 1)
	go func() {
		scan := bufio.NewScanner(stdout)
		id := ""
		sent := false
		for scan.Scan() {
			line := scan.Text()
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
		t.Fatal("formal CLI5 native PAKE not ready")
	}
	defer clear(local.code)
	approver := environmentValue(c.client.NewApproverV5(local.id, c.key))
	defer approver.Close()
	confirmed := environmentValue(approver.Confirm(ctx, local.code))
	proof := environmentValue(c.client.CurrentIssuerDAGEvidence())
	var own cryptox.SignedGrantWire
	for _, target := range proof.Source.View.Targets {
		if target.EnvironmentID == f.initial {
			for _, node := range proof.Source.View.Authorities {
				if environmentValue(cryptox.IssuerAuthorityHash(node.Grant)) == target.AuthorityHash {
					own = node.Grant
				}
			}
		}
	}
	if own.Grant.Role != "admin" {
		t.Fatal("C lacks exact current Admin")
	}
	g := own.Grant
	packet := environmentValue(cryptox.DecodeBase64(g.Envelope, 80, 80))
	key := environmentValue(cryptox.UnwrapEnvironmentKey(c.receiving, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: g.EnvironmentID, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: g.SubjectDeviceID, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey}, packet))
	defer clear(key)
	g.IssuerDeviceID = c.id
	g.SubjectDeviceID = confirmed.Context.InitiatorDeviceID
	g.SubjectSigningPublicKey = confirmed.Context.InitiatorSigningPublicKey
	g.SubjectReceivingPublicKey = confirmed.Context.InitiatorReceivingPublicKey
	g.GrantGeneration = "1"
	g.Role = role
	g.ExpiresAt = strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	g.IdempotencyKey = local.id + "-grant"
	wrapped := environmentValue(cryptox.WrapEnvironmentKey(key, cryptox.EnvelopeContext{AccountID: g.AccountID, AccountGeneration: g.AccountGeneration, EnvironmentID: g.EnvironmentID, KeyVersion: g.KeyVersion, RecipientType: "device", RecipientID: g.SubjectDeviceID, RecipientGeneration: g.GrantGeneration, RecipientPublicKey: g.SubjectReceivingPublicKey}))
	g.Envelope = cryptox.EncodeBase64(wrapped)
	grant := environmentValue(cryptox.SignGrant(g, c.key))
	approval := environmentValue(approver.PrepareApproval([]cryptox.SignedGrantWire{cryptox.GrantToWire(grant)}))
	_ = environmentValue(approver.Submit(ctx, approval))
	err = pair.Wait()
	if lose && err == nil || !lose && err != nil {
		t.Fatalf("formal CLI5 completion result mismatch: %v", err)
	}
	if completions.Load() != 1 {
		t.Fatal("CLI5 completion count mismatch")
	}
	if lose {
		_, err = run("pair", "", "--certificate-version", "5")
		dagStage(t, "CLI5 original accepted receipt resume", err)
		if completions.Load() != 1 {
			t.Fatal("CLI5 lost response caused new completion")
		}
	}
	binary = buildRecoveryCLI4(t, base, false)
	log := environmentValue(os.OpenFile(filepath.Join(base, "daemon.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600))
	defer log.Close()
	daemon := exec.CommandContext(ctx, binary, append(append([]string{"daemon"}, args...), "--platform-fragment", filepath.Join(directory, "environment.sh"), "--interval", "20ms", "--sync-interval", "1s")...)
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
			t.Error("formal CLI5 daemon failed drain")
		}
	}()
	readyDaemon := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := run("status", "")
		var status localipc.Status
		if err == nil && json.Unmarshal(out, &status) == nil && status.Environments == 1 && status.Sequence > 0 && boots.Load() > 0 {
			readyDaemon = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !readyDaemon {
		b, _ := os.ReadFile(log.Name())
		classes := []string{}
		for _, s := range []string{"protocol", "signature", "evidence", "ledger", "mapping", "grant", "checkpoint", "invalid", "binding", "source"} {
			if bytes.Contains(bytes.ToLower(b), []byte(s)) {
				classes = append(classes, s)
			}
		}
		t.Fatalf("formal CLI5 daemon not ready fixedClasses=%v boot=%t", classes, boots.Load() > 0)
	}
	_, err = run("activate", "", "--environment", f.initial, "--priority", "10")
	originMust(t, err)
	out, err := run("export", "")
	originMust(t, err)
	if !bytes.Contains(out, []byte("SYNTHETIC_DAG_C='synthetic-dag-c'")) {
		t.Fatal("formal CLI5 failed verified recovered data read")
	}
	_, err = run("put", "synthetic-cli5-value", "--environment", f.initial, "--name", "SYNTHETIC_CLI5", "--value-stdin", "--request-id", "dag-cli5-"+role)
	if role == "ro" {
		if err == nil {
			t.Fatal("limited RO CLI5 wrote shared data")
		}
	} else {
		dagStage(t, "limited RW CLI5 receipt/pull write", err)
		out, err = run("export", "")
		originMust(t, err)
		if !bytes.Contains(out, []byte("SYNTHETIC_CLI5='synthetic-cli5-value'")) {
			t.Fatal("RW CLI5 skipped verified pull")
		}
	}
	dagStage(t, "formal CLI5 PAKE/Boot/P4/"+role, nil)
}

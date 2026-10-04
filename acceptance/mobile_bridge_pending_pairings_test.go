package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobilebridge"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 真正 PAKE 管理者 B → 待批准申请 C → 每次新 native Workflow 的 Boot/Pull/GET。
// AES Save/Check/CAS callback 是 Go native 适配器证据，不是手机系统认证或 SDK 验收。
type p2BridgeSlot struct{ b3aBridgeSlot }

// 旧正式 cert3 Workflow 的成熟持久路径是 Save；DAG-only fixture 特意禁该方法。
// 本片保留旧路径，hint 不宣称新增 DAG CAS 能力。
func (s *p2BridgeSlot) SaveSealed(packet []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.packet = bytes.Clone(packet)
	return nil
}

func TestNativeBridgePendingPairingsHTTPS(t *testing.T) {
	f := newMobileManagerFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	d, e := mobilebridge.NewDevice()
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	slot := &p2BridgeSlot{}
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.proxy.Certificate().Raw})
	v, e := d.OpenAtomicWorkflow(f.proxy.URL, "synthetic.p2.native.slot", nil, ca, slot)
	if e != nil {
		t.Fatal(e)
	}
	rootView := environmentValue(f.root.View())
	command := environmentValue(json.Marshal(map[string]any{"version": 1, "endpoint": f.proxy.URL, "operation": "enrollDeviceV3", "email": f.email, "password": f.password, "pairingId": "p2-manager-B", "approverDeviceId": rootView.DeviceID}))
	type result struct {
		raw string
		err error
	}
	done := make(chan result, 1)
	go func() { raw, e := v.ExecuteEnrollment(string(command), []byte("68429173")); done <- result{raw, e} }()
	select {
	case id := <-f.ready:
		if id != "p2-manager-B" {
			t.Fatal("unexpected actual enrollment")
		}
	case r := <-done:
		t.Fatal("enrollment ended before begin", r.err)
	case <-ctx.Done():
		t.Fatal("enrollment begin timeout")
	}
	_, e = f.root.ApprovePairingV3(ctx, mobileworkflow.ApprovalInput{PairingID: "p2-manager-B", ShortCode: []byte("68429173"), Selections: []mobileworkflow.ApprovalSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: strconv.FormatInt(time.Now().Unix()+3600, 10)}}})
	if e != nil {
		t.Fatal("actual PAKE approval", e)
	}
	var enrolled struct {
		OK   bool                `json:"ok"`
		Data mobileworkflow.View `json:"data"`
	}
	select {
	case r := <-done:
		if r.err != nil || json.Unmarshal([]byte(r.raw), &enrolled) != nil || !enrolled.OK || enrolled.Data.DeviceID == "" {
			t.Fatal("actual native enrollment rejected", r.err)
		}
	case <-ctx.Done():
		t.Fatal("native enrollment completion timeout")
	}
	v.Close()
	if _, e = f.root.RetryApprovalV3(ctx, "p2-manager-B"); e != nil {
		t.Fatal("manager original approval completion", e)
	}
	clear(command)
	bID := enrolled.Data.DeviceID
	credential := cryptox.PasswordCredential(f.password)
	login, e := syncclient.Login(ctx, syncclient.LoginConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), Email: f.email, Credential: hex.EncodeToString(credential[:])})
	clear(credential[:])
	if e != nil {
		t.Fatal(e)
	}
	_, sign, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(sign)
	_, receiving, e := cryptox.GenerateReceivingKey()
	if e != nil {
		t.Fatal(e)
	}
	defer clear(receiving)
	digest := sha256.Sum256(sign.Public().(ed25519.PublicKey))
	cID := hex.EncodeToString(digest[:])
	engine := environmentValue(localstate.New(&originMemoryStore{state: localstate.EmptyState()}))
	pending, e := syncclient.NewEnrollmentV3(syncclient.EnrollmentConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: login.AccountID, AccountGeneration: 1, DeviceID: cID, LoginToken: login.Token, SigningKey: sign, ReceivingPrivateKey: receiving, Engine: engine})
	if e != nil {
		t.Fatal(e)
	}
	defer pending.Close()
	_, e = pending.Begin(ctx, bID, "p2-request-C", []byte("39174682"))
	if e != nil {
		t.Fatal("actual unapproved request", e)
	}
	var boots, pulls, lists atomic.Int32
	countHook := &mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.StatusCode == 200 {
			switch {
			case strings.HasSuffix(r.Request.URL.Path, "/boot-sessions"):
				boots.Add(1)
			case strings.HasSuffix(r.Request.URL.Path, "/pull"):
				pulls.Add(1)
			case strings.HasSuffix(r.Request.URL.Path, "/pairing-requests-v3"):
				lists.Add(1)
			}
		}
		return nil
	}}
	f.responseHook.Store(countHook)
	defer f.responseHook.Store(nil)
	run := func() (string, error) {
		owner, e := d.OpenAtomicWorkflow(f.proxy.URL, "synthetic.p2.native.slot", slot.read(), ca, slot)
		if e != nil {
			t.Fatal("cold owner", e)
		}
		defer owner.Close()
		raw := environmentValue(json.Marshal(map[string]any{"version": 1, "endpoint": f.proxy.URL, "operation": "pendingPairingRequestsV3"}))
		return owner.ExecutePendingPairings(string(raw))
	}
	raw, e := run()
	if e != nil {
		t.Fatal("native pending snapshot", e)
	}
	var out struct {
		Version   int    `json:"version"`
		Operation string `json:"operation"`
		OK        bool   `json:"ok"`
		Data      struct {
			AccountID          string                             `json:"accountId"`
			AccountGeneration  string                             `json:"accountGeneration"`
			ApproverDeviceID   string                             `json:"approverDeviceId"`
			CertificateVersion string                             `json:"certificateVersion"`
			Capabilities       []string                           `json:"capabilities"`
			Requests           []syncclient.PendingPairingRequest `json:"requests"`
			Authoritative      bool                               `json:"authoritativeForApproval"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(raw), &out) != nil || !out.OK || out.Version != 1 || out.Operation != "pendingPairingRequestsV3" || out.Data.AccountID != login.AccountID || out.Data.AccountGeneration != "1" || out.Data.ApproverDeviceID != bID || out.Data.CertificateVersion != "3" || len(out.Data.Capabilities) != 1 || out.Data.Capabilities[0] != cryptox.EnvironmentOriginCapability || out.Data.Authoritative || len(out.Data.Requests) != 1 || out.Data.Requests[0].IdempotencyKey != "p2-request-C" || out.Data.Requests[0].InitiatorDeviceID != cID || out.Data.Requests[0].State != "pending" {
		t.Fatal("exact real metadata missing")
	}
	for _, name := range []string{"sequence", "platform", "name", "token", "signature", "password"} {
		if strings.Contains(raw, `"`+name+`"`) {
			t.Fatal("invented or secret metadata")
		}
	}
	if boots.Load() != 1 || pulls.Load() != 1 || lists.Load() != 1 {
		t.Fatal("refresh did not use current device Boot/Pull/GET")
	}

	// 锁外 Invalidate 必须立即取消网络，迟到已合法响应不能回传提示。
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	// 原 endpoint 已写入密文，额外 proxy 只能替换 HTTP transport，不得改变绑定。
	// 使用原 fixture 的响应钩子阻塞，以保留原生 endpoint 和 CA 精确绑定。
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if strings.HasSuffix(r.Request.URL.Path, "/pairing-requests-v3") {
			close(entered)
			<-release
		}
		return nil
	}})
	owner, e := d.OpenAtomicWorkflow(f.proxy.URL, "synthetic.p2.native.slot", slot.read(), ca, slot)
	if e != nil {
		t.Fatal(e)
	}
	pendingRaw := environmentValue(json.Marshal(map[string]any{"version": 1, "endpoint": f.proxy.URL, "operation": "pendingPairingRequestsV3"}))
	late := make(chan result, 1)
	go func() { raw, e := owner.ExecutePendingPairings(string(pendingRaw)); late <- result{raw, e} }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("blocked hint never arrived")
	}
	invalidated := make(chan struct{})
	go func() { owner.Invalidate(); close(invalidated) }()
	select {
	case <-invalidated:
	case <-time.After(time.Second):
		t.Fatal("Invalidate waited for network lock")
	}
	once.Do(func() { close(release) })
	select {
	case r := <-late:
		if r.err == nil || r.raw != "" {
			t.Fatal("late hint escaped invalidated owner")
		}
	case <-ctx.Done():
		t.Fatal("cancelled owner failed to drain")
	}
	owner.Close()
	f.responseHook.Store(countHook)

	_, e = f.root.PrepareDeviceGrant(ctx, syncclient.GrantUpdateIntent{ID: "p2-B-readonly", EnvironmentID: f.initial, SubjectDeviceID: bID, Role: "ro", ExpiresAt: time.Now().Unix() + 3000})
	if e != nil {
		t.Fatal("current signed downgrade prepare", e)
	}
	_, e = f.root.RetryManagement(ctx, "p2-B-readonly")
	if e != nil {
		t.Fatal("current signed downgrade", e)
	}
	before := lists.Load()
	raw, e = run()
	if e == nil || raw != "" || lists.Load() != before {
		t.Fatal("downgraded owner returned cached Admin hint")
	}
	if len(slot.read()) == 0 {
		t.Fatal("bound native state lost")
	}
	// 测试只读取合成子服务与合成设备，不向输出写数据值或凭据。
}

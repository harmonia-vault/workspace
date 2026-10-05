package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/pairing"
	"github.com/harmonia-vault/core-go/syncclient"
)

// Real native PAKE and Go crypto talk to the TS/SQLite service over verified
// loopback HTTPS. The AES callback models the native boundary, not Android PIN.
type mobileManagerResponseHook struct{ invoke func(*http.Response) error }

type mobileManagerFixture struct {
	t                              *testing.T
	proxy                          *httptest.Server
	root                           *mobileworkflow.Workflow
	rootActor                      *mobileManagerActor
	responseHook                   atomic.Pointer[mobileManagerResponseHook]
	email, password, code, initial string
	ready                          chan string
	completes                      atomic.Int64
	loseEnvironment                atomic.Bool
	environmentPosts               atomic.Int64
	loseComplete                   atomic.Bool
}
type mobileManagerActor struct {
	workflow  *mobileworkflow.Workflow
	config    mobileworkflow.Config
	load      func() []byte
	failStage atomic.Int32
}

func newMobileManagerActor(t *testing.T, f *mobileManagerFixture) *mobileManagerActor {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, receiving, err := cryptox.GenerateReceivingKey()
	if err != nil {
		t.Fatal(err)
	}
	save, load := recoveryNativeSeal(t, "synthetic-manager-native-state-v1")
	a := &mobileManagerActor{load: load}
	a.config = mobileworkflow.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), SigningKey: key, ReceivingPrivateKey: receiving, SaveProtectedState: func(data []byte) error {
		var s struct {
			Enrollment *struct {
				Sequence uint64 `json:"sequence"`
				Applied  bool   `json:"applied"`
			} `json:"enrollmentV5"`
			Cloud localstate.State `json:"cloud"`
		}
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		stage := a.failStage.Load()
		if s.Enrollment != nil && ((stage == 1 && s.Enrollment.Sequence == 0) || (stage == 2 && s.Enrollment.Sequence > 0 && s.Cloud.Cloud.Sequence == 0) || (stage == 3 && s.Enrollment.Sequence > 0 && s.Cloud.Cloud.Sequence > 0 && !s.Enrollment.Applied) || (stage == 4 && s.Enrollment.Applied)) {
			return errors.New("synthetic native manager save failure")
		}
		return save(data)
	}}
	a.workflow = environmentValue(mobileworkflow.New(a.config))
	t.Cleanup(func() { a.workflow.Close() })
	return a
}
func (a *mobileManagerActor) reopen(t *testing.T) {
	t.Helper()
	a.workflow.Close()
	a.config.ProtectedState = a.load()
	a.workflow = environmentValue(mobileworkflow.New(a.config))
}
func newMobileManagerFixture(t *testing.T) *mobileManagerFixture {
	t.Helper()
	f := &mobileManagerFixture{t: t, email: "mobile-manager@example.invalid", password: "synthetic-manager-password", ready: make(chan string, 8)}
	fixture := startFixture(t, "--capture-email")
	target := environmentValue(url.Parse(fixture.Endpoint))
	reverse := httputil.NewSingleHostReverseProxy(target)
	reverse.ModifyResponse = func(r *http.Response) error {
		if hook := f.responseHook.Load(); hook != nil {
			if err := hook.invoke(r); err != nil {
				return err
			}
		}
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/pairings-v5") {
			data, err := io.ReadAll(r.Body)
			if err != nil {
				return err
			}
			_ = r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(data))
			var s struct {
				ID string `json:"idempotencyKey"`
			}
			if err = json.Unmarshal(data, &s); err != nil {
				return err
			}
			f.ready <- s.ID
		}
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/environment-changes-v4") && f.loseEnvironment.Swap(false) {
			_ = r.Body.Close()
			data := []byte(`{"error":"synthetic_lost_environment_origin"}`)
			r.StatusCode = 502
			r.Body = io.NopCloser(bytes.NewReader(data))
			r.ContentLength = int64(len(data))
			r.Header.Set("Content-Length", strconv.Itoa(len(data)))
		}
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.Contains(r.Request.URL.Path, "/pairings-v5/") && strings.HasSuffix(r.Request.URL.Path, "/complete") && f.loseComplete.Swap(false) {
			_ = r.Body.Close()
			data := []byte(`{"error":"synthetic_lost_mobile_receipt"}`)
			r.StatusCode = 502
			r.Body = io.NopCloser(bytes.NewReader(data))
			r.ContentLength = int64(len(data))
			r.Header.Set("Content-Length", strconv.Itoa(len(data)))
		}
		return nil
	}
	f.proxy = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.Contains(r.URL.Path, "/pairings-v5/") && strings.HasSuffix(r.URL.Path, "/complete") {
			f.completes.Add(1)
		}
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/environment-changes-v4") {
			f.environmentPosts.Add(1)
		}
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(f.proxy.Close)
	root := newMobileManagerActor(t, f)
	f.root = root.workflow
	f.rootActor = root
	ctx := context.Background()
	registered := environmentValue(f.root.Register(ctx, f.email, f.password))
	var mails []struct{ To, Text string }
	if status := callJSON(t, f.proxy.Client(), f.proxy.URL, "/test/emails", "", "", "", nil, &mails); status != 200 {
		t.Fatal(status)
	}
	var proof mobileworkflow.EmailVerification
	for _, m := range mails {
		if m.To == f.email {
			for _, line := range strings.Split(m.Text, "\n") {
				if code, ok := strings.CutPrefix(line, "验证码："); ok && len(code) == 8 {
					proof = mobileworkflow.EmailVerification{AccountID: registered.AccountID, AccountGeneration: registered.AccountGeneration, Code: code}
				}
			}
		}
	}
	if proof.AccountID != registered.AccountID || proof.Code == "" {
		t.Fatal("synthetic email proof missing")
	}
	if err := f.root.VerifyEmail(ctx, proof); err != nil {
		t.Fatal(err)
	}
	if err := f.root.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	f.code = environmentValue(f.root.BeginInitialization(ctx, "合成环境X", "manager-init"))
	initial := environmentValue(f.root.CompleteInitialization(ctx, f.code))
	f.initial = initial.Environments[0].ID
	if _, err := f.root.SetVariable(ctx, f.initial, "SYNTHETIC_X", "synthetic-x-value", "manager-x-put"); err != nil {
		t.Fatal(err)
	}
	f.environmentPosts.Store(0)
	return f
}
func (f *mobileManagerFixture) enroll(t *testing.T, manager *mobileworkflow.Workflow, a *mobileManagerActor, id string, selections []mobileworkflow.ApprovalSelection) (mobileworkflow.View, error) {
	t.Helper()
	managerID := environmentValue(manager.View()).DeviceID
	return f.enrollWithManagerID(t, manager, a, id, selections, managerID)
}
func (f *mobileManagerFixture) enrollWithManagerID(t *testing.T, manager *mobileworkflow.Workflow, a *mobileManagerActor, id string, selections []mobileworkflow.ApprovalSelection, managerID string) (mobileworkflow.View, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}

	code := []byte("68429173")
	defer clear(code)
	type result struct {
		view mobileworkflow.View
		err  error
	}
	done := make(chan result, 1)
	go func() {
		v, e := a.workflow.EnrollDevice(ctx, mobileworkflow.EnrollmentInput{PairingID: id, ApproverDeviceID: managerID, ShortCode: code})
		done <- result{v, e}
	}()
	select {
	case got := <-f.ready:
		if got != id {
			t.Fatal("unexpected synthetic pairing")
		}
	case r := <-done:
		return r.view, r.err
	case <-ctx.Done():
		t.Fatal("pairing begin timeout")
	}
	approval, err := manager.ApprovePairingV5(ctx, mobileworkflow.ApprovalInput{PairingID: id, ShortCode: code, Selections: selections})
	if err != nil {
		t.Fatal("actual manager approval", err)
	}
	if approval.State != "approved" && approval.State != "complete" {
		t.Fatal(approval)
	}
	select {
	case r := <-done:
		return r.view, r.err
	case <-ctx.Done():
		t.Fatal("actual enrollment timeout")
	}
	return mobileworkflow.View{}, ctx.Err()
}
func mobileAssertPending(t *testing.T, a *mobileManagerActor) {
	t.Helper()
	if _, err := a.workflow.View(); !errors.Is(err, mobileworkflow.ErrMobileEnrollmentPending) {
		t.Fatal("pending enrollment exposed cached view", err)
	}
	if _, err := a.workflow.SetVariable(context.Background(), "unavailable", "SYNTHETIC", "value", "blocked-pending"); !errors.Is(err, mobileworkflow.ErrMobileEnrollmentPending) {
		t.Fatal("pending enrollment opened CRUD", err)
	}
	info := environmentValue(a.workflow.EnrollmentInfo())
	if info.TrustedDevice || info.State == "complete" {
		t.Fatal("pending receipt claimed applied", info)
	}
}
func TestMobileManagerEnrollmentNativeSaveBoundariesAndRestart(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("requires fixed real native SPAKE2")
	}
	for _, stage := range []int32{1, 2, 3, 4, 5} {
		t.Run(strconv.Itoa(int(stage)), func(t *testing.T) {
			f := newMobileManagerFixture(t)
			a := newMobileManagerActor(t, f)
			if stage == 5 {
				f.loseComplete.Store(true)
			} else {
				a.failStage.Store(stage)
			}
			_, err := f.enroll(t, f.root, a, "manager-b-enroll", []mobileworkflow.ApprovalSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: strconv.FormatInt(time.Now().Unix()+600, 10)}})
			if err == nil {
				t.Fatal("synthetic save/response fault accepted as success")
			}
			mobileAssertPending(t, a)
			if stage == 1 && f.completes.Load() != 0 {
				t.Fatal("complete POST preceded native receipt save")
			}
			a.failStage.Store(0)
			if stage != 1 {
				a.reopen(t)
				mobileAssertPending(t, a)
			} else {
				// Failed pre-POST save leaves no sealed receipt. An independently reopened
				// context must remain untrusted; the live operation can save its same receipt.
				old := a.workflow
				cfg := a.config
				cfg.ProtectedState = nil
				reopened := environmentValue(mobileworkflow.New(cfg))
				if _, err := reopened.View(); !errors.Is(err, mobileworkflow.ErrNotTrusted) {
					t.Fatal("unsaved receipt survived restart", err)
				}
				reopened.Close()
				a.workflow = old
			}
			view, err := a.workflow.ResumeEnrollment(context.Background(), "manager-b-enroll")
			if err != nil {
				t.Fatal("same protected receipt resume", err)
			}
			if len(view.Environments) != 1 || view.Environments[0].Variables["SYNTHETIC_X"] != "synthetic-x-value" {
				t.Fatal(view)
			}
			info := environmentValue(a.workflow.EnrollmentInfo())
			if !info.TrustedDevice || info.State != "complete" {
				t.Fatal(info)
			}
			a.reopen(t)
			if _, err = a.workflow.View(); err != nil {
				t.Fatal("applied receipt failed sealed resume", err)
			}
			if _, err = f.root.RetryApprovalV5(context.Background(), "manager-b-enroll"); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestMobileGenericManagerRealOriginCreateApproveReadOnlyAndRotate(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("requires fixed real native SPAKE2")
	}
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	b := newMobileManagerActor(t, f)
	expires := strconv.FormatInt(time.Now().Unix()+600, 10)
	if _, err := f.enroll(t, f.root, b, "manager-b-enroll", []mobileworkflow.ApprovalSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: expires}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.root.RetryApprovalV5(ctx, "manager-b-enroll"); err != nil {
		t.Fatal(err)
	}
	b.reopen(t)
	f.loseEnvironment.Store(true)
	if _, err := b.workflow.CreateEnvironment(ctx, "非初始手机Y", "manager-b-create-y"); err == nil {
		t.Fatal("accepted create response loss falsely succeeded")
	}
	b.reopen(t)
	created, err := b.workflow.CreateEnvironment(ctx, "非初始手机Y", "manager-b-create-y")
	if err != nil {
		t.Fatal("non-root same two-package create resume", err)
	}
	if f.environmentPosts.Load() != 1 {
		t.Fatal("accepted create retry became a new write")
	}
	y := ""
	for _, e := range created.Environments {
		if e.ID != f.initial {
			y = e.ID
		}
	}
	if y == "" {
		t.Fatal(created)
	}
	if _, err = b.workflow.SetVariable(ctx, y, "SYNTHETIC_Y", "synthetic-y-value", "manager-b-y-put"); err != nil {
		t.Fatal(err)
	}
	c := newMobileManagerActor(t, f)
	readExpires := strconv.FormatInt(time.Now().Unix()+500, 10)
	cv, err := f.enroll(t, b.workflow, c, "manager-c-enroll", []mobileworkflow.ApprovalSelection{{EnvironmentID: y, Role: "ro", ExpiresAt: readExpires}})
	if err != nil {
		t.Fatal("non-root approving Y reader", err)
	}
	if len(cv.Environments) != 1 || cv.Environments[0].ID != y || cv.Environments[0].Variables["SYNTHETIC_Y"] != "synthetic-y-value" || cv.Environments[0].Role != localstate.ReadOnly {
		t.Fatal("reader scopes/decryption", cv)
	}
	if _, err = c.workflow.SetVariable(ctx, y, "SYNTHETIC_Y", "forbidden", "manager-c-write"); !errors.Is(err, syncclient.ErrWritePermission) {
		t.Fatal("read-only writer opened", err)
	}
	if _, err = b.workflow.RetryApprovalV5(ctx, "manager-c-enroll"); err != nil {
		t.Fatal(err)
	}
	d := newMobileManagerActor(t, f)
	laterExpiry := strconv.FormatInt(time.Now().Unix()+900, 10)
	if _, err := f.enroll(t, f.root, d, "manager-d-enroll", []mobileworkflow.ApprovalSelection{{EnvironmentID: f.initial, Role: "ro", ExpiresAt: laterExpiry}}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.root.RetryApprovalV5(ctx, "manager-d-enroll"); err != nil {
		t.Fatal(err)
	}
	f.loseEnvironment.Store(true)
	if _, err := b.workflow.RotateEnvironment(ctx, f.initial, "manager-b-rotate-x"); err == nil {
		t.Fatal("accepted rotate response loss falsely succeeded")
	}
	b.reopen(t)
	if _, err = b.workflow.RotateEnvironment(ctx, f.initial, "manager-b-rotate-x"); err != nil {
		t.Fatal("non-root original two-package rotation resume", err)
	}
	if f.environmentPosts.Load() != 2 {
		t.Fatal("accepted environment retries became new writes", f.environmentPosts.Load())
	}
	dv := environmentValue(d.workflow.Pull(ctx))
	if len(dv.Environments) != 1 || dv.Environments[0].Variables["SYNTHETIC_X"] != "synthetic-x-value" {
		t.Fatal("read-only rotation recipient lost", dv)
	}
	var saved struct {
		Grants            []cryptox.SignedGrantWire `json:"grants"`
		EnvironmentWrites map[string]struct {
			Sequence uint64 `json:"sequence"`
			Origin   struct {
				Packet cryptox.EnvironmentChangeV2 `json:"packet"`
			} `json:"originV2"`
		} `json:"environmentWrites"`
	}
	if err = json.Unmarshal(b.load(), &saved); err != nil {
		t.Fatal(err)
	}
	rot := saved.EnvironmentWrites["manager-b-rotate-x"]
	head, _ := strconv.ParseUint(rot.Origin.Packet.Change.ExpectedSequence, 10, 64)
	if rot.Sequence != head+1+uint64(len(rot.Origin.Packet.Change.Mutations)) {
		t.Fatal("sealed accepted tail differs from original batch", rot.Sequence)
	}
	roles := map[string]string{}
	for _, g := range rot.Origin.Packet.Change.Grants {
		roles[g.Grant.SubjectDeviceID] = g.Grant.Role + "/" + g.Grant.ExpiresAt
	}
	if roles[environmentValue(f.root.View()).DeviceID] != "admin/0" || roles[environmentValue(b.workflow.View()).DeviceID] != "admin/"+expires || roles[environmentValue(d.workflow.View()).DeviceID] != "ro/"+laterExpiry {
		t.Fatal("rotation changed current recipient role or lifetime", roles)
	}
	bv := environmentValue(b.workflow.View())
	for _, e := range bv.Environments {
		if e.ID == f.initial && e.Variables["SYNTHETIC_X"] != "synthetic-x-value" {
			t.Fatal("rotation lost current plaintext")
		}
	}
	av := environmentValue(f.root.Pull(ctx))
	if len(av.Environments) != 1 || av.Environments[0].Variables["SYNTHETIC_X"] != "synthetic-x-value" {
		t.Fatal("root permanent recipient lost", av)
	}
	b.reopen(t)
	c.reopen(t)
	if _, err = c.workflow.Pull(ctx); err != nil {
		t.Fatal("reader after foreign rotation", err)
	}
	// Exact signed checkpoint and ledger are sealed; no bool or server directory
	// may reopen cert3 cached plaintext without them.
	var state map[string]json.RawMessage
	if err = json.Unmarshal(b.load(), &state); err != nil {
		t.Fatal(err)
	}
	var local map[string]json.RawMessage
	_ = json.Unmarshal(state["cloud"], &local)
	var cloud map[string]json.RawMessage
	_ = json.Unmarshal(local["cloud"], &cloud)
	delete(cloud, "issuerEvidence")
	local["cloud"] = environmentValue(json.Marshal(cloud))
	state["cloud"] = environmentValue(json.Marshal(local))
	cfg := b.config
	cfg.ProtectedState = environmentValue(json.Marshal(state))
	if bad, err := mobileworkflow.New(cfg); err == nil {
		bad.Close()
		t.Fatal("cert3 cache without ledger restored")
	}
}

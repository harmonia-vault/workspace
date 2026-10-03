package acceptance

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
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

func approvalManager4(t *testing.T) (*mobileManagerFixture, *mobileManagerActor) {
	t.Helper()
	f := newMobileManagerFixture(t)
	e, owner, _ := authorityActor(t, f, "approval4-recovery-transition")
	defer owner.Close()
	info, err := e.workflow.RegisterRecoveredDevice(context.Background(), "approval4-recovery-device", []mobileworkflow.RecoveryDeviceSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: "0"}})
	if err != nil || !info.TrustedDevice {
		t.Fatal(info, err)
	}
	return f, e
}
func approvalChild4(t *testing.T, f *mobileManagerFixture, e *mobileManagerActor, id string) (*syncclient.EnrollmentV4, context.Context, <-chan error, *mobileManagerActor) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	child := newMobileManagerActor(t, f)
	credential := cryptox.PasswordCredential(f.password)
	defer clear(credential[:])
	login := environmentValue(syncclient.Login(ctx, syncclient.LoginConfig{HTTPClient: f.proxy.Client(), Endpoint: f.proxy.URL, Email: f.email, Credential: hex.EncodeToString(credential[:])}))
	idHash := sha256.Sum256(child.config.SigningKey.Public().(ed25519.PublicKey))
	engine := environmentValue(localstate.New(&originMemoryStore{state: localstate.EmptyState()}))
	enrol := environmentValue(syncclient.NewEnrollmentV4(syncclient.EnrollmentConfig{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: login.AccountID, AccountGeneration: environmentValue(strconv.ParseUint(login.AccountGeneration, 10, 64)), DeviceID: hex.EncodeToString(idHash[:]), LoginToken: login.Token, SigningKey: child.config.SigningKey, ReceivingPrivateKey: child.config.ReceivingPrivateKey, Engine: engine}))
	t.Cleanup(enrol.Close)
	manager := environmentValue(e.workflow.View()).DeviceID
	if _, err := enrol.Begin(ctx, manager, id, []byte("67941386")); err != nil {
		t.Fatal(err)
	}
	ready := make(chan error, 1)
	go func() {
		for {
			if _, err := enrol.Advance(ctx); err != nil {
				ready <- err
				return
			}
			if _, err := enrol.Receipt(); err == nil {
				ready <- nil
				return
			} else if !errors.Is(err, pairing.ErrState) {
				ready <- err
				return
			}
			select {
			case <-ctx.Done():
				ready <- ctx.Err()
				return
			case <-time.After(30 * time.Millisecond):
			}
		}
	}()
	return enrol, ctx, ready, child
}
func approvalCompleteChild4(t *testing.T, f *mobileManagerFixture, enrol *syncclient.EnrollmentV4, ctx context.Context, ready <-chan error, child *mobileManagerActor) {
	t.Helper()
	if err := <-ready; err != nil {
		t.Fatal("real child PAKE/HPKE", err)
	}
	result, err := enrol.Complete(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Verifier.Close()
	a := result.Receipt.Approval
	engine := environmentValue(localstate.New(&originMemoryStore{state: localstate.EmptyState()}))
	c := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, HTTPClient: f.proxy.Client(), AccountID: a.Context.AccountID, AccountGeneration: environmentValue(strconv.ParseUint(a.Context.AccountGeneration, 10, 64)), DeviceID: a.Context.InitiatorDeviceID, Engine: engine, Verifier: result.Verifier}))
	c = environmentValue(c.BootDevice(ctx, child.config.SigningKey))
	if _, err = c.Pull(ctx); err != nil {
		t.Fatal("real child ordinary proof3 pull", err)
	}
	env := engine.State().Cloud.Environments[f.initial]
	if env.Role != localstate.ReadOnly || env.Values["SYNTHETIC_X"] != "synthetic-x-value" || len(engine.State().Cloud.Environments) != 1 {
		t.Fatal("child HPKE/AEAD rights/value mismatch")
	}
	if result.Sequence == 0 || len(a.Grants) != 1 || a.Grants[0].Grant.Role != "ro" || a.Grants[0].Grant.EnvironmentID != f.initial {
		t.Fatal("explicit child rights changed")
	}
}
func TestMobileApprovalV4RealHighLevelJournalAndCompletion(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("requires real native SPAKE2")
	}
	for _, stage := range []string{"normal", "prepared-save", "attempted-save", "approved-save", "accepted-lost-response", "complete-save"} {
		t.Run(stage, func(t *testing.T) {
			f, e := approvalManager4(t)
			id := "mobile-v4-" + stage
			enrol, ctx, ready, child := approvalChild4(t, f, e, id)
			var posts atomic.Int64
			var lose atomic.Bool
			lose.Store(stage == "accepted-lost-response")
			f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
				if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/pairings-v4/"+id+"/approve") {
					posts.Add(1)
					if r.StatusCode == 200 && lose.Swap(false) {
						authorityLostResponse(r)
					}
				}
				return nil
			}})
			nativeSave := e.config.SaveProtectedState
			var fail atomic.Bool
			fail.Store(stage != "normal" && stage != "accepted-lost-response" && stage != "complete-save")
			e.config.SaveProtectedState = func(data []byte) error {
				var state struct {
					Approval *struct {
						Attempted bool
						Approved  bool
						Sequence  uint64
					} `json:"pendingApprovalV4"`
				}
				if err := json.Unmarshal(data, &state); err != nil {
					return err
				}
				r := state.Approval
				if fail.Load() && r != nil && (stage == "prepared-save" && !r.Attempted || stage == "attempted-save" && r.Attempted && !r.Approved || stage == "approved-save" && r.Approved && r.Sequence == 0 || stage == "complete-save" && r.Sequence != 0) {
					return errors.New("synthetic v4 approval native save failure")
				}
				return nativeSave(data)
			}
			// New per-operation native-authenticated context; no manager key reaches any bridge.
			e.reopen(t)
			choice := []mobileworkflow.ApprovalSelection{{EnvironmentID: f.initial, Role: "ro", ExpiresAt: strconv.FormatInt(time.Now().Unix()+300, 10)}}
			out, err := e.workflow.ApprovePairingV4(ctx, mobileworkflow.ApprovalInput{PairingID: id, ShortCode: []byte("67941386"), Selections: choice})
			fault := stage != "normal" && stage != "complete-save"
			if fault && err == nil {
				t.Fatal("fault reported successful approval", out)
			}
			if (stage == "prepared-save" || stage == "attempted-save") && posts.Load() != 0 {
				t.Fatal("failed native seal posted approval")
			}
			if stage == "normal" || stage == "complete-save" {
				if err != nil || out.State != "approved" || out.Sequence != 0 {
					t.Fatal("approved incorrectly equals complete", out, err)
				}
			}
			if stage == "attempted-save" || stage == "accepted-lost-response" || stage == "approved-save" {
				if err = e.workflow.CancelApprovalV4(id); !errors.Is(err, mobileworkflow.ErrApprovalPending) {
					t.Fatal("attempted unknown approval cancelled", err)
				}
			}
			fail.Store(false)
			if stage != "prepared-save" {
				e.reopen(t)
			}
			if fault {
				out, err = e.workflow.RetryApprovalV4(ctx, id)
				if err != nil || out.State != "approved" || out.Sequence != 0 {
					t.Fatal("same sealed v4 original retry", out, err)
				}
			}
			if posts.Load() != 1 {
				t.Fatal("original approval became a second write", posts.Load())
			}
			if _, err = e.workflow.View(); !errors.Is(err, mobileworkflow.ErrApprovalPending) {
				t.Fatal("pending complete gate lost", err)
			}
			approvalCompleteChild4(t, f, enrol, ctx, ready, child)
			if stage == "complete-save" {
				fail.Store(true)
			}
			out, err = e.workflow.RetryApprovalV4(ctx, id)
			if stage == "complete-save" {
				if err == nil || out.State == "complete" || out.Sequence != 0 {
					t.Fatal("last seal failed but falsely complete", out, err)
				}
				fail.Store(false)
				e.reopen(t)
				out, err = e.workflow.RetryApprovalV4(ctx, id)
			}
			if err != nil || out.State != "complete" || out.Sequence == 0 {
				t.Fatal("child exact double-signed completion", out, err)
			}
			e.reopen(t)
			info, err := e.workflow.ApprovalInfoV4()
			if err != nil || info.State != "complete" || info.Sequence != out.Sequence {
				t.Fatal("sealed completion restore", info, err)
			}
		})
	}
}

// 正式CLI4默认执行路径仍由原helper验证；唯一替换的是管理者高层审批入口。
func TestMobileApprovalV4ApprovesFormalCLIAndFinalSeal(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("requires real native SPAKE2")
	}
	f, e := approvalManager4(t)
	nativeSave := e.config.SaveProtectedState
	var fail atomic.Bool
	e.config.SaveProtectedState = func(data []byte) error {
		var state struct {
			Approval *struct{ Sequence uint64 } `json:"pendingApprovalV4"`
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return err
		}
		if fail.Load() && state.Approval != nil && state.Approval.Sequence > 0 {
			return errors.New("synthetic formal CLI4 final native save failure")
		}
		return nativeSave(data)
	}
	e.reopen(t)
	originalID := ""
	actualRecoveryCLIPairAndDaemon(t, f, e, f.initial, "ro", true, func(ctx context.Context, id string, code []byte) {
		originalID = id
		out, err := e.workflow.ApprovePairingV4(ctx, mobileworkflow.ApprovalInput{
			PairingID: id, ShortCode: code,
			Selections: []mobileworkflow.ApprovalSelection{{EnvironmentID: f.initial, Role: "ro", ExpiresAt: "0"}},
		})
		if err != nil || out.State != "approved" || out.Sequence != 0 || out.PairingID != id {
			t.Fatal("actual high-level V4 approval falsely complete or failed", out, err)
		}
	})
	if originalID == "" {
		t.Fatal("formal CLI4 original ID absent")
	}
	fail.Store(true)
	out, err := e.workflow.RetryApprovalV4(context.Background(), originalID)
	if err == nil || out.State == "complete" || out.Sequence != 0 {
		t.Fatal("formal CLI4 manager final seal failed but reported complete", out, err)
	}
	if _, err = e.workflow.View(); !errors.Is(err, mobileworkflow.ErrApprovalPending) {
		t.Fatal("formal CLI4 unsealed complete bypassed pending gate", err)
	}
	fail.Store(false)
	e.reopen(t)
	out, err = e.workflow.RetryApprovalV4(context.Background(), originalID)
	if err != nil || out.State != "complete" || out.Sequence == 0 || out.PairingID != originalID {
		t.Fatal("formal CLI4 original sealed approval failed final retry", out, err)
	}
	e.reopen(t)
	info, err := e.workflow.ApprovalInfoV4()
	if err != nil || info.State != "complete" || info.Sequence != out.Sequence {
		t.Fatal("formal CLI4 exact complete metadata not sealed", info, err)
	}
	t.Log("真实高层Recovered E→正式CLI4→504原收据恢复→CGO0新Boot/Proof3/RO拒写→管理者finalseal失败原id恢复通过")
}

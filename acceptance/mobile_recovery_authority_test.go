package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/pairing"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMobileRecoveryAuthorityRealTypedTransition(t *testing.T) {
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	r := newMobileManagerActor(t, f)
	if err := r.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	old, info, err := r.workflow.BeginRecoveryAuthoritySession(ctx, f.code)
	if err != nil {
		t.Fatal("continuous entry", err)
	}
	defer old.Close()
	if info.TrustedDevice || !info.RotationRequired {
		t.Fatal(info)
	}
	if _, err = r.workflow.RecoveryView(); err != nil {
		t.Fatal(err)
	}
	code, err := r.workflow.BeginRecoveryAuthorityTransition(ctx, "typed-transition-smoke")
	if err != nil {
		t.Fatal("challenge", err)
	}
	if _, err = r.workflow.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
		t.Fatal("pending plaintext gate", err)
	}
	if _, _, err = r.workflow.CompleteRecoveryAuthorityTransition(ctx, code[:51]); err == nil {
		t.Fatal("partial code accepted")
	}
	owner, done, err := r.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
	if err != nil {
		t.Fatal("typed signed full rotation", err)
	}
	defer owner.Close()
	if done.TrustedDevice || done.RotationRequired || done.RecoveryGeneration != "2" {
		t.Fatal(done)
	}
	if _, err = old.Binding(); !errors.Is(err, mobileworkflow.ErrRecoverySession) {
		t.Fatal("old owner retained", err)
	}
	v, err := r.workflow.RecoveryView()
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Environments) != 1 || v.Environments[0].Variables["SYNTHETIC_X"] != "synthetic-x-value" {
		t.Fatal(v.Info)
	}
	r.reopen(t)
	if _, err = r.workflow.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoverySession) {
		t.Fatal("sealed cache replaced process owner", err)
	}
	if err = r.workflow.AttachRecoverySession(owner); err != nil {
		t.Fatal("native newop owner attach", err)
	}
	if _, err = r.workflow.RecoveryView(); err != nil {
		t.Fatal(err)
	}
}

func TestMobileRecoveryAuthorityRealExplicitDeviceAndBoot(t *testing.T) {
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	e := newMobileManagerActor(t, f)
	if err := e.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	old, _, err := e.workflow.BeginRecoveryAuthoritySession(ctx, f.code)
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	code, err := e.workflow.BeginRecoveryAuthorityTransition(ctx, "typed-explicit-rotation")
	if err != nil {
		t.Fatal(err)
	}
	current, done, err := e.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	if done.TrustedDevice || done.RotationRequired {
		t.Fatal(done)
	}
	if _, err = e.workflow.View(); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
		t.Fatal("rotation auto trusted", err)
	}
	registered, err := e.workflow.RegisterRecoveredDevice(ctx, "typed-explicit-e", []mobileworkflow.RecoveryDeviceSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: "0"}})
	if err != nil {
		t.Fatal("explicit actual HPKE+cert4+boot/pull", err)
	}
	if !registered.TrustedDevice || registered.Sequence == 0 || registered.State != "trusted" {
		t.Fatal(registered)
	}
	v, err := e.workflow.View()
	if err != nil || len(v.Environments) != 1 || v.Environments[0].Variables["SYNTHETIC_X"] != "synthetic-x-value" {
		t.Fatal("same verified stream", err, v.Checkpoint)
	}
	e.reopen(t)
	if _, err = e.workflow.Pull(ctx); err != nil {
		t.Fatal("fresh device boot from exact sealed cert4", err)
	}
	if _, err = e.workflow.SetVariable(ctx, f.initial, "SYNTHETIC_RECOVERED_WRITE", "synthetic-recovered-value", "typed-e-put"); err != nil {
		t.Fatal("actual admin shared mutation", err)
	}
}

func authorityActor(t *testing.T, f *mobileManagerFixture, id string) (*mobileManagerActor, *mobileworkflow.RecoverySession, string) {
	t.Helper()
	ctx := context.Background()
	e := newMobileManagerActor(t, f)
	if err := e.workflow.Login(ctx, f.email, f.password); err != nil {
		t.Fatal(err)
	}
	old, _, err := e.workflow.BeginRecoveryAuthoritySession(ctx, f.code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(old.Close)
	code, err := e.workflow.BeginRecoveryAuthorityTransition(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	current, _, err := e.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(current.Close)
	return e, current, code
}
func authorityLostResponse(r *http.Response) {
	_ = r.Body.Close()
	data := []byte(`{"error":"synthetic_lost_authority_receipt"}`)
	r.StatusCode = 502
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	r.Header.Set("Content-Length", strconv.Itoa(len(data)))
}
func TestMobileRecoveryAuthorityOriginalJournalAndNativeSaveFailures(t *testing.T) {
	for _, stage := range []string{"transition-pre-post", "transition-lost-response", "transition-final-save", "device-pre-post", "device-accepted-save", "device-final-save", "device-lost-response", "device-proof-missing"} {
		t.Run(stage, func(t *testing.T) {
			f := newMobileManagerFixture(t)
			ctx := context.Background()
			e := newMobileManagerActor(t, f)
			if err := e.workflow.Login(ctx, f.email, f.password); err != nil {
				t.Fatal(err)
			}
			old, _, err := e.workflow.BeginRecoveryAuthoritySession(ctx, f.code)
			if err != nil {
				t.Fatal(err)
			}
			defer old.Close()
			code, err := e.workflow.BeginRecoveryAuthorityTransition(ctx, "journal-transition")
			if err != nil {
				t.Fatal(err)
			}
			var transitionPosts, devicePosts atomic.Int64
			var lose, missing atomic.Bool
			lose.Store(strings.Contains(stage, "lost-response"))
			missing.Store(stage == "device-proof-missing")
			f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
				if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovery-authority-transitions") {
					transitionPosts.Add(1)
					if stage == "transition-lost-response" && lose.Swap(false) && r.StatusCode == 200 {
						authorityLostResponse(r)
					}
				}
				if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovered-devices") {
					devicePosts.Add(1)
					if stage == "device-lost-response" && lose.Swap(false) && r.StatusCode == 200 {
						authorityLostResponse(r)
					}
				}
				if strings.HasSuffix(r.Request.URL.Path, "/pull") && missing.Load() && r.StatusCode == 200 {
					data, _ := io.ReadAll(r.Body)
					_ = r.Body.Close()
					var wire map[string]json.RawMessage
					if err := json.Unmarshal(data, &wire); err != nil {
						return err
					}
					delete(wire, "issuerEvidence")
					data, err := json.Marshal(wire)
					if err != nil {
						return err
					}
					r.Body = io.NopCloser(bytes.NewReader(data))
					r.ContentLength = int64(len(data))
					r.Header.Set("Content-Length", strconv.Itoa(len(data)))
				}
				return nil
			}})
			var fail atomic.Bool
			fail.Store(true)
			nativeSave := e.config.SaveProtectedState
			e.config.SaveProtectedState = func(data []byte) error {
				var state struct {
					Authority *struct {
						Pending *struct {
							Applied bool
							Packet  struct {
								AuthorizationSignature string `json:"authorizationSignature"`
							}
						}
					} `json:"recoveryAuthority"`
					Device *struct {
						AcceptedSequence uint64 `json:"acceptedSequence"`
						Applied          bool
					} `json:"recoveredDevice"`
				}
				if err := json.Unmarshal(data, &state); err != nil {
					return err
				}
				should := stage == "transition-pre-post" && state.Authority != nil && state.Authority.Pending != nil && state.Authority.Pending.Packet.AuthorizationSignature != "" || stage == "transition-final-save" && state.Authority != nil && state.Authority.Pending != nil && state.Authority.Pending.Applied || stage == "device-pre-post" && state.Device != nil && state.Device.AcceptedSequence == 0 || stage == "device-accepted-save" && state.Device != nil && state.Device.AcceptedSequence != 0 && !state.Device.Applied || stage == "device-final-save" && state.Device != nil && state.Device.Applied
				if fail.Load() && should {
					return errors.New("synthetic authority native storage failure")
				}
				return nativeSave(data)
			}
			// New operation obtains the saved native context; old RAM owner is explicitly
			// reattached after independent AES/authentication, never read from Dart.
			e.reopen(t)
			if err = e.workflow.AttachRecoverySession(old); err != nil {
				t.Fatal(err)
			}
			next, info, err := e.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
			if strings.HasPrefix(stage, "transition-") {
				if err == nil || info.TrustedDevice {
					t.Fatal("fault did not close applied gate", stage, info, err)
				}
				if stage == "transition-pre-post" && transitionPosts.Load() != 0 {
					t.Fatal("native failure posted signed packet")
				}
				if stage != "transition-pre-post" {
					if _, err = old.Binding(); !errors.Is(err, mobileworkflow.ErrRecoverySession) {
						t.Fatal("sealed old owner retained", err)
					}
				}
				if _, err = e.workflow.RecoveryView(); err == nil {
					t.Fatal("unknown plaintext opened")
				}
				fail.Store(false)
				e.reopen(t)
				if stage == "transition-pre-post" {
					if err = e.workflow.AttachRecoverySession(old); err != nil {
						t.Fatal(err)
					}
				}
				next, info, err = e.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
				if err != nil || info.TrustedDevice || info.RotationRequired {
					t.Fatal("same original transition resume", info, err)
				}
				defer next.Close()
				if transitionPosts.Load() != 1 {
					t.Fatal("idempotent original transition became new write", transitionPosts.Load())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer next.Close()
			registration, err := e.workflow.RegisterRecoveredDevice(ctx, "journal-device", []mobileworkflow.RecoveryDeviceSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: "0"}})
			if err == nil || registration.TrustedDevice {
				t.Fatal("device failure falsely trusted", registration, err)
			}
			if stage == "device-pre-post" && devicePosts.Load() != 0 {
				t.Fatal("native save failure posted enrollment")
			}
			if _, err = e.workflow.View(); err == nil {
				t.Fatal("device pending exposed plaintext")
			}
			if _, err = e.workflow.RecoveryView(); err == nil {
				t.Fatal("device pending exposed restricted plaintext")
			}
			meta, err := e.workflow.RecoveredDeviceInfo()
			if err != nil || meta.TrustedDevice {
				t.Fatal(meta, err)
			}
			fail.Store(false)
			missing.Store(false)
			if stage != "device-pre-post" {
				e.reopen(t)
			}
			registration, err = e.workflow.RetryRecoveredDevice(ctx, "journal-device")
			if err != nil || !registration.TrustedDevice {
				t.Fatal("same original device status/boot/pull/save", registration, err)
			}
			if devicePosts.Load() != 1 {
				t.Fatal("device original retry became new operation", devicePosts.Load())
			}
			e.reopen(t)
			if _, err = e.workflow.Pull(ctx); err != nil {
				t.Fatal("durable exact accepted ledger", err)
			}
		})
	}
}
func TestMobileRecoveryAuthorityPendingDeadlineAndForgedApplied(t *testing.T) {
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	e, owner, _ := authorityActor(t, f, "deadline-transition")
	defer owner.Close()
	nativeSave := e.config.SaveProtectedState
	var fail atomic.Bool
	fail.Store(true)
	e.config.SaveProtectedState = func(data []byte) error {
		var s map[string]json.RawMessage
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if fail.Load() && len(s["recoveredDevice"]) != 0 {
			return errors.New("synthetic journal blocked")
		}
		return nativeSave(data)
	}
	e.reopen(t)
	if err := e.workflow.AttachRecoverySession(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := e.workflow.RegisterRecoveredDevice(ctx, "deadline-device", []mobileworkflow.RecoveryDeviceSelection{{EnvironmentID: f.initial, Role: "ro", ExpiresAt: "0"}}); err == nil {
		t.Fatal("save failed")
	}
	fail.Store(false)
	// First persist the same packet without allowing POST, then close/reopen only
	// authenticated native state. Server clock remains real; client alone must refuse.
	clock := time.Now()
	var offset atomic.Int64
	e.config.Now = func() time.Time { return clock.Add(time.Duration(offset.Load()) * time.Second) }
	e.config.SaveProtectedState = func(data []byte) error { return nativeSave(data) }
	// Calling Retry with a cancelled context durably journals before networking.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := e.workflow.RetryRecoveredDevice(cancelled, "deadline-device"); err == nil {
		t.Fatal("cancelled retry succeeded")
	}
	e.reopen(t)
	offset.Store(121)
	if _, err := e.workflow.RetryRecoveredDevice(ctx, "deadline-device"); !errors.Is(err, mobileworkflow.ErrRecoveryExpired) {
		t.Fatal("expired original replay", err)
	}
	if _, err := e.workflow.RecoveryView(); err == nil {
		t.Fatal("deadline pending data exposed")
	}
	native := e.load()
	var state map[string]json.RawMessage
	if err := json.Unmarshal(native, &state); err != nil {
		t.Fatal(err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(state["recoveredDevice"], &record); err != nil {
		t.Fatal(err)
	}
	record["applied"] = json.RawMessage("true")
	state["recoveredDevice"] = environmentValue(json.Marshal(record))
	bad := e.config
	bad.ProtectedState = environmentValue(json.Marshal(state))
	if w, err := mobileworkflow.New(bad); err == nil {
		w.Close()
		t.Fatal("forged Applied promoted without receipt/ledger")
	}
}

func TestMobileRecoveryAuthorityRealAllDevicesRevokedMultipleVersions(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("requires real native SPAKE2")
	}
	f, b, c, y := recoveryOriginPopulated(t)
	ctx := context.Background()
	recoveryOriginRevokeDevice(t, f, b, f.rootActor, "authority-revoke-root")
	recoveryOriginRevokeDevice(t, f, b, c, "authority-revoke-c")
	if result, err := b.workflow.RevokeSelf(ctx, "authority-revoke-last-b"); err != nil || !result.Completed {
		t.Fatal(result, err)
	}
	e, owner, _ := authorityActor(t, f, "allrevoked-transition")
	defer owner.Close()
	v, err := e.workflow.RecoveryView()
	if err != nil {
		t.Fatal(err)
	}
	recovered := map[string]mobileworkflow.RecoveredEnvironment{}
	for _, r := range v.Environments {
		recovered[r.ID] = r
	}
	if recovered[f.initial].KeyVersion != "2" || recovered[f.initial].Variables["SYNTHETIC_X"] != "synthetic-x-value" || recovered[y].Variables["SYNTHETIC_C_WRITE"] != "synthetic-c-shared-value" {
		t.Fatal("full graph/real HPKE data was not recovered")
	}
	info, err := e.workflow.RegisterRecoveredDevice(ctx, "allrevoked-explicit-e", []mobileworkflow.RecoveryDeviceSelection{{EnvironmentID: f.initial, Role: "ro", ExpiresAt: strconv.FormatInt(time.Now().Unix()+240, 10)}, {EnvironmentID: y, Role: "admin", ExpiresAt: "0"}})
	if err != nil || !info.TrustedDevice {
		t.Fatal(info, err)
	}
	e.reopen(t)
	view, err := e.workflow.Pull(ctx)
	if err != nil || len(view.Environments) != 2 {
		t.Fatal("new E boot exact proof3 after old actors revoked", err)
	}
	if _, err = e.workflow.SetVariable(ctx, f.initial, "SYNTHETIC_RO_FORBIDDEN", "value", "e-cannot-write-ro"); err == nil {
		t.Fatal("RO device used recovery origin to write")
	}
	if _, err = e.workflow.SetVariable(ctx, y, "SYNTHETIC_NEW_E_WRITE", "synthetic-e-value", "e-y-write"); err != nil {
		t.Fatal(err)
	}
}

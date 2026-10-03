package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/mobileworkflow"
)

// This uses only synthetic accounts and actual Go Ed25519/HPKE/AEAD against the
// HTTPS SQLite service. Config.Now models a device clock; host/AVD clocks stay
// untouched and the signed server expiry is never changed.
func TestNativeRecoveryRegistrationWaitsForStrictSigningHorizon(t *testing.T) {
	for _, tc := range []struct {
		name   string
		lag    time.Duration
		cancel bool
	}{
		{"three-second-device-lag", 3 * time.Second, false},
		{"cancel-after-bound-challenge", 3 * time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newMobileManagerFixture(t)
			a := f.rootActor
			ctx := context.Background()
			view := environmentValue(a.workflow.CreateEnvironment(ctx, "合成恢复Y", "clock-root-y"))
			y := ""
			for _, env := range view.Environments {
				if env.ID != f.initial {
					y = env.ID
				}
			}
			if y == "" {
				t.Fatal("synthetic Y was not created")
			}
			_ = environmentValue(a.workflow.SetVariable(ctx, f.initial, "SYNTHETIC_X_ONLY", "synthetic-clock-x", "clock-root-x"))
			_ = environmentValue(a.workflow.SetVariable(ctx, y, "SYNTHETIC_CROSS", "synthetic-clock-y", "clock-root-y-value"))
			_ = environmentValue(a.workflow.RotateEnvironment(ctx, f.initial, "clock-root-rotate-x"))
			revoked, err := a.workflow.RevokeSelf(ctx, "clock-root-revoke")
			if err != nil || !revoked.Completed {
				t.Fatal("synthetic root revocation failed", err)
			}
			e := newMobileManagerActor(t, f)
			if tc.lag != 0 {
				e.config.Now = func() time.Time { return time.Now().Add(-tc.lag) }
				e.workflow.Close()
				e.workflow = environmentValue(mobileworkflow.New(e.config))
			}
			if err = e.workflow.Login(ctx, f.email, f.password); err != nil {
				t.Fatal(err)
			}
			old, _, err := e.workflow.BeginRecoveryAuthoritySession(ctx, f.code)
			if err != nil {
				t.Fatal(err)
			}
			defer old.Close()
			code := environmentValue(e.workflow.BeginRecoveryAuthorityTransition(ctx, "clock-original-transition"))
			var lost atomic.Bool
			lost.Store(true)
			var attempts atomic.Int64
			var expiry atomic.Int64
			registerCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
				if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovery-authority-transitions") && r.StatusCode == 200 && lost.Swap(false) {
					authorityLostResponse(r)
				}
				if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovered-devices") {
					attempts.Add(1)
				}
				if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovered-device-challenges") && r.StatusCode == 200 {
					data, err := io.ReadAll(r.Body)
					if err != nil {
						return err
					}
					_ = r.Body.Close()
					r.Body = io.NopCloser(strings.NewReader(string(data)))
					var challenge struct {
						ExpiresAt int64 `json:"expiresAt"`
					}
					if err = json.Unmarshal(data, &challenge); err != nil {
						return err
					}
					expiry.Store(challenge.ExpiresAt)
					if tc.cancel {
						time.AfterFunc(100*time.Millisecond, cancel)
					}
				}
				return nil
			}})
			// Full-code entry naturally consumes time. This deliberately isolates the
			// immediate registration horizon from the separate old-transition signer.
			if tc.lag != 0 {
				time.Sleep(tc.lag + time.Second)
			}
			_, _, err = e.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
			if !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
				t.Fatal("accepted loss did not remain pending", err)
			}
			e.reopen(t)
			owner, info, err := e.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
			code = ""
			if err != nil || info.TrustedDevice {
				t.Fatal("original transition did not remain restricted", err)
			}
			defer owner.Close()
			binding := environmentValue(owner.Binding())
			e.reopen(t)
			if err = e.workflow.AttachRecoverySession(owner); err != nil {
				t.Fatal(err)
			}
			_ = environmentValue(e.workflow.RecoveryView())
			e.reopen(t)
			if _, err = e.workflow.View(); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
				t.Fatal("ordinary view bypassed restricted recovery", err)
			}
			e.reopen(t)
			if err = e.workflow.AttachRecoverySession(owner); err != nil {
				t.Fatal(err)
			}
			choices := []mobileworkflow.RecoveryDeviceSelection{{EnvironmentID: f.initial, Role: "ro", ExpiresAt: strconv.FormatInt(time.Now().Unix()+3600, 10)}, {EnvironmentID: y, Role: "admin", ExpiresAt: "0"}}
			out, err := e.workflow.RegisterRecoveredDevice(registerCtx, "clock-register-e", choices)
			if tc.cancel {
				if err == nil || out.TrustedDevice || attempts.Load() != 0 {
					t.Fatal("canceled challenge posted or became trusted", err)
				}
				if _, err = owner.Binding(); !errors.Is(err, mobileworkflow.ErrRecoverySession) {
					t.Fatal("cancellation retained signing owner", err)
				}
				if info, err := e.workflow.RecoveredDeviceInfo(); err != nil || info.TrustedDevice || info.State != "none" {
					t.Fatal("canceled registration created a journal", err)
				}
				return
			}
			if err != nil || !out.TrustedDevice || attempts.Load() != 1 {
				t.Fatal("bounded local-clock wait failed real registration", err)
			}
			var saved struct {
				RecoveredDevice *struct {
					Packet struct {
						Enrollment struct {
							ExpiresAt string `json:"expiresAt"`
						} `json:"enrollment"`
					} `json:"packet"`
					Applied bool `json:"applied"`
				} `json:"recoveredDevice"`
			}
			if err = json.Unmarshal(e.load(), &saved); err != nil || saved.RecoveredDevice == nil || !saved.RecoveredDevice.Applied || saved.RecoveredDevice.Packet.Enrollment.ExpiresAt != strconv.FormatInt(expiry.Load(), 10) {
				t.Fatal("registration changed original signed challenge expiry or lost final seal", err)
			}
			if binding.ExpiresAt > time.Now().Add(-tc.lag).Unix()+300 {
				t.Fatal("process owner deadline was extended")
			}
			t.Log("synthetic XKV2/Y, all old devices revoked, original transition restart, per-operation native context, explicit XRO/YAdmin: original expiry preserved and one accepted POST")
		})
	}
}

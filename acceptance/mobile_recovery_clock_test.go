package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/harmonia-vault/core-go/mobileworkflow"
)

func TestMobileRecoveryPureRollbackDurablyClosesCacheAndRetainsOnlyOriginalJournal(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "restricted-cache"
		if pending {
			name = "signed-unknown-original-journal"
		}
		t.Run(name, func(t *testing.T) {
			f := newRealRecoveryFixture(t)
			ctx := context.Background()
			code := ""
			if pending {
				code = environmentValue(f.workflow.BeginRecoveryRotation(ctx, "recovery-pure-rollback-original"))
				f.loss.Store(3)
				if _, err := f.workflow.CompleteRecoveryRotation(ctx, code); !errors.Is(err, mobileworkflow.ErrRecoveryPending) {
					t.Fatal(err)
				}
			}
			_, before := f.counts()
			f.clockOffset.Store(-10)
			if _, err := f.workflow.RecoveryInfo(); !errors.Is(err, mobileworkflow.ErrRecoveryExpired) {
				t.Fatal("pure rollback was not rejected", err)
			}
			f.clockOffset.Store(0)
			if _, err := f.workflow.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoveryExpired) {
				t.Fatal("clock recovery reopened cached plaintext", err)
			}
			f.reopen()
			if _, err := f.workflow.RecoveryView(); !errors.Is(err, mobileworkflow.ErrRecoveryExpired) {
				t.Fatal("native sealed restart reopened plaintext", err)
			}
			var state struct {
				Recovery struct {
					Closed bool              `json:"sessionClosed"`
					Token  string            `json:"sessionToken"`
					Keys   map[string]string `json:"keys"`
					Vault  struct {
						Events []json.RawMessage `json:"events"`
					} `json:"vault"`
					Rotation *struct {
						Proposal struct {
							ID string `json:"idempotencyKey"`
						} `json:"proposal"`
						Signature string `json:"signature"`
					} `json:"rotation"`
				} `json:"recovery"`
			}
			if err := json.Unmarshal(f.load(), &state); err != nil || !state.Recovery.Closed || state.Recovery.Token != "" || len(state.Recovery.Keys) != 0 || len(state.Recovery.Vault.Events) != 0 {
				t.Fatal("native durable token/cache wipe failed", err)
			}
			if pending {
				if state.Recovery.Rotation == nil || state.Recovery.Rotation.Proposal.ID != "recovery-pure-rollback-original" || state.Recovery.Rotation.Signature == "" {
					t.Fatal("unknown original signed journal lost")
				}
				if _, err := f.workflow.CompleteRecoveryRotation(ctx, code); !errors.Is(err, mobileworkflow.ErrRecoveryExpired) {
					t.Fatal("clock recovery reopened original POST", err)
				}
			}
			_, after := f.counts()
			if after != before {
				t.Fatal("closed session submitted another POST", before, after)
			}
		})
	}
}

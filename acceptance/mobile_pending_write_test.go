package acceptance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
)

func pendingBusinessByID(t *testing.T, w *mobileworkflow.Workflow, id string) mobileworkflow.PendingBusinessInfo {
	t.Helper()
	records, err := w.PendingBusinessOperations()
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range records {
		data := environmentValue(json.Marshal(r))
		var fields map[string]json.RawMessage
		originMust(t, json.Unmarshal(data, &fields))
		if len(fields) != 6 {
			t.Fatal("pending metadata contains unexpected fields")
		}
		for _, field := range []string{"id", "operation", "environmentId", "state", "sequence", "applied"} {
			if _, ok := fields[field]; !ok {
				t.Fatal("pending metadata schema changed")
			}
		}
		if r.ID == id {
			return r
		}
	}
	t.Fatal("original protected pending ID absent")
	return mobileworkflow.PendingBusinessInfo{}
}

// 只用真实初始化的首根手机、独立SQLite与HTTPS；newAPI不读取Dart替代输入。
func TestMobileBusinessPendingMetadataAndOriginalIDRetryAfterReopen(t *testing.T) {
	f := newMobileManagerFixture(t)
	a := f.rootActor
	ctx := context.Background()
	var mutations atomic.Int64
	var lose atomic.Bool
	lose.Store(true)
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/mutations") {
			mutations.Add(1)
			if r.StatusCode == 200 && lose.Swap(false) {
				authorityLostResponse(r)
			}
		}
		return nil
	}})
	const writeID = "pending-business-original-put"
	if _, err := a.workflow.SetVariable(ctx, f.initial, "SYNTHETIC_PENDING_ONLY", "synthetic-pending-accepted-value", writeID); !errors.Is(err, syncclient.ErrWritePending) {
		t.Fatal("accepted lost response not reported pending", err)
	}
	a.reopen(t)
	pending := pendingBusinessByID(t, a.workflow, writeID)
	if pending.Operation != "put" || pending.EnvironmentID != f.initial || pending.Applied || pending.Sequence != 0 || pending.State != "unknown" {
		t.Fatal("restart guessed original write outcome", pending)
	}
	applied, err := a.workflow.RetryBusinessOperationByID(ctx, writeID)
	if err != nil || !applied.Applied || applied.State != "applied" || applied.Sequence == 0 || applied.ID != writeID || mutations.Load() != 1 {
		t.Fatal("sameID retry replaced original or failed verified apply", applied, err)
	}
	a.reopen(t)
	applied, err = a.workflow.RetryBusinessOperationByID(ctx, writeID)
	if err != nil || !applied.Applied || mutations.Load() != 1 {
		t.Fatal("already sealed original ID falsely not found or rewritten", applied, err)
	}
	view := environmentValue(a.workflow.View())
	if len(view.Environments) != 1 || view.Environments[0].Variables["SYNTHETIC_PENDING_ONLY"] != "synthetic-pending-accepted-value" {
		t.Fatal("original writer retry bypassed or failed common pull")
	}
	const envID = "pending-business-original-create"
	f.loseEnvironment.Store(true)
	if _, err = a.workflow.CreateEnvironment(ctx, "合成重启后原包环境", envID); err == nil {
		t.Fatal("accepted lost environment response reported success")
	}
	a.reopen(t)
	pending = pendingBusinessByID(t, a.workflow, envID)
	if pending.Operation != "create" || pending.EnvironmentID == "" || pending.Applied || pending.State != "unknown" {
		t.Fatal("restart environment metadata guessed success", pending)
	}
	applied, err = a.workflow.RetryBusinessOperationByID(ctx, envID)
	if err != nil || !applied.Applied || applied.State != "applied" || applied.EnvironmentID != pending.EnvironmentID || f.environmentPosts.Load() != 1 {
		t.Fatal("original environment signed origin retry changed or rewrote package", applied, err)
	}
	a.reopen(t)
	records := environmentValue(a.workflow.PendingBusinessOperations())
	if len(records) != 0 {
		t.Fatal("sealed applied operations retained as pending", records)
	}
	view = environmentValue(a.workflow.View())
	found := false
	for _, env := range view.Environments {
		if env.ID == pending.EnvironmentID && env.Name == "合成重启后原包环境" {
			found = true
		}
	}
	if !found || len(view.Environments) != 2 {
		t.Fatal("environment retry failed same verified pull/label or created duplicate")
	}
	if _, err = a.workflow.RetryBusinessOperationByID(ctx, "unknown-original-ID"); !errors.Is(err, syncclient.ErrWriteConflict) {
		t.Fatal("unrecorded ID created a new operation", err)
	}
	if mutations.Load() != 1 || f.environmentPosts.Load() != 1 {
		t.Fatal("metadata/retry introduced additional writes")
	}
}

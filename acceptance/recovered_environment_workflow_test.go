package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/pairing"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 本轮只调用手机高层公开CRUD，不用手签包替代产品环境journal。
func TestRecoveredMobileEnvironmentCRUDOriginalJournalAndFinalSave(t *testing.T) {
	if !pairing.NativeAvailable() {
		t.Skip("实际恢复源需原生BoringSSL构建")
	}
	ctx := context.Background()
	f := newMobileManagerFixture(t)
	e := newMobileManagerActor(t, f)
	originMust(t, e.workflow.Login(ctx, f.email, f.password))
	old, _, err := e.workflow.BeginRecoveryAuthoritySession(ctx, f.code)
	originMust(t, err)
	defer old.Close()
	code := environmentValue(e.workflow.BeginRecoveryAuthorityTransition(ctx, "native-environment-transition"))
	fresh, _, err := e.workflow.CompleteRecoveryAuthorityTransition(ctx, code)
	code = ""
	originMust(t, err)
	defer fresh.Close()
	_ = environmentValue(e.workflow.RegisterRecoveredDevice(ctx, "native-environment-register", []mobileworkflow.RecoveryDeviceSelection{{EnvironmentID: f.initial, Role: "admin", ExpiresAt: "0"}}))
	const createID = "native-environment-create-original"
	var lose atomic.Bool
	lose.Store(true)
	var posts atomic.Int64
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.StatusCode == 200 && r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/environment-changes-v3") {
			posts.Add(1)
			if lose.Swap(false) {
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
	if _, err = e.workflow.CreateEnvironment(ctx, "synthetic-recovered-Z", createID); err == nil {
		t.Fatal("lost accepted create reported successful high-level view")
	}
	original := recoveredEnvironmentJournal(t, e.load(), createID)
	if original.Capability != cryptox.RecoveryAuthorityCapability || original.Proof == nil || original.Applied {
		t.Fatal("unknown create missing exact protected Proof3 pending journal")
	}
	e.reopen(t)
	view := environmentValue(e.workflow.CreateEnvironment(ctx, "synthetic-recovered-Z", createID))
	current := recoveredEnvironmentJournal(t, e.load(), createID)
	if !current.Applied || !bytes.Equal(original.Packet, current.Packet) || posts.Load() != 1 {
		t.Fatal("high-level unknown retry changed original source/package or repeated accepted POST")
	}
	f.responseHook.Store(nil)
	environment := ""
	for _, item := range view.Environments {
		if item.Name == "synthetic-recovered-Z" {
			environment = item.ID
		}
	}
	if environment == "" {
		t.Fatal("high-level decrypted environment label missing")
	}
	_ = environmentValue(e.workflow.SetVariable(ctx, environment, "SYNTHETIC_NATIVE_VALUE", "synthetic-native-before-rotate", "native-environment-value"))
	// 原生保存失败发生在已验接受下发之后；不能对UI宣称操作已持久完成。
	const rotateID = "native-environment-rotate-original"
	baseSave := e.config.SaveProtectedState
	var failFinal atomic.Bool
	failFinal.Store(true)
	e.config.SaveProtectedState = func(data []byte) error {
		var state struct {
			Writes map[string]struct {
				Applied bool `json:"applied"`
			} `json:"environmentWrites"`
		}
		if json.Unmarshal(data, &state) != nil {
			return errors.New("synthetic native state invalid")
		}
		if failFinal.Load() && state.Writes[rotateID].Applied {
			return errors.New("synthetic final environment save rejected")
		}
		return baseSave(data)
	}
	e.reopen(t)
	if _, err = e.workflow.RotateEnvironment(ctx, environment, rotateID); !errors.Is(err, syncclient.ErrAcceptedNotApplied) {
		t.Fatal("native final save failure lost accepted-not-applied classification", err)
	}
	pending := recoveredEnvironmentJournal(t, e.load(), rotateID)
	if pending.Applied || pending.Capability != cryptox.RecoveryAuthorityCapability {
		t.Fatal("failed native final save recorded applied")
	}
	e.reopen(t)
	failFinal.Store(false)
	_ = environmentValue(e.workflow.RotateEnvironment(ctx, environment, rotateID))
	applied := recoveredEnvironmentJournal(t, e.load(), rotateID)
	if !applied.Applied || !bytes.Equal(pending.Packet, applied.Packet) {
		t.Fatal("final-save resume regenerated original signed rotation")
	}
	view = environmentValue(e.workflow.RenameEnvironment(ctx, environment, "synthetic-native-renamed", "native-environment-rename"))
	found := false
	for _, item := range view.Environments {
		if item.ID == environment && item.Name == "synthetic-native-renamed" && item.Variables["SYNTHETIC_NATIVE_VALUE"] == "synthetic-native-before-rotate" {
			found = true
		}
	}
	if !found {
		t.Fatal("high-level rotate/rename omitted exact live data or label")
	}
	_ = environmentValue(e.workflow.DeleteVariable(ctx, environment, "SYNTHETIC_NATIVE_VALUE", "native-environment-delete-value"))
	view = environmentValue(e.workflow.DeleteEnvironment(ctx, environment, "native-environment-delete"))
	for _, item := range view.Environments {
		if item.ID == environment {
			t.Fatal("high-level delete retained environment cache")
		}
	}
	e.reopen(t)
	_ = environmentValue(e.workflow.Pull(ctx))
	t.Log("实际恢复E手机高层创建/504原journal New恢复→变量写→轮换/原生finalSave失败原包恢复→重命名/删变量/删环境→重开验证通过")
}

type recoveredEnvironmentJournalView struct {
	Packet     json.RawMessage
	Proof      *cryptox.IssuerRecoveryProof
	Capability string
	Applied    bool
}

func recoveredEnvironmentJournal(t *testing.T, data []byte, id string) recoveredEnvironmentJournalView {
	t.Helper()
	defer clear(data)
	var state struct {
		Writes map[string]struct {
			Applied bool `json:"applied"`
			Origin  *struct {
				Packet     json.RawMessage              `json:"packet"`
				Proof      *cryptox.IssuerRecoveryProof `json:"recoveryControl"`
				Capability string                       `json:"capability"`
			} `json:"originV2"`
		} `json:"environmentWrites"`
	}
	originMust(t, json.Unmarshal(data, &state))
	record, ok := state.Writes[id]
	if !ok || record.Origin == nil {
		t.Fatal("native environment original source journal absent")
	}
	return recoveredEnvironmentJournalView{Packet: bytes.Clone(record.Origin.Packet), Proof: record.Origin.Proof, Capability: record.Origin.Capability, Applied: record.Applied}
}

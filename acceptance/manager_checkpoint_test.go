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

	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 真正服务端接受整批轮换，但合成代理故意漏一条已知内部写入。
// 不裁剪其它字段，不假造签名，不碰宿主环境。
func TestMobileManagerPartialRotationCannotAdvanceOrClaimApplied(t *testing.T) {
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	before, err := f.root.SetVariable(ctx, f.initial, "CHECKPOINT_SECOND", "synthetic-second", "checkpoint-second")
	if err != nil {
		t.Fatal(err)
	}
	var omitted atomic.Int64
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.StatusCode != 200 || r.Request.Method != "GET" || !strings.HasSuffix(r.Request.URL.Path, "/pull") || r.Request.URL.Query().Get("scope") != "" {
			return nil
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		_ = r.Body.Close()
		var body map[string]json.RawMessage
		if err = json.Unmarshal(raw, &body); err != nil {
			return err
		}
		var events []json.RawMessage
		if err = json.Unmarshal(body["events"], &events); err != nil {
			return err
		}
		kept := make([]json.RawMessage, 0, len(events))
		for _, rawEvent := range events {
			var event syncclient.Event
			if err = json.Unmarshal(rawEvent, &event); err != nil {
				return err
			}
			if event.Mutation.Mutation.KeyVersion == "2" && event.Mutation.Mutation.Name == "CHECKPOINT_SECOND" {
				omitted.Add(1)
				continue
			}
			kept = append(kept, rawEvent)
		}
		body["events"], err = json.Marshal(kept)
		if err != nil {
			return err
		}
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		r.ContentLength = int64(len(raw))
		r.Header.Set("Content-Length", strconv.Itoa(len(raw)))
		return nil
	}})
	posts := f.environmentPosts.Load()
	if _, err = f.root.RotateEnvironment(ctx, f.initial, "checkpoint-partial-rotation"); !errors.Is(err, syncclient.ErrAcceptedNotApplied) {
		t.Fatal("partial rotation was claimed complete", err)
	}
	if omitted.Load() == 0 || f.environmentPosts.Load() != posts+1 {
		t.Fatal("fault did not run against exactly one accepted POST")
	}
	if _, err = f.root.Pull(ctx); err == nil {
		t.Fatal("repeat partial pull was accepted")
	}
	state, err := f.root.ExportProtectedState()
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Cloud struct {
			Cloud struct {
				Sequence uint64 `json:"sequence"`
			} `json:"cloud"`
		} `json:"cloud"`
		Writes map[string]struct {
			Sequence uint64 `json:"sequence"`
			Applied  bool   `json:"applied"`
		} `json:"environmentWrites"`
	}
	if err = json.Unmarshal(state, &saved); err != nil {
		t.Fatal(err)
	}
	record := saved.Writes["checkpoint-partial-rotation"]
	if record.Sequence == 0 || record.Applied || saved.Cloud.Cloud.Sequence != before.Checkpoint {
		t.Fatal("partial batch advanced durable data or applied journal")
	}
	// 被标成applied的坏本机日志也不能在重启时偷偷开放。
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(state, &fields); err != nil {
		t.Fatal(err)
	}
	var writes map[string]map[string]json.RawMessage
	if err = json.Unmarshal(fields["environmentWrites"], &writes); err != nil {
		t.Fatal(err)
	}
	writes["checkpoint-partial-rotation"]["applied"] = json.RawMessage("true")
	fields["environmentWrites"], err = json.Marshal(writes)
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.rootActor.config
	cfg.ProtectedState, err = json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	if bad, err := mobileworkflow.New(cfg); err == nil {
		bad.Close()
		t.Fatal("bad applied batch restored")
	}
	// 完整下发后只查同一原id；不新签、不重新POST，密封重启仍验证。
	f.responseHook.Store(nil)
	after, err := f.root.RotateEnvironment(ctx, f.initial, "checkpoint-partial-rotation")
	if err != nil {
		t.Fatal("original batch could not resume", err)
	}
	if after.Checkpoint <= before.Checkpoint || f.environmentPosts.Load() != posts+1 {
		t.Fatal("resume changed transaction or failed to apply")
	}
	f.rootActor.reopen(t)
	f.root = f.rootActor.workflow
	if _, err = f.root.View(); err != nil {
		t.Fatal("exact applied checkpoint did not reopen", err)
	}
}

// 已经见过的事务指纹不能替代本次full响应中缺失的数据。
func TestMobileManagerKnownRotationFullPullCannotDropDeclaredMutation(t *testing.T) {
	f := newMobileManagerFixture(t)
	ctx := context.Background()
	if _, err := f.root.SetVariable(ctx, f.initial, "KNOWN_SECOND", "synthetic-known", "known-second"); err != nil {
		t.Fatal(err)
	}
	before, err := f.root.RotateEnvironment(ctx, f.initial, "known-complete-rotation")
	if err != nil {
		t.Fatal(err)
	}
	var omitted atomic.Int64
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
		if r.StatusCode != 200 || r.Request.Method != "GET" || !strings.HasSuffix(r.Request.URL.Path, "/pull") || r.Request.URL.Query().Get("scope") != "" || r.Request.URL.Query().Get("after") != "0" {
			return nil
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			return err
		}
		_ = r.Body.Close()
		var body map[string]json.RawMessage
		if err = json.Unmarshal(raw, &body); err != nil {
			return err
		}
		var events []json.RawMessage
		if err = json.Unmarshal(body["events"], &events); err != nil {
			return err
		}
		kept := make([]json.RawMessage, 0, len(events))
		for _, rawEvent := range events {
			var event syncclient.Event
			if err = json.Unmarshal(rawEvent, &event); err != nil {
				return err
			}
			if event.Mutation.Mutation.KeyVersion == "2" && event.Mutation.Mutation.Name == "KNOWN_SECOND" {
				omitted.Add(1)
				continue
			}
			kept = append(kept, rawEvent)
		}
		body["events"], err = json.Marshal(kept)
		if err != nil {
			return err
		}
		raw, err = json.Marshal(body)
		if err != nil {
			return err
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		r.ContentLength = int64(len(raw))
		r.Header.Set("Content-Length", strconv.Itoa(len(raw)))
		return nil
	}})
	// 新环境要求同一Full历史；代理仅漏原来已验证的X批次一项。
	if _, err = f.root.CreateEnvironment(ctx, "完整响应检查", "known-full-create"); !errors.Is(err, syncclient.ErrAcceptedNotApplied) {
		t.Fatal("full response used an old fingerprint for omitted transaction data", err)
	}
	if omitted.Load() == 0 {
		t.Fatal("known-data omission was not exercised")
	}
	state, err := f.root.ExportProtectedState()
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Cloud struct {
			Cloud struct {
				Sequence uint64 `json:"sequence"`
			} `json:"cloud"`
		} `json:"cloud"`
	}
	if err = json.Unmarshal(state, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Cloud.Cloud.Sequence != before.Checkpoint {
		t.Fatal("partial full pull advanced durable data")
	}
	f.responseHook.Store(nil)
	if _, err = f.root.CreateEnvironment(ctx, "完整响应检查", "known-full-create"); err != nil {
		t.Fatal("exact original request could not resume", err)
	}
	view, err := f.root.View()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range view.Environments {
		if e.ID == f.initial && e.Variables["KNOWN_SECOND"] == "synthetic-known" {
			found = true
		}
	}
	if !found {
		t.Fatal("full retry lost authenticated original data")
	}
}

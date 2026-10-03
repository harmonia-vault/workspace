package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/mobileworkflow"
	"github.com/harmonia-vault/core-go/syncclient"
)

// Public signed packets only; the real preparation uses the existing Go crypto
// and TS service. This fixture is not a production packet injection API.
type s2aPreparationJournal struct {
	mu      sync.Mutex
	binding syncclient.DAGJournalBinding
	raw     []byte
}

func (s *s2aPreparationJournal) LoadDAGJournal(b syncclient.DAGJournalBinding) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b != s.binding {
		return nil, syncclient.ErrDAGJournalConflict
	}
	if s.raw == nil {
		return nil, os.ErrNotExist
	}
	return bytes.Clone(s.raw), nil
}
func (s *s2aPreparationJournal) CompareAndSwapDAGJournal(b syncclient.DAGJournalBinding, old, next []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b != s.binding || !syncclient.EqualDAGJournalBytes(s.raw, old) {
		return syncclient.ErrDAGJournalConflict
	}
	s.raw = bytes.Clone(next)
	return nil
}

type s2aRoutes struct {
	mu     sync.Mutex
	phase  string
	counts map[string]map[string]int
}

func (s *s2aRoutes) set(phase string) { s.mu.Lock(); s.phase = phase; s.mu.Unlock() }
func (s *s2aRoutes) client(base *http.Client) *http.Client {
	client := *base
	transport := base.Transport.(*http.Transport).Clone()
	transport.Proxy = func(request *http.Request) (*url.URL, error) {
		path := request.URL.Path
		route := "other"
		for _, name := range []string{"protocol-info", "recovery-challenges", "recovery-sessions", "recovery-vault-v2", "recovery-authority-challenges-v2", "recovery-authority-transitions-v2", "recovered-device-challenges-v2", "recovered-devices-v2"} {
			if strings.HasSuffix(path, "/"+name) {
				route = name
				break
			}
			if strings.Contains(path, "/"+name+"/") {
				route = name + "/original"
				break
			}
		}
		s.mu.Lock()
		if s.counts[s.phase] == nil {
			s.counts[s.phase] = map[string]int{}
		}
		s.counts[s.phase][request.Method+" "+route]++
		s.mu.Unlock()
		return nil, nil // isolated verified loopback TLS, no host proxy setting changed
	}
	client.Transport = transport
	return &client
}
func (s *s2aRoutes) assertAndLog(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for phase, routes := range s.counts {
		if phase != "preparation" {
			for route, n := range routes {
				if n > 0 && route != "GET protocol-info" && route != "POST recovery-challenges" && route != "POST recovery-sessions" && route != "GET recovery-vault-v2" && route != "GET recovery-authority-transitions-v2/original" && route != "GET recovered-devices-v2/original" {
					t.Fatal("query attempted forbidden route", phase, route)
				}
			}
		}
	}
	raw, _ := json.Marshal(s.counts)
	t.Log("S2A_ROUTE_COUNTS", string(raw))
}

// Existing synthetic AES callback, guarded by one whole-state lock; only this
// test prepares a record. Query must use CAS, never an ordinary Save fallback.
type s2aNativeState struct {
	mu      sync.Mutex
	save    func([]byte) error
	load    func() []byte
	fail    bool
	commits int
}

func s2aHash(raw []byte) string        { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func (s *s2aNativeState) read() []byte { s.mu.Lock(); defer s.mu.Unlock(); return s.load() }
func (s *s2aNativeState) check(expected string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := s.load()
	defer clear(raw)
	if s2aHash(raw) != expected {
		return syncclient.ErrDAGJournalConflict
	}
	return nil
}
func (s *s2aNativeState) cas(expected string, next []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	raw := s.load()
	defer clear(raw)
	if s2aHash(raw) != expected {
		return syncclient.ErrDAGJournalConflict
	}
	if s.fail {
		return errors.New("synthetic final native CAS failure")
	}
	if err := s.save(next); err != nil {
		return err
	}
	s.commits++
	return nil
}
func s2aMobileOriginal(t *testing.T, f *mobileManagerFixture, client *http.Client, original syncclient.ProtectedDAGOperation) (mobileworkflow.Config, *s2aNativeState) {
	t.Helper()
	a := newMobileManagerActor(t, f)
	raw := environmentValue(a.workflow.ExportProtectedState())
	a.workflow.Close()
	var state map[string]json.RawMessage
	if json.Unmarshal(raw, &state) != nil {
		t.Fatal("synthetic empty native state")
	}
	clear(raw)
	var device, signing, receiving string
	_ = json.Unmarshal(state["deviceId"], &device)
	_ = json.Unmarshal(state["signingPublicKey"], &signing)
	_ = json.Unmarshal(state["receivingPublicKey"], &receiving)
	state["accountId"], _ = json.Marshal(original.AccountID)
	state["accountGeneration"] = json.RawMessage(`"1"`)
	journal, _ := json.Marshal(map[string]any{"ownerEpoch": uint64(0), "operation": original})
	state["recoveryDAG"], _ = json.Marshal(map[string]any{"version": 1, "profile": cryptox.RecoveryDAGCapability, "endpoint": f.proxy.URL, "accountId": original.AccountID, "accountGeneration": uint64(1), "deviceId": device, "signingPublicKey": signing, "receivingPublicKey": receiving, "ownerEpoch": uint64(0), "journal": json.RawMessage(journal)})
	prepared, _ := json.Marshal(state)
	defer clear(prepared)
	save, load := recoveryNativeSeal(t, "synthetic-mobile-dag-s2a-state")
	slot := &s2aNativeState{save: save, load: load}
	if err := save(prepared); err != nil {
		t.Fatal(err)
	}
	c := a.config
	c.HTTPClient = client
	c.ProtectedState = slot.read()
	c.SaveProtectedState = func([]byte) error { return errors.New("S2a must not use ordinary Save") }
	c.SaveProtectedStateCAS = slot.cas
	c.CheckProtectedState = slot.check
	return c, slot
}
func s2aOpen(t *testing.T, c mobileworkflow.Config, slot *s2aNativeState) *mobileworkflow.Workflow {
	t.Helper()
	c.ProtectedState = slot.read()
	w := environmentValue(mobileworkflow.New(c))
	t.Cleanup(w.Close)
	return w
}
func s2aPrepare(t *testing.T, accepted bool) (*mobileManagerFixture, *s2aRoutes, *http.Client, syncclient.ProtectedDAGOperation, string) {
	t.Helper()
	f := newMobileManagerFixture(t)
	routes := &s2aRoutes{phase: "preparation", counts: map[string]map[string]int{}}
	client := routes.client(f.proxy.Client())
	raw := environmentValue(f.root.ExportProtectedState())
	defer clear(raw)
	var state struct {
		AccountID string `json:"accountId"`
	}
	if json.Unmarshal(raw, &state) != nil {
		t.Fatal("synthetic account")
	}
	binding := syncclient.DAGJournalBinding{Endpoint: f.proxy.URL, AccountID: state.AccountID, AccountGeneration: 1}
	journal := environmentValue(syncclient.NewCheckedDAGJournal(&s2aPreparationJournal{binding: binding}, binding))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session := environmentValue(syncclient.OpenDAGRecoverySession(ctx, syncclient.DAGRecoveryConfig{Endpoint: f.proxy.URL, HTTPClient: client, AccountID: state.AccountID, AccountGeneration: 1, Journal: journal}, f.code))
	defer session.Close()
	newCode := environmentValue(session.BeginTransition(ctx, "s2a-original-transition"))
	_, err := session.SealTransition(ctx, newCode)
	if err != nil {
		t.Fatal("preparation seal", err)
	}
	currentCode := f.code
	if accepted {
		f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "POST" && strings.HasSuffix(r.Request.URL.Path, "/recovery-authority-transitions-v2") && r.StatusCode == 200 {
				_ = r.Body.Close()
				r.StatusCode = 504
				r.Body = io.NopCloser(strings.NewReader(`{"error":"synthetic_lost_original_response"}`))
				r.ContentLength = -1
				r.Header.Del("Content-Length")
			}
			return nil
		}})
		_, err = session.RetryTransition(ctx)
		if !errors.Is(err, syncclient.ErrEnrollmentPending) {
			t.Fatal("accepted response loss not retained", err)
		}
		f.responseHook.Store(nil)
		currentCode = newCode
	}
	original := environmentValue(journal.Load())
	if original.AcceptedSequence != 0 || original.Applied {
		t.Fatal("fixture must retain uncertain original")
	}
	session.Close() // actual original bearer/keys owner is destroyed before mobile query
	return f, routes, client, original, currentCode
}

func TestMobileDAGOriginalQueryHTTPS(t *testing.T) {
	t.Run("accepted-lost-response-native-failure-and-reopen", func(t *testing.T) {
		f, routes, client, original, currentCode := s2aPrepare(t, true)
		defer routes.assertAndLog(t)
		config, slot := s2aMobileOriginal(t, f, client, original)
		before := slot.read()
		w := s2aOpen(t, config, slot)
		slot.fail = true
		routes.set("query-final-save-failure")
		result, err := w.QueryRecoveryDAGOriginal(context.Background(), []byte(currentCode))
		if !errors.Is(err, mobileworkflow.ErrDAGPersistence) || result.Confirmation != "receipt-observed" || result.TrustedDevice || !bytes.Equal(before, slot.read()) {
			t.Fatal("accepted save failure pretended durable confirmation", err)
		}
		w.Close()
		slot.fail = false
		w = s2aOpen(t, config, slot)
		routes.set("query-accepted-after-reopen")
		result, err = w.QueryRecoveryDAGOriginal(context.Background(), []byte(currentCode))
		if err != nil || result.Observation != "accepted" || result.Confirmation != "original-verified-and-saved" || !result.RotationRequired || result.TrustedDevice || result.Pending.OperationID != original.OperationID || result.Pending.ContentHash != original.ContentHash || !result.Pending.OriginalApplied {
			t.Fatal("fresh session did not confirm only original", err)
		}
		w.Close()
		w = s2aOpen(t, config, slot)
		persisted := environmentValue(w.RecoveryDAGPendingInfo())
		if persisted.Acceptance != "accepted" || persisted.TrustedDevice || persisted.OperationID != original.OperationID {
			t.Fatal("original confirmation lost after reopen")
		}
		if _, err = w.View(); !errors.Is(err, mobileworkflow.ErrRecoveryRestricted) {
			t.Fatal("original confirmation opened ordinary vault", err)
		}
		var vaultReads int
		f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "GET" && strings.HasSuffix(r.Request.URL.Path, "/recovery-vault-v2") {
				vaultReads++
				if vaultReads == 2 {
					body, e := io.ReadAll(r.Body)
					if e != nil {
						return e
					}
					_ = r.Body.Close()
					var vault map[string]json.RawMessage
					if e = json.Unmarshal(body, &vault); e != nil {
						return e
					}
					vault["rotationRequired"] = json.RawMessage(`false`)
					body, e = json.Marshal(vault)
					if e != nil {
						return e
					}
					r.Body = io.NopCloser(bytes.NewReader(body))
					r.ContentLength = -1
					r.Header.Del("Content-Length")
				}
			}
			return nil
		}})
		routes.set("query-fresh-session-false-after-resolve")
		result, err = w.QueryRecoveryDAGOriginal(context.Background(), []byte(currentCode))
		if err == nil || result.Observation != "unknown" || result.TrustedDevice {
			t.Fatal("old receipt cleared fresh rotation requirement", err)
		}
		f.responseHook.Store(nil)
		routes.set("query-wrong-old-code")
		result, err = w.QueryRecoveryDAGOriginal(context.Background(), []byte(f.code))
		if err == nil || result.Observation != "unknown" || result.TrustedDevice {
			t.Fatal("wrong current code became not-accepted", err)
		}
	})
	t.Run("not-accepted-and-unknown-retain-original", func(t *testing.T) {
		f, routes, client, original, currentCode := s2aPrepare(t, false)
		defer routes.assertAndLog(t)
		config, slot := s2aMobileOriginal(t, f, client, original)
		before := slot.read()
		w := s2aOpen(t, config, slot)
		routes.set("query-not-accepted")
		result, err := w.QueryRecoveryDAGOriginal(context.Background(), []byte(currentCode))
		if err != nil || result.Observation != "not-accepted-at-query" || result.Pending.OriginalApplied || !result.RotationRequired || result.TrustedDevice || !bytes.Equal(before, slot.read()) {
			t.Fatal("not-accepted query closed or replaced original", err)
		}
		for _, mode := range []string{"lost-status", "missing-accepted"} {
			routes.set("query-" + mode)
			f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
				if r.Request.Method == "GET" && strings.Contains(r.Request.URL.Path, "/recovery-authority-transitions-v2/") {
					_ = r.Body.Close()
					body := `{"operationId":"` + original.OperationID + `"}`
					if mode == "lost-status" {
						r.StatusCode = 504
						body = `{"error":"synthetic_lost_status"}`
					}
					r.Body = io.NopCloser(strings.NewReader(body))
					r.ContentLength = -1
					r.Header.Del("Content-Length")
				}
				return nil
			}})
			result, err = w.QueryRecoveryDAGOriginal(context.Background(), []byte(currentCode))
			if err == nil || result.Observation != "unknown" || result.TrustedDevice || !bytes.Equal(before, slot.read()) {
				t.Fatal("unknown query became a negative or replaced packet", mode, err)
			}
		}
		f.responseHook.Store(nil)
		w.Close()
		w = s2aOpen(t, config, slot)
		routes.set("query-after-network-restored")
		result, err = w.QueryRecoveryDAGOriginal(context.Background(), []byte(currentCode))
		if err != nil || result.Observation != "not-accepted-at-query" || result.Pending.OperationID != original.OperationID || !bytes.Equal(before, slot.read()) || slot.commits != 0 {
			t.Fatal("reopen failed original-only query", err)
		}
	})
	t.Run("independent-logout-during-original-status", func(t *testing.T) {
		f, routes, client, original, currentCode := s2aPrepare(t, false)
		defer routes.assertAndLog(t)
		config, slot := s2aMobileOriginal(t, f, client, original)
		query := s2aOpen(t, config, slot)
		logoutConfig := config
		logoutConfig.ProtectedState = slot.read()
		expected := s2aHash(logoutConfig.ProtectedState)
		logoutConfig.SaveProtectedState = func(next []byte) error {
			if err := slot.cas(expected, next); err != nil {
				return err
			}
			expected = s2aHash(next)
			return nil
		}
		other := environmentValue(mobileworkflow.New(logoutConfig))
		defer other.Close()
		entered, resume := make(chan struct{}), make(chan struct{})
		var once sync.Once
		release := func() { once.Do(func() { close(resume) }) }
		defer release()
		f.responseHook.Store(&mobileManagerResponseHook{invoke: func(r *http.Response) error {
			if r.Request.Method == "GET" && strings.Contains(r.Request.URL.Path, "/recovery-authority-transitions-v2/") {
				close(entered)
				<-resume
			}
			return nil
		}})
		routes.set("query-independent-logout")
		type answer struct {
			result mobileworkflow.RecoveryDAGQueryResult
			err    error
		}
		done := make(chan answer, 1)
		go func() {
			result, err := query.QueryRecoveryDAGOriginal(context.Background(), []byte(currentCode))
			done <- answer{result, err}
		}()
		select {
		case <-entered:
		case <-time.After(10 * time.Second):
			t.Fatal("query did not reach original GET barrier")
		}
		if err := other.Logout(); err != nil {
			t.Fatal("current native logout", err)
		}
		tombstone := slot.read()
		release()
		select {
		case got := <-done:
			if got.err == nil || got.result.Observation != "unknown" || got.result.TrustedDevice {
				t.Fatal("stale query returned usable result", got.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("query did not leave stale owner")
		}
		if !bytes.Equal(tombstone, slot.read()) {
			t.Fatal("late query replaced logout tombstone")
		}
		var state struct {
			Cloud struct {
				AccountClosed bool   `json:"accountClosed"`
				SessionEpoch  uint64 `json:"sessionEpoch"`
			} `json:"cloud"`
			Recovery json.RawMessage `json:"recoveryDAG"`
		}
		if json.Unmarshal(tombstone, &state) != nil || !state.Cloud.AccountClosed || state.Cloud.SessionEpoch != 1 || len(state.Recovery) != 0 {
			t.Fatal("durable native logout facts missing")
		}
	})
}

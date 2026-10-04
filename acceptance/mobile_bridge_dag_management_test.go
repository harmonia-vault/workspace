//go:build harmonia_boringssl

package acceptance

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/cryptox"
	"github.com/harmonia-vault/core-go/localstate"
	"github.com/harmonia-vault/core-go/mobilebridge"
	"github.com/harmonia-vault/core-go/syncclient"
)

// 仅Go原生测试适配器在RAM解包自己生成的AES状态，以同真实recovered
// 原包建成熟ApproverV5；不向Dart暴露key，也不预造Accepted或跳PAKE。
func dagManagementNativeApprover(t *testing.T, f *mobileManagerFixture, d *mobilebridge.Device, namespace string, packet []byte) *dagAcceptanceActor {
	t.Helper()
	material := environmentValue(d.ExportProtectedMaterial())
	defer clear(material)
	key := ed25519.NewKeyFromSeed(material[8:40])
	receiving := bytes.Clone(material[40:])
	t.Cleanup(func() { clear(key); clear(receiving) })
	x := environmentValue(ecdh.X25519().NewPrivateKey(receiving))
	info := environmentValue(json.Marshal([]string{"harmonia/native-workflow-aes/v1", namespace, f.proxy.URL, cryptox.EncodeBase64(key.Public().(ed25519.PublicKey)), cryptox.EncodeBase64(x.PublicKey().Bytes())}))
	secret := append(bytes.Clone(key[:32]), receiving...)
	defer clear(secret)
	aesKey := environmentValue(hkdf.Key(sha256.New, secret, nil, string(info), 32))
	defer clear(aesKey)
	block := environmentValue(aes.NewCipher(aesKey))
	aead := environmentValue(cipher.NewGCM(block))
	if len(packet) < 40 || string(packet[:8]) != "HARMST01" {
		t.Fatal("native test state format")
	}
	n := int(binary.BigEndian.Uint32(packet[8:12]))
	if n > 4096 || len(packet) < 12+n+12+16 {
		t.Fatal("native test state bound")
	}
	plain := environmentValue(aead.Open(nil, packet[12+n:24+n], packet[24+n:], packet[:12+n]))
	defer clear(plain)
	var state struct {
		AccountID         string           `json:"accountId"`
		AccountGeneration string           `json:"accountGeneration"`
		DeviceID          string           `json:"deviceId"`
		Cloud             localstate.State `json:"cloud"`
		Recovered         struct {
			Original struct {
				Endpoint          string          `json:"endpoint"`
				AccountID         string          `json:"accountId"`
				AccountGeneration uint64          `json:"accountGeneration"`
				OwnerEpoch        uint64          `json:"ownerEpoch"`
				Journal           json.RawMessage `json:"journal"`
			} `json:"original"`
		} `json:"recoveredDAGDevice"`
	}
	originMust(t, json.Unmarshal(plain, &state))
	defer clear(state.Recovered.Original.Journal)
	original := state.Recovered.Original
	op := environmentValue(syncclient.DecodeDAGJournal(syncclient.DAGJournalBinding{Endpoint: original.Endpoint, AccountID: original.AccountID, AccountGeneration: original.AccountGeneration, OwnerEpoch: original.OwnerEpoch}, original.Journal))
	result := environmentValue(syncclient.RecoveredDAGResultFromConfirmedOperation(op))
	verifier := environmentValue(syncclient.NewRecoveredDAGPinnedVerifier(syncclient.RecoveredDAGPinnedTrust{Trust: syncclient.PinnedTrust{AccountID: state.AccountID, AccountGeneration: op.AccountGeneration, DeviceID: state.DeviceID, DeviceSigningPublicKey: key.Public().(ed25519.PublicKey), ReceivingPrivateKey: receiving}, Pin: result.Pin, Evidence: result.Evidence, Accepted: result.Accepted}))
	t.Cleanup(verifier.Close)
	vault := newDAGLabVault(t)
	store := &dagAcceptanceStore{vault: vault}
	originMust(t, store.Save(state.Cloud))
	engine := environmentValue(localstate.New(store))
	client := environmentValue(syncclient.NewForBoot(syncclient.Config{Endpoint: f.proxy.URL, ProtocolMajor: 2, HTTPClient: f.proxy.Client(), AccountID: state.AccountID, AccountGeneration: op.AccountGeneration, DeviceID: state.DeviceID, Verifier: verifier, Engine: engine}))
	client = environmentValue(client.BootDevice(context.Background(), key))
	_ = environmentValue(client.Pull(context.Background()))
	return &dagAcceptanceActor{client: client, verifier: verifier, engine: engine, key: key, receiving: receiving, id: state.DeviceID, vault: vault, result: result}
}

func TestNativeBridgeDAGManagementHTTPS(t *testing.T) {
	f := newMobileManagerFixture(t)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.proxy.Certificate().Raw})
	d := environmentValue(mobilebridge.NewDevice())
	defer func() { d.Close() }()
	registry := environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.dag.management", "workflow", 151))
	defer func() { registry.Close() }()
	slot := &dagEnvironmentBridgeSlot{}
	namespace := "synthetic.dag.management\x00harmonia/workflow-state/v1\x00workflow"
	open := func() *mobilebridge.VaultWorkflow {
		captured := slot.read()
		defer clear(captured)
		v := environmentValue(d.OpenAtomicWorkflow(f.proxy.URL, namespace, captured, ca, slot))
		if e := v.AttachDAGRegistry(registry); e != nil {
			v.Close()
			t.Fatal(e)
		}
		return v
	}
	command := func(op string, fields map[string]string) string {
		out := map[string]any{"version": 1, "endpoint": f.proxy.URL, "operation": op}
		for k, v := range fields {
			out[k] = v
		}
		raw, _ := json.Marshal(out)
		return string(raw)
	}
	decode := func(raw string) map[string]any {
		var out map[string]any
		if json.Unmarshal([]byte(raw), &out) != nil {
			t.Fatal("strict bridge result")
		}
		return out
	}
	recovery := func(op string, fields map[string]string, code []byte) map[string]any {
		v := open()
		defer v.Close()
		raw, e := v.ExecuteDAGRecovery(command(op, fields), code)
		if e != nil {
			t.Fatal("real recovery prerequisite", op, e)
		}
		out := decode(raw)
		if out["ok"] != true {
			t.Fatal("recovery incomplete", op)
		}
		return out
	}
	recovery("openDAGRecoveryOwner", map[string]string{"email": f.email, "password": f.password}, []byte(f.code))
	code := recovery("beginDAGRecoveryTransition", nil, nil)["recoveryCode"].(string)
	recovery("sealDAGRecoveryTransition", nil, []byte(code))
	code = ""
	recovery("retryDAGRecoveryTransition", nil, nil)
	choices := recovery("dagRecoveredEnrollmentChoices", nil, nil)["data"].(map[string]any)
	expiry := strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)
	selections := []map[string]string{}
	for _, raw := range choices["environments"].([]any) {
		row := raw.(map[string]any)
		selections = append(selections, map[string]string{"environmentId": row["environmentId"].(string), "keyVersion": row["keyVersion"].(string), "role": "admin", "expiresAt": expiry})
	}
	selected, _ := json.Marshal(selections)
	sealed := recovery("sealDAGRecoveredDevice", map[string]string{"expectedSequence": choices["sequence"].(string), "recoveryHeadHash": choices["recoveryHeadHash"].(string), "selections": string(selected)}, nil)["data"].(map[string]any)
	original := map[string]string{"operationId": sealed["operationId"].(string), "contentHash": sealed["contentHash"].(string)}
	recovery("retryDAGRecoveredDevice", original, nil)
	recovery("applyDAGRecoveredDevice", original, nil)
	manager := dagManagementNativeApprover(t, f, d, namespace, slot.read())
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()
	reader := dagEnvironmentEnrollRO(t, ctx, f, manager, f.initial)
	t.Log("正式恢复Boot/P4/finalCAS + 真实V5 PAKE RO设备已完成")
	var posts, mutationPosts atomic.Int32
	var lose atomic.Bool
	f.responseHook.Store(&mobileManagerResponseHook{invoke: func(resp *http.Response) error {
		if resp.Request.Method == "POST" && strings.HasSuffix(resp.Request.URL.Path, "/mutations") {
			mutationPosts.Add(1)
		}
		if resp.Request.Method == "POST" && strings.HasSuffix(resp.Request.URL.Path, "/grants") {
			posts.Add(1)
			if resp.StatusCode == 200 && lose.Swap(false) {
				b2LoseResponse(resp)
			}
		}
		return nil
	}})
	management := func(op string, fields map[string]string) (map[string]any, error) {
		started := time.Now()
		v := open()
		defer v.Close()
		raw, e := v.ExecuteDAGManagement(command(op, fields))
		t.Log("管理操作耗时", op, time.Since(started).Round(time.Millisecond))
		if e != nil {
			if raw != "" {
				t.Fatal("hard error leaked view")
			}
			return nil, e
		}
		return decode(raw), nil
	}
	grant := func(id, role, until string) map[string]string {
		return map[string]string{"requestId": id, "environmentId": f.initial, "subjectDeviceId": reader.id, "role": role, "expiresAt": until}
	}
	list := environmentValue(management("dagManagementDevices", map[string]string{"environmentId": f.initial}))
	found := false
	for _, raw := range list["data"].(map[string]any)["devices"].([]any) {
		row := raw.(map[string]any)
		if row["deviceId"] == reader.id {
			found = row["role"] == "ro"
		}
	}
	if !found || list["trustedDevice"] != false {
		t.Fatal("verified reader list")
	}
	if _, e := management("prepareDAGDeviceGrant", grant("management-deny-permanent", "admin", "0")); e == nil || posts.Load() != 0 {
		t.Fatal("temporary Admin widened or POSTed", e)
	}
	until := strconv.FormatInt(time.Now().Add(20*time.Minute).Unix(), 10)
	prepared := environmentValue(management("prepareDAGDeviceGrant", grant("management-rw", "rw", until)))
	if prepared["trustedDevice"] != false || posts.Load() != 0 {
		t.Fatal("prepare POSTed or promoted")
	}
	lose.Store(true)
	unknown := environmentValue(management("retryDAGManagement", map[string]string{"requestId": "management-rw"}))
	if unknown["ok"] != false || unknown["trustedDevice"] != false || posts.Load() != 1 {
		t.Fatal("504 lost original metadata")
	}
	deniedCancel := environmentValue(management("cancelDAGManagement", map[string]string{"requestId": "management-rw"}))
	keptOriginal := deniedCancel["data"].(map[string]any)["original"].(map[string]any)
	if deniedCancel["ok"] != false || deniedCancel["trustedDevice"] != false || keptOriginal["requestId"] != "management-rw" || keptOriginal["attempted"] != true || keptOriginal["canceled"] != false || keptOriginal["state"] != "pending" {
		t.Fatal("attempted unknown original not retained")
	}
	// cold新Device/registry；原保护slot仍在，只有原ID/status续办。
	material := environmentValue(d.ExportProtectedMaterial())
	d.Close()
	registry.Close()
	d = environmentValue(mobilebridge.ImportProtectedMaterial(material))
	clear(material)
	registry = environmentValue(mobilebridge.NewNativeDAGRegistry("synthetic.dag.management", "workflow", 151))
	info := environmentValue(management("dagManagementInfo", nil))["data"].(map[string]any)["management"].(map[string]any)
	if info["requestId"] != "management-rw" || info["state"] != "pending" || info["sequence"] != "0" {
		t.Fatal("cold original changed")
	}
	applied := environmentValue(management("retryDAGManagement", map[string]string{"requestId": "management-rw"}))
	if applied["trustedDevice"] != true || posts.Load() != 1 || applied["data"].(map[string]any)["management"].(map[string]any)["applied"] != true {
		t.Fatal("receipt retry did not finalCAS or reposted")
	}
	_ = environmentValue(reader.client.Pull(ctx))
	if reader.engine.State().Cloud.Environments[f.initial].Role != localstate.ReadWrite {
		t.Fatal("RO to RW absent")
	}
	writer := environmentValue(syncclient.NewWriter(manager.result.Accepted.Submission.Enrollment.AccountID, 1, reader.id, reader.engine.State().SessionEpoch, reader.key, dagAcceptanceWriteJournal{reader.vault}))
	defer writer.Close()
	if !environmentValue(writer.Execute(ctx, reader.client, syncclient.WriteRequest{ID: "management-reader-write", Operation: "put", EnvironmentID: f.initial, Name: "SYNTHETIC_MANAGED", Value: "synthetic-managed-value"})).Applied {
		t.Fatal("RW HPKE write not applied")
	}
	t.Log("504/原IDcold续办无重POST + RW实际HPKE/Pull/write已完成")
	for i, role := range []string{"admin", "ro", "none"} {
		id := "management-role-" + role
		value := until
		if role == "none" {
			value = "0"
		}
		_ = environmentValue(management("prepareDAGDeviceGrant", grant(id, role, value)))
		out := environmentValue(management("retryDAGManagement", map[string]string{"requestId": id}))
		if out["trustedDevice"] != true || posts.Load() != int32(i+2) {
			t.Fatal("role not applied", role)
		}
		_ = environmentValue(reader.client.Pull(ctx))
		if role == "none" {
			if len(reader.engine.State().Cloud.Environments) != 0 {
				t.Fatal("none resurrected environment")
			}
		} else {
			expected := localstate.Admin
			if role == "ro" {
				expected = localstate.ReadOnly
			}
			if reader.engine.State().Cloud.Environments[f.initial].Role != expected {
				t.Fatal("role missing", role)
			}
			if role == "ro" {
				before := mutationPosts.Load()
				if _, e := writer.Execute(ctx, reader.client, syncclient.WriteRequest{ID: "management-RO-denied", Operation: "put", EnvironmentID: f.initial, Name: "SYNTHETIC_DENIED", Value: "synthetic-denied"}); !errors.Is(e, syncclient.ErrWritePermission) || mutationPosts.Load() != before {
					t.Fatal("RO writer did not deny", e)
				}
			}
		}
	}
	// 未尝试取消永久退休本地原ID，不宣称server closed。
	_ = environmentValue(management("prepareDAGDeviceGrant", grant("management-local-cancel", "ro", until)))
	canceled := environmentValue(management("cancelDAGManagement", map[string]string{"requestId": "management-local-cancel"}))
	if canceled["data"].(map[string]any)["management"].(map[string]any)["canceled"] != true {
		t.Fatal("local ID not retired")
	}
	if _, e := management("prepareDAGDeviceGrant", grant("management-local-cancel", "rw", until)); e == nil {
		t.Fatal("retired ID reused")
	}
	if posts.Load() != 4 {
		t.Fatal("unexpected grant POST")
	}
	t.Log("有限Admin/RO/none + unknown不可取消 + 本地未尝试ID退休已完成")
	// 最后故意CAS失败，只断言0POST；旧registry不可作为后续正常操作owner。
	before := posts.Load()
	beforeSlot := slot.read()
	slot.fail.Store(true)
	if _, e := management("prepareDAGDeviceGrant", grant("management-CAS-denied", "ro", until)); e == nil || posts.Load() != before {
		t.Fatal("native CAS failure sent grant", e)
	}
	if !bytes.Equal(beforeSlot, slot.read()) {
		t.Fatal("failed CAS changed protected state")
	}
	clear(beforeSlot)
}

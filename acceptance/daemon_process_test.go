package acceptance

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/harmonia-vault/core-go/localipc"
)

// 使用真实已入网的受保护目录、独立 CLI 进程与正式 IPC。CA 为本次
// httptest 证书，只显式追加，不修改宿主系统根或关闭 TLS 验证。
func exerciseProtectedDaemonProcess(t *testing.T, directory, uid string, certificate *x509.Certificate, bootPerformed func() bool, loseNextMutation func(), revoke func()) {
	t.Helper()
	private := filepath.Dir(directory)
	caFile := filepath.Join(private, "test-ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(private, "harmonia-acceptance")
	build := exec.Command("go", "build", "-o", binary, "./cmd/harmonia")
	build.Dir = "../core-go"
	// 仅沿用构建工具路径与 Go 缓存位置；不传入宿主真实环境值。
	goEnv := exec.Command("go", "env", "GOCACHE", "GOMODCACHE", "GOPATH")
	goEnv.Env = []string{"PATH=" + os.Getenv("PATH")}
	// go env 需要用户配置的默认目录；通过显式只读值提取，不枚举环境。
	for _, key := range []string{"HOME", "USERPROFILE", "GOCACHE", "GOMODCACHE", "GOPATH"} {
		if value := os.Getenv(key); value != "" {
			goEnv.Env = append(goEnv.Env, key+"="+value)
		}
	}
	cacheOutput, err := goEnv.Output()
	if err != nil {
		t.Fatal("读取 Go 工具缓存位置失败")
	}
	cachePaths := strings.Split(strings.TrimSpace(string(cacheOutput)), "\n")
	if len(cachePaths) != 3 {
		t.Fatal("Go 缓存位置结果不完整")
	}
	build.Env = []string{"PATH=" + os.Getenv("PATH"), "GOCACHE=" + cachePaths[0], "GOMODCACHE=" + cachePaths[1], "GOPATH=" + cachePaths[2], "CGO_ENABLED=0"}
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("实际 CLI 构建失败：%v\n%s", err, output)
	}
	logFile, err := os.OpenFile(filepath.Join(private, "daemon-test.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	process := exec.Command(binary, "daemon", "--local-directory", directory, "--local-user", uid, "--ca-file", caFile, "--interval", "20ms", "--sync-interval", "1s")
	process.Env = []string{"PATH=" + os.Getenv("PATH")}
	process.Stdout, process.Stderr = io.Discard, logFile
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	t.Cleanup(func() {
		_ = process.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				data, _ := os.ReadFile(logFile.Name())
				t.Errorf("正式 daemon 结束失败：%v；固定诊断 contextCanceled=%t", err, bytes.Contains(data, []byte("context canceled")))
			}
		case <-time.After(5 * time.Second):
			_ = process.Process.Kill()
			<-done
			t.Error("正式 daemon 未正常停止")
		}
	})
	callInput := func(command, input string, extra ...string) ([]byte, error) {
		args := append([]string{command, "--local-directory", directory, "--local-user", uid}, extra...)
		cli := exec.Command(binary, args...)
		cli.Env = []string{"PATH=" + os.Getenv("PATH")}
		cli.Stdin = strings.NewReader(input)
		return cli.CombinedOutput()
	}
	call := func(command string, extra ...string) ([]byte, error) {
		return callInput(command, "", extra...)
	}
	waitStatus := func(predicate func(localipc.Status) bool) localipc.Status {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			output, err := call("status")
			var status localipc.Status
			if err == nil && json.Unmarshal(output, &status) == nil && predicate(status) {
				return status
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("正式 daemon/IPC 未在期限内收敛")
		return localipc.Status{}
	}
	waitStatus(func(s localipc.Status) bool {
		return bootPerformed() && s.Sequence >= 3 && s.AccountGeneration == 1 && s.Environments == 1
	})
	exports, err := call("export")
	if err != nil || !bytes.Contains(exports, []byte("native-paired-value")) {
		t.Fatal("正式 daemon 未下发已验签值", err)
	}
	// 原 request-id 经加密 journal 固定。故意丢弃已接受的 HTTP 回应，
	// 再通过独立 CLI 查原回执，不能生成新密文/新序号或直接修改本地权威值。
	writeResult := func(output []byte) localipc.SharedWriteResult {
		t.Helper()
		var result localipc.SharedWriteResult
		start := bytes.IndexByte(output, '{')
		if start < 0 || json.Unmarshal(bytes.TrimSpace(output[start:]), &result) != nil {
			t.Fatal("正式 CLI 写回执不是限定 JSON 元数据")
		}
		for _, secret := range []string{"cli-first-synthetic", "cli-later-synthetic", "selected-a-synthetic", "selected-b-synthetic", "unselected-synthetic", "local-only-synthetic"} {
			if bytes.Contains(output, []byte(secret)) {
				t.Fatal("共享写回执泄漏合成明文")
			}
		}
		return result
	}
	loseNextMutation()
	lost, err := callInput("put", "cli-first-synthetic", "--environment", "dev", "--name", "CLI_SHARED_SYNTHETIC", "--value-stdin", "--request-id", "actual-cli-put")
	if err == nil || !bytes.Contains(lost, []byte("actual-cli-put")) {
		t.Fatal("丢回应未保留可重查的原请求ID")
	}
	retried, err := call("write-retry", "--request-id", "actual-cli-put")
	if err != nil {
		t.Fatal("正式 CLI 原请求重查失败", err, string(retried))
	}
	receipt := writeResult(retried)
	if receipt.Accepted != 1 || !receipt.Applied || len(receipt.Sequences) != 1 || receipt.Sequences[0] != 4 {
		t.Fatal("原回执/正式下发序号错误", receipt)
	}
	waitStatus(func(s localipc.Status) bool { return s.Sequence == 4 })
	overwrite, err := callInput("put", "cli-later-synthetic", "--environment", "dev", "--name", "CLI_SHARED_SYNTHETIC", "--value-stdin", "--request-id", "actual-cli-overwrite")
	if err != nil {
		t.Fatal("正式 CLI 后写失败", err)
	}
	if next := writeResult(overwrite); next.Accepted != 1 || !next.Applied || len(next.Sequences) != 1 || next.Sequences[0] != 5 {
		t.Fatal("后写序号错误", next)
	}
	retried, err = call("write-retry", "--request-id", "actual-cli-put")
	if err != nil {
		t.Fatal("正式 CLI 历史回执重查失败", err)
	}
	if old := writeResult(retried); len(old.Sequences) != 1 || old.Sequences[0] != 4 {
		t.Fatal("历史重试变成新写", old)
	}
	waitStatus(func(s localipc.Status) bool { return s.Sequence == 5 })
	exports, err = call("export")
	if err != nil || !bytes.Contains(exports, []byte("cli-later-synthetic")) || bytes.Contains(exports, []byte("cli-first-synthetic")) {
		t.Fatal("历史重试覆盖较新值")
	}
	imported, err := callInput("import", `{"SELECTED_A":"selected-a-synthetic","SELECTED_B":"selected-b-synthetic","UNSELECTED_C":"unselected-synthetic"}`, "--environment", "dev", "--import-stdin", "--select", "SELECTED_A,SELECTED_B", "--request-id", "actual-cli-import")
	if err != nil {
		t.Fatal("正式 CLI 显式选中导入失败", err)
	}
	if selected := writeResult(imported); selected.Total != 2 || selected.Accepted != 2 || !selected.Applied || len(selected.Sequences) != 2 || selected.Sequences[0] != 6 || selected.Sequences[1] != 7 {
		t.Fatal("导入非选中集合或接受序号异常", selected)
	}
	exports, err = call("export")
	if err != nil || !bytes.Contains(exports, []byte("selected-a-synthetic")) || !bytes.Contains(exports, []byte("selected-b-synthetic")) || bytes.Contains(exports, []byte("UNSELECTED_C")) {
		t.Fatal("未选中变量被导入或选中项未下发")
	}
	if _, err = callInput("override-set", "local-only-synthetic", "--environment", "dev", "--name", "SELECTED_A", "--value-stdin"); err != nil {
		t.Fatal("正式 CLI 本机 override 失败", err)
	}
	waitStatus(func(s localipc.Status) bool { return s.Sequence == 7 })
	exports, err = call("export")
	if err != nil || !bytes.Contains(exports, []byte("local-only-synthetic")) {
		t.Fatal("本机 override 未参与下发")
	}
	deleted, err := call("delete", "--environment", "dev", "--name", "SELECTED_A", "--request-id", "actual-cli-delete")
	if err != nil {
		t.Fatal("正式 CLI 删除失败", err)
	}
	if removal := writeResult(deleted); removal.Accepted != 1 || !removal.Applied || len(removal.Sequences) != 1 || removal.Sequences[0] != 8 {
		t.Fatal("删除未经过正式下发", removal)
	}
	exports, err = call("export")
	if err != nil || bytes.Contains(exports, []byte("SELECTED_A")) || bytes.Contains(exports, []byte("local-only-synthetic")) {
		t.Fatal("云删变量仍让本机 override 生效")
	}
	// 默认进程扫描也只在完全合成 Env 的独立 CLI 中运行，绝不扫描宿主。
	callScan := func(command string, extra ...string) ([]byte, error) {
		args := append([]string{command, "--current-env"}, extra...)
		cli := exec.Command(binary, args...)
		cli.Env = []string{"SCAN_SELECTED=scan-selected-synthetic", "SCAN_UNSELECTED=scan-unselected-synthetic"}
		return cli.CombinedOutput()
	}
	preview, err := callScan("import-preview")
	var names []string
	if err != nil || json.Unmarshal(preview, &names) != nil || len(names) != 2 || names[0] != "SCAN_SELECTED" || names[1] != "SCAN_UNSELECTED" || bytes.Contains(preview, []byte("scan-selected-synthetic")) || bytes.Contains(preview, []byte("scan-unselected-synthetic")) {
		t.Fatal("实际 CLI 合成进程预览未限定为名称")
	}
	loseNextMutation()
	scanLost, err := callScan("import", "--local-directory", directory, "--local-user", uid, "--environment", "dev", "--select", "SCAN_SELECTED", "--request-id", "actual-cli-scan")
	if err == nil || !bytes.Contains(scanLost, []byte("actual-cli-scan")) || bytes.Contains(scanLost, []byte("scan-selected-synthetic")) || bytes.Contains(scanLost, []byte("scan-unselected-synthetic")) {
		t.Fatal("扫描导入未知结果未保留原ID或泄漏值")
	}
	scanRetry, err := call("write-retry", "--request-id", "actual-cli-scan")
	if err != nil || bytes.Contains(scanRetry, []byte("scan-selected-synthetic")) {
		t.Fatal("实际扫描导入原回执重查失败或泄漏值")
	}
	if scan := writeResult(scanRetry); scan.Total != 1 || scan.Accepted != 1 || !scan.Applied || len(scan.Sequences) != 1 || scan.Sequences[0] != 9 {
		t.Fatal("实际扫描导入原ID成为新写或选中集合错误", scan)
	}
	waitStatus(func(s localipc.Status) bool { return s.Sequence == 9 })
	exports, err = call("export")
	if err != nil || !bytes.Contains(exports, []byte("scan-selected-synthetic")) || bytes.Contains(exports, []byte("SCAN_UNSELECTED")) || bytes.Contains(exports, []byte("scan-unselected-synthetic")) {
		t.Fatal("实际扫描未按原验签拉取流下发或导入了未选项")
	}
	if _, err = call("pause"); err != nil {
		t.Fatal("正式 IPC 暂停失败", err)
	}
	if _, err = callInput("put", "paused-synthetic", "--environment", "dev", "--name", "PAUSED_WRITE", "--value-stdin", "--request-id", "actual-cli-paused"); err == nil {
		t.Fatal("暂停期间共享写未拒绝")
	}
	if _, err = callScan("import", "--local-directory", directory, "--local-user", uid, "--environment", "dev", "--select", "SCAN_SELECTED", "--request-id", "actual-cli-paused-scan"); err == nil {
		t.Fatal("暂停期间实际扫描导入未拒绝")
	}
	waitStatus(func(s localipc.Status) bool { return s.Paused && s.Sequence == 9 })
	// 暂停保留配置，却仍处理已经收到的授权撤销。
	revoke()
	waitStatus(func(s localipc.Status) bool { return s.Paused && s.Environments == 0 })
	exports, err = call("export")
	if err != nil || len(exports) != 0 {
		t.Fatal("暂停后的正式 daemon 未移除已撤销环境", err)
	}
	fragment, err := os.ReadFile(filepath.Join(directory, "environment.sh"))
	if err != nil || bytes.Contains(fragment, []byte("native-paired-value")) {
		t.Fatal("撤销后正式 fragment 留有合成托管值", err)
	}
	t.Log("通过：实际无 fixture CLI daemon 在没有登录 session 的情况下从已接受入网回执启动，经显式测试 CA 的 HTTPS boot/授权拉取和身份 IPC；独立 CLI 的 put/delete/选中stdin及合成进程扫描导入、原ID丢回应/历史重试、本机override、暂停拒写通过；关单次 CLI 服务继续，暂停收到撤销仍清托管值。宿主 env 未修改。")
}

func exerciseV2DaemonProcess(t *testing.T, directory, uid string, certificate *x509.Certificate, bootPerformed func() bool) {
	t.Helper()
	private := filepath.Dir(directory)
	caFile := filepath.Join(private, "test-ca.pem")
	if err := os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(private, "harmonia-acceptance")
	build := exec.Command("go", "build", "-o", binary, "./cmd/harmonia")
	build.Dir = "../core-go"
	// 仅沿用构建工具路径与 Go 缓存位置；不传入宿主真实环境值。
	goEnv := exec.Command("go", "env", "GOCACHE", "GOMODCACHE", "GOPATH")
	goEnv.Env = []string{"PATH=" + os.Getenv("PATH")}
	// go env 需要用户配置的默认目录；通过显式只读值提取，不枚举环境。
	for _, key := range []string{"HOME", "USERPROFILE", "GOCACHE", "GOMODCACHE", "GOPATH"} {
		if value := os.Getenv(key); value != "" {
			goEnv.Env = append(goEnv.Env, key+"="+value)
		}
	}
	cacheOutput, err := goEnv.Output()
	if err != nil {
		t.Fatal("读取 Go 工具缓存位置失败")
	}
	cachePaths := strings.Split(strings.TrimSpace(string(cacheOutput)), "\n")
	if len(cachePaths) != 3 {
		t.Fatal("Go 缓存位置结果不完整")
	}
	build.Env = []string{"PATH=" + os.Getenv("PATH"), "GOCACHE=" + cachePaths[0], "GOMODCACHE=" + cachePaths[1], "GOPATH=" + cachePaths[2], "CGO_ENABLED=0"}
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("实际 CLI 构建失败：%v\n%s", err, output)
	}
	logFile, err := os.OpenFile(filepath.Join(private, "daemon-test.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	process := exec.Command(binary, "daemon", "--local-directory", directory, "--local-user", uid, "--ca-file", caFile, "--interval", "20ms", "--sync-interval", "1s")
	process.Env = []string{"PATH=" + os.Getenv("PATH")}
	process.Stdout, process.Stderr = io.Discard, logFile
	if err = process.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- process.Wait() }()
	defer func() {
		_ = process.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			if err != nil {
				data, _ := os.ReadFile(logFile.Name())
				t.Errorf("正式 daemon 结束失败：%v；固定诊断 contextCanceled=%t", err, bytes.Contains(data, []byte("context canceled")))
			}
		case <-time.After(5 * time.Second):
			_ = process.Process.Kill()
			<-done
			t.Error("正式 daemon 未正常停止")
		}
	}()
	callInput := func(command, input string, extra ...string) ([]byte, error) {
		args := append([]string{command, "--local-directory", directory, "--local-user", uid}, extra...)
		cli := exec.Command(binary, args...)
		cli.Env = []string{"PATH=" + os.Getenv("PATH")}
		cli.Stdin = strings.NewReader(input)
		return cli.CombinedOutput()
	}
	call := func(command string, extra ...string) ([]byte, error) {
		return callInput(command, "", extra...)
	}
	waitStatus := func(predicate func(localipc.Status) bool) localipc.Status {
		t.Helper()
		deadline := time.Now().Add(12 * time.Second)
		for time.Now().Before(deadline) {
			output, err := call("status")
			var status localipc.Status
			if err == nil && json.Unmarshal(output, &status) == nil && predicate(status) {
				return status
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatal("正式 daemon/IPC 未在期限内收敛")
		return localipc.Status{}
	}
	waitStatus(func(s localipc.Status) bool {
		return bootPerformed() && s.Sequence == 5 && s.AccountGeneration == 1 && s.Environments == 1
	})
	exports, err := call("export")
	if err != nil || !bytes.Contains(exports, []byte("historical-a-synthetic")) || !bytes.Contains(exports, []byte("historical-b-synthetic")) {
		t.Fatal("真实 v2 daemon 未从受保护双签回执重建逐环境来源", err)
	}
	if _, err = callInput("put", "readonly-daemon-synthetic", "--environment", "dev", "--name", "RO_DAEMON_WRITE", "--value-stdin", "--request-id", "v2-daemon-readonly"); err == nil {
		t.Fatal("v2只读daemon被允许写云")
	}
	waitStatus(func(s localipc.Status) bool { return s.Sequence == 5 })
	fragment, err := os.ReadFile(filepath.Join(directory, "environment.sh"))
	if err != nil || !bytes.Contains(fragment, []byte("historical-a-synthetic")) || !bytes.Contains(fragment, []byte("historical-b-synthetic")) {
		t.Fatal("v2正式fragment缺少历史来源", err)
	}
	t.Log("通过：独立正式 v2 daemon 无登录session，重验加密双签证书与完整逐环境proof，新boot会话和普通pull验证A/B历史；RO共享写失败，停止后台保留配置。")
}

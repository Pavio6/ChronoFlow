//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

const (
	// 二次开关：即使带有 e2e 标签，也必须明确授权才会启动真实进程
	e2eEnabledEnv = "CHRONOFLOW_E2E"
	// E2E 专用 MySQL 与业务库，端口和数据库名称均与本地开发隔离
	mysqlDSN      = "root:chronoflow-e2e@tcp(127.0.0.1:3307)/chronoflow_e2e?charset=utf8mb4&parseTime=True&loc=Local"
	mysqlAdminDSN = "root:chronoflow-e2e@tcp(127.0.0.1:3307)/?charset=utf8mb4&parseTime=True&loc=Local"
)

// testRuntime 管理一次测试套件需要的真实二进制进程和隔离配置
type testRuntime struct {
	rootDir string
	binDir  string
	env     []string
	ports   map[string]int
	roles   []*roleProcess
}

// roleProcess 保存单个角色进程及其输出；失败时会把输出附在断言错误中
type roleProcess struct {
	role string
	port int
	cmd  *exec.Cmd
	logs synchronizedBuffer
}

var activeRuntime *testRuntime

// synchronizedBuffer 允许子进程持续写日志时，测试协程安全读取日志用于诊断
type synchronizedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *synchronizedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(value)
}

func (b *synchronizedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// TestMain 在所有测试前准备独立数据库、迁移和四个真实角色进程
func TestMain(m *testing.M) {
	if os.Getenv(e2eEnabledEnv) != "1" {
		fmt.Fprintf(os.Stderr, "e2e tests skipped: set %s=1 or run make e2e-test\n", e2eEnabledEnv)
		os.Exit(0)
	}

	runtime, err := newTestRuntime()
	if err != nil {
		fmt.Fprintf(os.Stderr, "prepare e2e runtime: %v\n", err)
		os.Exit(1)
	}
	if err := runtime.start(); err != nil {
		runtime.stop()
		fmt.Fprintf(os.Stderr, "start e2e runtime: %v\n", err)
		os.Exit(1)
	}
	activeRuntime = runtime

	code := m.Run()
	runtime.stop()
	os.Exit(code)
}

func newTestRuntime() (*testRuntime, error) {
	rootDir, err := repositoryRoot()
	if err != nil {
		return nil, err
	}
	binDir, err := os.MkdirTemp("", "chronoflow-e2e-*")
	if err != nil {
		return nil, fmt.Errorf("create e2e binary directory: %w", err)
	}

	// 为每个角色动态分配端口，避免占用开发环境固定端口或与并行测试冲突
	ports := make(map[string]int, 4)
	for _, role := range []string{"api", "scheduler", "dispatcher", "worker"} {
		port, reserveErr := reservePort()
		if reserveErr != nil {
			_ = os.RemoveAll(binDir)
			return nil, reserveErr
		}
		ports[role] = port
	}

	return &testRuntime{
		rootDir: rootDir,
		binDir:  binDir,
		ports:   ports,
		env: []string{
			// 指向 E2E 专用依赖，且缩短轮询和重试时间以加快测试反馈
			"CHRONOFLOW_DATABASE_DSN=" + mysqlDSN,
			"CHRONOFLOW_REDIS_ADDR=127.0.0.1:6380",
			"CHRONOFLOW_REDIS_DB=0",
			"CHRONOFLOW_SECURITY_ALLOW_PRIVATE_CALLBACKS=true",
			"CHRONOFLOW_SCHEDULER_POLL_INTERVAL_MS=100",
			"CHRONOFLOW_OUTBOX_POLL_INTERVAL_MS=100",
			"CHRONOFLOW_WORKER_READ_BLOCK_MS=100",
			"CHRONOFLOW_WORKER_RETRY_BASE_SECONDS=1",
			"CHRONOFLOW_WORKER_RETRY_MAX_SECONDS=2",
			"CHRONOFLOW_WORKER_HTTP_TIMEOUT_SECONDS=2",
			"CHRONOFLOW_LOG_FORMAT=console",
			"CHRONOFLOW_LOG_LEVEL=warn",
		},
	}, nil
}

func (r *testRuntime) start() error {
	// 每次套件运行都从空数据库开始，保证结果不受上一次运行残留数据影响
	if err := r.resetDatabase(); err != nil {
		return err
	}
	if err := r.buildBinaries(); err != nil {
		return err
	}
	if err := r.runMigration(); err != nil {
		return err
	}

	// 使用独立操作系统进程启动四个角色，验证真实部署方式下的协作
	for _, role := range []string{"api", "scheduler", "dispatcher", "worker"} {
		if err := r.startRole(role); err != nil {
			return err
		}
	}
	return nil
}

func (r *testRuntime) resetDatabase() error {
	// 迁移只负责建表，测试负责先重建 DSN 指向的空数据库
	db, err := sql.Open("mysql", mysqlAdminDSN)
	if err != nil {
		return fmt.Errorf("open E2E MySQL connection: %w", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping E2E MySQL: %w; run make e2e-up first", err)
	}
	if _, err := db.ExecContext(ctx, "DROP DATABASE IF EXISTS chronoflow_e2e"); err != nil {
		return fmt.Errorf("drop E2E database: %w", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE DATABASE chronoflow_e2e CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		return fmt.Errorf("create E2E database: %w", err)
	}
	return nil
}

func (r *testRuntime) buildBinaries() error {
	// 先编译一次，再启动二进制；避免每个角色使用 go run 带来的编译竞争和额外延迟
	for _, role := range []string{"api", "scheduler", "dispatcher", "worker", "migrate"} {
		output := filepath.Join(r.binDir, "chronoflow-"+role)
		command := exec.Command("go", "build", "-o", output, "./cmd/"+role)
		command.Dir = r.rootDir
		command.Env = os.Environ()
		result, err := command.CombinedOutput()
		if err != nil {
			return fmt.Errorf("build %s: %w\n%s", role, err, result)
		}
	}
	return nil
}

func (r *testRuntime) runMigration() error {
	// 通过与生产相同的迁移命令初始化 schema_migrations 和业务表
	command := exec.Command(filepath.Join(r.binDir, "chronoflow-migrate"), "up")
	command.Dir = r.rootDir
	command.Env = r.commandEnv(0)
	result, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("run E2E migration: %w\n%s", err, result)
	}
	return nil
}

func (r *testRuntime) startRole(role string) error {
	process := &roleProcess{
		role: role,
		port: r.ports[role],
		cmd:  exec.Command(filepath.Join(r.binDir, "chronoflow-"+role)),
	}
	process.cmd.Dir = r.rootDir
	process.cmd.Env = r.commandEnv(process.port)
	// 收集 stdout/stderr，在角色无法就绪或测试超时时提供上下文
	process.cmd.Stdout = &process.logs
	process.cmd.Stderr = &process.logs
	if err := process.cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", role, err)
	}
	r.roles = append(r.roles, process)
	if err := waitForReady(process.port, 15*time.Second); err != nil {
		return fmt.Errorf("wait for %s readiness: %w\n%s logs:\n%s", role, err, role, process.logs.String())
	}
	return nil
}

func (r *testRuntime) commandEnv(port int) []string {
	env := make([]string, 0, len(os.Environ())+len(r.env)+1)
	// 排除宿主机已有的 CHRONOFLOW_* 配置，防止其覆盖测试的隔离参数
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "CHRONOFLOW_") {
			env = append(env, value)
		}
	}
	env = append(env, r.env...)
	if port > 0 {
		env = append(env, fmt.Sprintf("CHRONOFLOW_SERVER_PORT=%d", port))
	}
	return env
}

func (r *testRuntime) stop() {
	// 反向停止角色，使 Worker 先结束消费，再停止上游生产者
	for index := len(r.roles) - 1; index >= 0; index-- {
		process := r.roles[index]
		if process.cmd.Process == nil || process.cmd.ProcessState != nil && process.cmd.ProcessState.Exited() {
			continue
		}
		_ = process.cmd.Process.Signal(os.Interrupt)
	}
	for index := len(r.roles) - 1; index >= 0; index-- {
		process := r.roles[index]
		if process.cmd.Process == nil || process.cmd.ProcessState != nil && process.cmd.ProcessState.Exited() {
			continue
		}
		// 优先发送 SIGINT 让应用走优雅关闭；超时后才强制终止
		if err := waitForProcess(process.cmd, 20*time.Second); err != nil {
			_ = process.cmd.Process.Kill()
			_ = process.cmd.Wait()
			fmt.Fprintf(os.Stderr, "%s did not stop cleanly: %v\n%s\n", process.role, err, process.logs.String())
		}
	}
	_ = os.RemoveAll(r.binDir)
}

func repositoryRoot() (string, error) {
	// 由当前测试文件推导仓库根目录，保证从任意工作目录执行测试都能找到 config 和 migrations
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("resolve E2E test path")
	}
	return filepath.Dir(filepath.Dir(file)), nil
}

func reservePort() (int, error) {
	// 由操作系统分配临时端口，关闭监听器后将端口交给待启动的角色进程
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("reserve local port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func waitForReady(port int, timeout time.Duration) error {
	// 不只等待 TCP 可连接，而是等待角色的依赖检查 /ready 返回 200
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: time.Second}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d/ready", port)
	var lastErr error
	for time.Now().Before(deadline) {
		response, err := client.Get(endpoint)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status %d", response.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s: %w", endpoint, lastErr)
}

func waitForProcess(command *exec.Cmd, timeout time.Duration) error {
	// Wait 可能阻塞到进程退出，因此放入 goroutine 并施加关闭超时
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case <-time.After(timeout):
		return fmt.Errorf("timeout after %s", timeout)
	case err := <-done:
		if err != nil {
			return err
		}
		return nil
	}
}

func roleLogs() string {
	// 汇总所有角色日志，便于一个断言失败时定位是哪个阶段未继续流转
	if activeRuntime == nil {
		return "E2E runtime was not started"
	}
	var output strings.Builder
	for _, process := range activeRuntime.roles {
		fmt.Fprintf(&output, "\n[%s]\n%s\n", process.role, process.logs.String())
	}
	return output.String()
}

func eventually(t *testing.T, timeout time.Duration, condition func() (bool, error)) {
	// 分布式链路是异步的；以短间隔轮询权威 API，而不是使用固定 sleep
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		matched, err := condition()
		if err != nil {
			lastErr = err
		} else if matched {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	if lastErr != nil {
		t.Fatalf("condition did not become true within %s: %v\nrole logs:%s", timeout, lastErr, roleLogs())
	}
	t.Fatalf("condition did not become true within %s\nrole logs:%s", timeout, roleLogs())
}

var once sync.Once

func apiBaseURL() string {
	if activeRuntime == nil {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", activeRuntime.ports["api"])
}

func logE2ERuntime(t *testing.T) {
	// 同一套件只打印一次依赖地址，避免多个场景重复输出
	t.Helper()
	once.Do(func() {
		t.Logf("E2E roles are running against MySQL 127.0.0.1:3307 and Redis 127.0.0.1:6380")
	})
}

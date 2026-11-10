// Package e2e 用黑盒方式测 mypi：把参数交给二进制，检查 stdout、stderr、
// 退出码，以及 faux provider 记下来的请求。测试不碰内部代码，改实现时这里
// 不用跟着改。
package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var binaryPath string

func TestMain(m *testing.M) {
	root, err := repoRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}
	tmp, err := os.MkdirTemp("", "mypi-e2e-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "e2e: %v\n", err)
		os.Exit(1)
	}
	binaryPath = filepath.Join(tmp, "mypi")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}

	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/mypi")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: build failed: %v\n%s", err, out)
		os.RemoveAll(tmp)
		os.Exit(1)
	}

	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", dir)
		}
		dir = parent
	}
}

type result struct {
	stdout string
	stderr string
	code   int
}

// run 跑一次 mypi，不喂输入。HOME 指向临时目录，保证读不到本机的用户配置。
func run(t *testing.T, env map[string]string, args ...string) result {
	t.Helper()
	return runWithInput(t, env, "", args...)
}

// runWithInput 跑一次 mypi，并把 input 当作 stdin 喂进去。
func runWithInput(t *testing.T, env map[string]string, input string, args ...string) result {
	t.Helper()

	cmd := exec.Command(binaryPath, args...)
	cmd.Dir = t.TempDir()
	cmd.Env = hermeticEnv(t, env)
	cmd.Stdin = strings.NewReader(input)

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run %v: %v", args, err)
		}
		code = exitErr.ExitCode()
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func hermeticEnv(t *testing.T, extra map[string]string) []string {
	t.Helper()
	home := t.TempDir()
	env := []string{
		"HOME=" + home,
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + t.TempDir(),
	}
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// writeScript 写一个 faux 脚本文件，返回它的路径。
func writeScript(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "script.json")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func TestSingleShotPrintsScriptedAnswer(t *testing.T) {
	script := writeScript(t, `{"responses":[{"chunks":["hello"," world"]}]}`)

	got := run(t, map[string]string{"MYPY_FAUX_SCRIPT": script}, "hi")

	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}
	if got.stdout != "hello world\n" {
		t.Errorf("stdout = %q, want %q", got.stdout, "hello world\n")
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want empty", got.stderr)
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	script := writeScript(t, `{"responses":[{"chunks":["stable"," answer"]}]}`)
	env := map[string]string{"MYPY_FAUX_SCRIPT": script}

	first := run(t, env, "hi")
	second := run(t, env, "hi")

	if first.stdout != second.stdout {
		t.Fatalf("outputs differ between runs: %q vs %q", first.stdout, second.stdout)
	}
	if first.code != second.code {
		t.Fatalf("exit codes differ: %d vs %d", first.code, second.code)
	}
}

func TestHelpAndVersionNeedNoConfiguration(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"--help"}, want: "Usage:"},
		{args: []string{"-h"}, want: "Usage:"},
		{args: []string{"--version"}, want: "mypi 0.0.1"},
	} {
		got := run(t, nil, tc.args...)
		if got.code != 0 {
			t.Errorf("%v: exit code = %d, want 0 (stderr: %s)", tc.args, got.code, got.stderr)
		}
		if !strings.Contains(got.stdout, tc.want) {
			t.Errorf("%v: stdout = %q, want it to contain %q", tc.args, got.stdout, tc.want)
		}
		if got.stderr != "" {
			t.Errorf("%v: stderr = %q, want empty", tc.args, got.stderr)
		}
	}
}

func TestProviderFailureGoesToStderr(t *testing.T) {
	script := writeScript(t, `{"responses":[{"error":"boom"}]}`)

	got := run(t, map[string]string{"MYPY_FAUX_SCRIPT": script}, "hi")

	if got.code != 1 {
		t.Fatalf("exit code = %d, want 1", got.code)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want empty on failure", got.stdout)
	}
	if !strings.Contains(got.stderr, "boom") {
		t.Errorf("stderr = %q, want it to mention the failure", got.stderr)
	}
}

func TestRunsWithBuiltInScript(t *testing.T) {
	got := run(t, nil, "hi")

	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "内置假模型") {
		t.Errorf("stdout = %q, want the built-in demo reply", got.stdout)
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want empty", got.stderr)
	}
}

func TestBadScriptPathIsReported(t *testing.T) {
	got := run(t, map[string]string{"MYPY_FAUX_SCRIPT": "/nonexistent/script.json"}, "hi")

	if got.code != 1 {
		t.Fatalf("exit code = %d, want 1", got.code)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want empty", got.stdout)
	}
	if !strings.Contains(got.stderr, "read script") {
		t.Errorf("stderr = %q, want it to mention reading the script", got.stderr)
	}
	if strings.Contains(got.stderr, "panic") {
		t.Errorf("stderr = %q, want no panic", got.stderr)
	}
}

func TestFauxRecordsFullRequest(t *testing.T) {
	script := writeScript(t, `{"responses":[{"chunks":["ok"]}]}`)
	record := filepath.Join(t.TempDir(), "record.jsonl")

	got := run(t, map[string]string{
		"MYPY_FAUX_SCRIPT": script,
		"MYPY_FAUX_RECORD": record,
	}, "read go.mod")
	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}

	raw, err := os.ReadFile(record)
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		t.Fatalf("recorded %d requests, want 1 (%q)", len(lines), raw)
	}

	var rec struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
		Stream bool `json:"stream"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("parse recording %q: %v", lines[0], err)
	}

	if rec.Model != "faux-1" {
		t.Errorf("model = %q, want faux-1", rec.Model)
	}
	if !rec.Stream {
		t.Error("stream = false, want true")
	}
	if len(rec.Messages) != 1 {
		t.Fatalf("messages = %d, want 1", len(rec.Messages))
	}
	if rec.Messages[0].Role != "user" {
		t.Errorf("role = %q, want user", rec.Messages[0].Role)
	}
	if len(rec.Messages[0].Content) != 1 || rec.Messages[0].Content[0].Text != "read go.mod" {
		t.Errorf("content = %+v, want one text block with the prompt", rec.Messages[0].Content)
	}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	got := run(t, nil, "--nope", "hi")

	if got.code != 2 {
		t.Fatalf("exit code = %d, want 2", got.code)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want empty", got.stdout)
	}
	if !strings.Contains(got.stderr, "unknown flag") {
		t.Errorf("stderr = %q, want it to name the bad flag", got.stderr)
	}
}

func TestNoInputAndNoPromptExitsCleanly(t *testing.T) {
	got := run(t, nil)

	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}
	if got.stdout != "" {
		t.Errorf("stdout = %q, want empty", got.stdout)
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want empty", got.stderr)
	}
}

func TestUnknownProviderIsUsageError(t *testing.T) {
	got := run(t, nil, "--provider", "openai", "hi")

	if got.code != 2 {
		t.Fatalf("exit code = %d, want 2", got.code)
	}
	if !strings.Contains(got.stderr, "unknown provider") {
		t.Errorf("stderr = %q, want it to name the provider problem", got.stderr)
	}
}

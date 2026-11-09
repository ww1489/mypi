package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordedRequest 是假 provider 记下的一次请求。
type recordedRequest struct {
	Model    string            `json:"model"`
	Messages []recordedMessage `json:"messages"`
}

// recordedMessage 是请求里的一条消息。
type recordedMessage struct {
	Role    string `json:"role"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

// readRecords 读出录制文件里所有请求。
func readRecords(t *testing.T, path string) []recordedRequest {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}
	var out []recordedRequest
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		var req recordedRequest
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			t.Fatalf("parse recording %q: %v", line, err)
		}
		out = append(out, req)
	}
	return out
}

func TestReplSendsGrowingHistory(t *testing.T) {
	script := writeScript(t, `{"responses":[
		{"chunks":["a1"]},
		{"chunks":["a2"]},
		{"chunks":["a3"]}
	]}`)
	record := filepath.Join(t.TempDir(), "record.jsonl")

	got := runWithInput(t, map[string]string{
		"MYPY_FAUX_SCRIPT": script,
		"MYPY_FAUX_RECORD": record,
	}, "one\ntwo\nthree\n")
	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}

	records := readRecords(t, record)
	if len(records) != 3 {
		t.Fatalf("recorded %d requests, want 3", len(records))
	}

	// 第二轮应该带上：第一问、第一答、第二问。
	wantCounts := []int{1, 3, 5}
	for i, want := range wantCounts {
		if len(records[i].Messages) != want {
			t.Errorf("request %d has %d messages, want %d", i+1, len(records[i].Messages), want)
		}
	}

	// 角色顺序必须是 user/assistant 交替。
	wantRoles := []string{"user", "assistant", "user", "assistant", "user"}
	for i, msg := range records[2].Messages {
		if msg.Role != wantRoles[i] {
			t.Errorf("message %d role = %q, want %q", i, msg.Role, wantRoles[i])
		}
	}
}

func TestReplPrintsEveryAnswer(t *testing.T) {
	script := writeScript(t, `{"responses":[
		{"chunks":["first"]},
		{"chunks":["second"]}
	]}`)

	got := runWithInput(t, map[string]string{"MYPY_FAUX_SCRIPT": script}, "one\ntwo\n")
	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}
	if got.stdout != "first\nsecond\n" {
		t.Errorf("stdout = %q, want %q", got.stdout, "first\nsecond\n")
	}
	if got.stderr != "" {
		t.Errorf("stderr = %q, want empty", got.stderr)
	}
}

func TestReplIgnoresBlankLines(t *testing.T) {
	script := writeScript(t, `{"responses":[{"chunks":["only"]}]}`)
	record := filepath.Join(t.TempDir(), "record.jsonl")

	got := runWithInput(t, map[string]string{
		"MYPY_FAUX_SCRIPT": script,
		"MYPY_FAUX_RECORD": record,
	}, "\n\n   \nonly question\n\n")
	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}

	records := readRecords(t, record)
	if len(records) != 1 {
		t.Fatalf("recorded %d requests, want 1", len(records))
	}
}

func TestReplRecordsFailedTurnInHistory(t *testing.T) {
	script := writeScript(t, `{"responses":[
		{"error":"boom"},
		{"chunks":["recovered"]}
	]}`)
	record := filepath.Join(t.TempDir(), "record.jsonl")

	got := runWithInput(t, map[string]string{
		"MYPY_FAUX_SCRIPT": script,
		"MYPY_FAUX_RECORD": record,
	}, "one\ntwo\n")

	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "boom") {
		t.Errorf("stderr = %q, want it to report the failure", got.stderr)
	}
	if !strings.Contains(got.stdout, "recovered") {
		t.Errorf("stdout = %q, want the second answer", got.stdout)
	}

	// 失败的轮次要留在历史里（和 PiG 一样）：第二次请求应该是
	// [user, assistant(失败), user]，而不是只剩半句问题。
	records := readRecords(t, record)
	if len(records) != 2 {
		t.Fatalf("recorded %d requests, want 2", len(records))
	}
	if len(records[1].Messages) != 3 {
		t.Fatalf("second request has %d messages, want 3 (user + failed assistant + user)", len(records[1].Messages))
	}
	wantRoles := []string{"user", "assistant", "user"}
	for i, msg := range records[1].Messages {
		if msg.Role != wantRoles[i] {
			t.Errorf("message %d role = %q, want %q", i, msg.Role, wantRoles[i])
		}
	}
}

func TestReplEOFExitsCleanly(t *testing.T) {
	script := writeScript(t, `{"responses":[{"chunks":["done"]}]}`)

	got := runWithInput(t, map[string]string{"MYPY_FAUX_SCRIPT": script}, "one\n")

	if got.code != 0 {
		t.Fatalf("exit code = %d, want 0", got.code)
	}
	if strings.Contains(got.stderr, "panic") {
		t.Errorf("stderr = %q, want no panic", got.stderr)
	}
}

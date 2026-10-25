package main

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestClaudeCost(t *testing.T) {
	u := claudeUsage{Input: 1e6, Output: 1e6, CacheRead: 1e6, CacheCreate: 2e6}
	u.CacheCreation = &struct {
		Write5m int64 `json:"ephemeral_5m_input_tokens"`
		Write1h int64 `json:"ephemeral_1h_input_tokens"`
	}{Write5m: 1e6, Write1h: 1e6}
	// opus-5-5: 4 in + 20 out + 0.20 read + 5 (5m write) + 8 (1h write)
	if got := u.cost("claude-opus-5-5"); !near(got, 37.2) {
		t.Fatalf("opus-5-5 cost = %v, want 37.2", got)
	}
	u.Speed = "fast"
	if got := u.cost("claude-opus-5-5"); !near(got, 74.4) {
		t.Fatalf("fast cost = %v, want 74.4", got)
	}
	if got := u.cost("<synthetic>"); got != 0 {
		t.Fatalf("synthetic cost = %v, want 0", got)
	}
}

func TestCodexCost(t *testing.T) {
	u := codexUsage{Input: 100_000, Cached: 50_000, CacheWrite: 10_000, Output: 100_000}
	// gpt-6.1-sol: 40K uncached*2 + 50K cached*0.10 + 10K write*2.5 + 100K out*10, per M
	if got := u.cost("gpt-6.1-sol", false); !near(got, 0.08+0.005+0.025+1) {
		t.Fatalf("sol cost = %v", got)
	}
	// Over 272K input: input rates double, output rate x1.5.
	long := codexUsage{Input: 300_000, Output: 100_000}
	if got := long.cost("gpt-6.1-sol", false); !near(got, 0.3*4+0.1*15) {
		t.Fatalf("long-context sol cost = %v", got)
	}
	if got := u.cost("gpt-6.1-sol", true); !near(got, 2*u.cost("gpt-6.1-sol", false)) {
		t.Fatalf("priority cost = %v, want 2x standard", got)
	}
	if got := u.cost("codex-auto-review", false); got != 0 {
		t.Fatalf("unpriced model cost = %v, want 0", got)
	}
}

// writeLog writes lines to path, creating parent dirs.
func writeLog(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
}

func TestScanDedupAndIncremental(t *testing.T) {
	home = t.TempDir()
	ts := now.UTC().Format(time.RFC3339)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	st := &State{Files: map[string]FileState{}, Seen: map[string]bool{}, Days: map[string]*Day{}}

	msg := `{"type":"assistant","timestamp":"` + ts + `","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":1000000,"output_tokens":0}}}`
	claudeLog := filepath.Join(home, ".claude", "projects", "p", "s.jsonl")
	// The same message twice in one file and once more in a resumed session.
	writeLog(t, claudeLog, msg, msg)
	writeLog(t, filepath.Join(home, ".claude", "projects", "p", "resumed.jsonl"), msg)

	codexLog := filepath.Join(home, ".codex", "sessions", now.Format("2006/01/02"), "rollout.jsonl")
	ctx := `{"timestamp":"` + ts + `","type":"turn_context","payload":{"model":"gpt-6-luna","turn_id":"t1"}}`
	tok := func(total int64) string {
		return `{"timestamp":"` + ts + `","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"total_tokens":` +
			itoa(total) + `},"last_token_usage":{"input_tokens":100000,"output_tokens":0}}}}`
	}
	// 100K input bills at short-context Luna rates ($0.10/M). A repeated token_count
	// with the same turn and running total must not count twice.
	writeLog(t, codexLog, ctx, tok(1_000_000), tok(1_000_000))
	// A forked session copies the parent's turn and events.
	writeLog(t, filepath.Join(filepath.Dir(codexLog), "fork.jsonl"), ctx, tok(1_000_000))

	scanClaude(st, monthStart)
	scanCodex(st, monthStart)
	d := st.Days[today]
	if d == nil || !near(d.ClaudeCost, 4) {
		t.Fatalf("claude after first scan = %+v", d)
	}
	if d.CodexTokens != 100_000 || !near(d.CodexCost, 0.01) {
		t.Fatalf("codex after first scan = %+v", d)
	}

	// Rescanning reads nothing new; appending a turn counts only the new turn.
	scanClaude(st, monthStart)
	scanCodex(st, monthStart)
	writeLog(t, codexLog, tok(2_000_000))
	scanCodex(st, monthStart)
	if !near(d.ClaudeCost, 4) || d.CodexTokens != 200_000 || !near(d.CodexCost, 0.02) {
		t.Fatalf("after rescan = %+v", d)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

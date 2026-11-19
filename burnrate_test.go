package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestMain drops the user's config, so tests do not depend on it.
func TestMain(m *testing.M) {
	cfg = config{}
	os.Exit(m.Run())
}

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
	// fable-5-2 must not fall through to the older, pricier claude-fable-5 entry.
	if p, _ := claudePrice("claude-fable-5-2"); p != (price{8, 40, 0.20}) {
		t.Fatalf("fable-5-2 price = %+v", p)
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
	// gpt-6.2-astra: 40K uncached*8 + 50K cached*0.40 + 10K write*10 + 100K out*40, per M
	if got := u.cost("gpt-6.2-astra", false); !near(got, 0.32+0.02+0.1+4) {
		t.Fatalf("astra 6.2 cost = %v", got)
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
	st := &State{Files: map[string]FileState{}, Seen: map[string]bool{}, Days: map[string]*Day{}, Prompted: map[string]time.Time{}}

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

func TestText(t *testing.T) {
	r := report{Name: "Claude", Today: 27.894, Quotas: []quota{{Percent: 78}}}
	if got := r.text(); got != "Claude $27.89 78%" {
		t.Fatalf("text = %q", got)
	}
	r.Quotas = nil
	if got := r.text(); got != "Claude $27.89" {
		t.Fatalf("text without quota = %q", got)
	}
}

func TestClaudeQuota(t *testing.T) {
	// Resets lie in the future, else the limits count as already reset.
	session, weekly := now.Add(time.Hour).UTC().Format(time.RFC3339Nano), now.Add(72*time.Hour).UTC().Format(time.RFC3339)
	body := `{
		"limits": [
			{"kind": "session", "percent": 1, "resets_at": "` + session + `"},
			{"kind": "weekly_all", "percent": 5, "resets_at": "` + weekly + `"},
			{"kind": "weekly_scoped", "percent": 0, "scope": {"model": {"display_name": "Fable"}}}
		],
		"extra_usage": {"monthly_limit": 50000, "used_credits": 1000, "utilization": null, "decimal_places": 2}
	}`
	var b claudeUsageBody
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatal(err)
	}
	var c ClaudeQuota
	if err := b.apply(&c); err != nil {
		t.Fatal(err)
	}
	st := &State{Days: map[string]*Day{}, Claude: map[string]ClaudeQuota{"claude": c}}
	if len(c.Limits) != 3 || c.Limits[2].Name != "Weekly (Fable)" || !near(c.Used, 10) || !near(c.Percent, 2) {
		t.Fatalf("quota = %+v", c)
	}
	if r := claudeReport(st); r.percentText() != "5%" {
		t.Fatalf("bar = %q, want 5%% (the fullest weekly limit)", r.percentText())
	}
}

func TestLimitPastReset(t *testing.T) {
	st := &State{Days: map[string]*Day{}, Claude: map[string]ClaudeQuota{"claude": {FetchedAt: now, Limits: []PlanLimit{
		{Kind: "weekly_all", Name: "Weekly", Percent: 40, ResetsAt: now.Add(-time.Hour)},
		{Kind: "session", Name: "Session", Percent: 70, ResetsAt: now.Add(time.Hour)},
		{Kind: "weekly_scoped", Name: "Weekly (Fable)", Percent: 10, ResetsAt: now.Add(time.Hour)},
	}}}}
	r := claudeReport(st)
	// The weekly limit has reset, and the session limit never counts on the bar.
	if r.percentText() != "10%" {
		t.Fatalf("bar = %q, want 10%%", r.percentText())
	}
	if !strings.HasPrefix(r.Notes[0], "Weekly: 0% used, reset ") {
		t.Fatalf("note = %q", r.Notes[0])
	}
}

func TestMultiAccount(t *testing.T) {
	home = t.TempDir()
	saved := cfg
	defer func() { cfg = saved }()
	cfg.ClaudeAccounts = []claudeAccount{{Name: "work", Dir: "~/.claude"}, {Name: "personal", Dir: "~/.claude-personal"}}
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
	st := &State{Files: map[string]FileState{}, Seen: map[string]bool{}, Days: map[string]*Day{},
		Prompted: map[string]time.Time{}, Claude: map[string]ClaudeQuota{}}

	at := func(ago time.Duration) string { return now.Add(-ago).UTC().Format(time.RFC3339) }
	reply := func(id string, ago time.Duration) string {
		return `{"type":"assistant","timestamp":"` + at(ago) + `","requestId":"r","message":{"id":"` + id +
			`","model":"claude-opus-5-5","usage":{"input_tokens":1000000,"output_tokens":0}}}`
	}
	typed := func(ago time.Duration) string {
		return `{"type":"user","timestamp":"` + at(ago) + `","message":{"role":"user","content":"hi"}}`
	}
	// Lines that look like prompts but are not typed: a tool result, a meta line, a subagent's prompt.
	notTyped := func(ago time.Duration) []string {
		return []string{
			`{"type":"user","timestamp":"` + at(ago) + `","message":{"content":[{"type":"tool_result","content":"ok"}]},"toolUseResult":{}}`,
			`{"type":"user","timestamp":"` + at(ago) + `","isMeta":true,"message":{"content":"caveat"}}`,
			`{"type":"user","timestamp":"` + at(ago) + `","isSidechain":true,"message":{"content":"subtask"}}`,
		}
	}
	work := filepath.Join(home, ".claude", "projects", "p", "s.jsonl")
	// Work: $8 and a prompt an hour ago, but it still writes replies and tool results now.
	// Personal: $4 and a prompt a minute ago. So personal is the account you use.
	writeLog(t, work, typed(time.Hour), reply("w1", time.Hour), reply("w2", 0))
	writeLog(t, work, notTyped(0)...)
	writeLog(t, filepath.Join(home, ".claude-personal", "projects", "p", "s.jsonl"), typed(time.Minute), reply("p1", time.Minute))
	scanClaude(st, monthStart)

	soon := now.Add(time.Hour)
	st.Claude["work"] = ClaudeQuota{Plan: "team", FetchedAt: now, Limit: 500, Limits: []PlanLimit{{Name: "Weekly", Percent: 90, ResetsAt: soon}}}
	st.Claude["personal"] = ClaudeQuota{Plan: "pro", FetchedAt: now, Limits: []PlanLimit{{Name: "Weekly", Percent: 27, ResetsAt: soon}}}
	r := claudeReport(st)
	if !near(r.Today, 12) || r.text() != "Claude $12.00 P 27%" {
		t.Fatalf("today = %v, text = %q", r.Today, r.text())
	}

	// You type in work: the bar switches.
	writeLog(t, work, typed(0))
	scanClaude(st, monthStart)
	if got := claudeReport(st).text(); got != "Claude $12.00 W 90%" {
		t.Fatalf("after a prompt in work: text = %q", got)
	}
	table := strings.Join(r.table(), "\n")
	for _, want := range []string{"work  personal   total", "$8.00     $4.00  $12.00"} {
		if !strings.Contains(table, want) {
			t.Fatalf("table lacks %q:\n%s", want, table)
		}
	}
	notes := strings.Join(r.Notes, "\n")
	for _, want := range []string{"○ work (Team)", "● personal (Pro)", "Monthly credits (after limits): $0.00 / $500.00 used"} {
		if !strings.Contains(notes, want) {
			t.Fatalf("notes lack %q:\n%s", want, notes)
		}
	}
}

func TestCreditsOff(t *testing.T) {
	var b claudeUsageBody
	body := `{"limits": [{"kind": "weekly_all", "percent": 27}],
		"extra_usage": {"is_enabled": false, "monthly_limit": 5000, "used_credits": 0, "decimal_places": 2}}`
	if err := json.Unmarshal([]byte(body), &b); err != nil {
		t.Fatal(err)
	}
	var q ClaudeQuota
	if err := b.apply(&q); err != nil || q.Limit != 0 {
		t.Fatalf("credits that are off: limit = %v, err = %v", q.Limit, err)
	}
}

func TestFailedFetchWaits(t *testing.T) {
	home = t.TempDir() // no credentials: both fetches fail before any request
	st := &State{Claude: map[string]ClaudeQuota{}}
	fetchQuotas(st)
	if c := st.Claude["claude"]; !c.TriedAt.Equal(now) || !c.FetchedAt.IsZero() || !st.Spend.TriedAt.Equal(now) {
		t.Fatalf("after a failed fetch: claude = %+v, codex = %+v", c, st.Spend)
	}
}

func TestAccountConfig(t *testing.T) {
	home = t.TempDir()
	saved := cfg
	defer func() { cfg = saved }()
	t.Setenv("WORK_DIR", filepath.Join(home, ".claude-work"))
	cfg.ClaudeAccounts = []claudeAccount{
		{Dir: "$WORK_DIR"},                     // no name: named after the dir
		{Name: "wife", Dir: "~/.claude-wife"},  // shares a first letter with work
		{Name: "wife", Dir: "~/.claude-wife2"}, // duplicate name
		{Name: "empty"},                        // no dir: skipped
	}
	got := claudeAccounts()
	want := []claudeAccount{
		{Name: "claude-work", Dir: filepath.Join(home, ".claude-work")},
		{Name: "wife", Dir: filepath.Join(home, ".claude-wife")},
		{Name: "wife-2", Dir: filepath.Join(home, ".claude-wife2")},
	}
	if len(got) != len(want) {
		t.Fatalf("accounts = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("account %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	two := []claudeAccount{{Name: "work"}, {Name: "wife"}}
	if a, b := tag("work", two), tag("wife", two); a != "WO" || b != "WI" {
		t.Fatalf("tags = %q, %q", a, b)
	}
	if got := tag("wife", got); got != "WIFE" {
		t.Fatalf("tag beside wife-2 = %q, want WIFE", got)
	}
}

func TestAccountsHint(t *testing.T) {
	home = t.TempDir()
	saved := cfg
	defer func() { cfg = saved }()
	cfg = config{}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	for _, d := range []string{".claude", ".claude-personal"} {
		writeLog(t, filepath.Join(home, d, "projects", "p", "s.jsonl"), "{}")
		writeLog(t, filepath.Join(home, d, ".claude.json"), "{}")
	}
	writeLog(t, filepath.Join(home, ".cache", "projects", "x.jsonl"), "{}") // logs, but not Claude Code's
	hint := accountsHint()
	for _, want := range []string{"Found 2 ", `"dir": "~/.claude"`, `"name": "claude-personal"`} {
		if !strings.Contains(hint, want) {
			t.Fatalf("hint lacks %q:\n%s", want, hint)
		}
	}
	cfg.ClaudeAccounts = []claudeAccount{{Name: "work", Dir: "~/.claude"}}
	if accountsHint() != "" {
		t.Fatal("hint shown although accounts are configured")
	}
}

func TestPool(t *testing.T) {
	soon := now.Add(time.Hour)
	limits := func(session, weekly float64) []PlanLimit {
		return []PlanLimit{
			{Kind: "session", Percent: session, ResetsAt: soon},
			{Kind: "weekly_all", Percent: weekly, ResetsAt: soon},
		}
	}
	for _, tc := range []struct {
		name    string
		q       ClaudeQuota
		percent float64
		credits bool
	}{
		{"inside the plan: weekly", ClaudeQuota{Limits: limits(70, 40), Limit: 500, Percent: 0}, 40, false},
		{"weekly used up: credits", ClaudeQuota{Limits: limits(0, 100), Limit: 500, Percent: 12}, 12, true},
		{"session used up: credits", ClaudeQuota{Limits: limits(100, 40), Limit: 500, Percent: 3}, 3, true},
		{"used up, no credits: blocked", ClaudeQuota{Limits: limits(100, 40)}, 100, false},
	} {
		if p, c := tc.q.pool(); !near(p, tc.percent) || c != tc.credits {
			t.Errorf("%s: pool = %v, %v; want %v, %v", tc.name, p, c, tc.percent, tc.credits)
		}
	}
	r := report{Name: "Claude", Today: 1, Quotas: []quota{{Tag: "W", Percent: 12, Credits: true}}}
	if got := r.text(); got != "Claude $1.00 W 12%$" {
		t.Fatalf("text = %q", got)
	}

	// Codex: weekly from the logs, credits once it is used up.
	st := &State{Days: map[string]*Day{}, Codex: CodexQuota{UsedPercent: 30, ResetsAt: soon.Unix(), SeenAt: now},
		Spend: CodexSpend{Limit: 25000, Used: 500, Percent: 2, FetchedAt: now}}
	if got := codexReport(st).percentText(); got != "30%" {
		t.Fatalf("codex inside the plan = %q", got)
	}
	st.Codex.UsedPercent = 100
	if got := codexReport(st).percentText(); got != "2%$" {
		t.Fatalf("codex weekly used up = %q", got)
	}
}

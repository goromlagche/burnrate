// burnrate shows daily Claude/Codex usage and quota in the status bar.
//
// It runs once and exits. On Linux a systemd user timer runs it every minute
// and Waybar (or i3status) reads the files it writes. On macOS BurnrateBar runs
// it with --menubar and shows the items in the menu bar.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	claudeUsageURL = "https://api.anthropic.com/api/oauth/usage"
	codexUsageURL  = "https://chatgpt.com/backend-api/wham/usage"
	apiInterval    = 5 * time.Minute
	apiStale       = 30 * time.Minute
)

type Day struct {
	ClaudeCost  float64 `json:"claude_cost"` // estimate at API list prices
	CodexTokens int64   `json:"codex_tokens"`
	CodexCost   float64 `json:"codex_cost"` // estimate at API list prices
}

type FileState struct {
	Offset int64 `json:"offset"`
	// Codex token_count lines carry no model, turn or tier; carry them from earlier lines.
	Model    string `json:"model,omitempty"`
	Turn     string `json:"turn,omitempty"`
	Priority bool   `json:"priority,omitempty"`
}

type ClaudeQuota struct {
	Used      float64   `json:"used"` // USD
	Limit     float64   `json:"limit"`
	Percent   float64   `json:"percent"`
	FetchedAt time.Time `json:"fetched_at"`
}

type CodexQuota struct {
	UsedPercent   float64   `json:"used_percent"`
	WindowMinutes int64     `json:"window_minutes"`
	ResetsAt      int64     `json:"resets_at"`
	SeenAt        time.Time `json:"seen_at"`
}

// CodexSpend is the workspace's monthly spend control, in credits.
type CodexSpend struct {
	Used      float64   `json:"used"`
	Limit     float64   `json:"limit"` // 0 when the workspace sets no limit
	Percent   float64   `json:"percent"`
	ResetsAt  int64     `json:"resets_at"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Bump to force a full rescan when the parsed fields change.
const stateVersion = 4

type State struct {
	Version int                  `json:"version"`
	Month   string               `json:"month"`
	Files   map[string]FileState `json:"files"`
	Seen    map[string]bool      `json:"seen"` // billed Claude messages and Codex events
	Days    map[string]*Day      `json:"days"`
	Claude  ClaudeQuota          `json:"claude"`
	Codex   CodexQuota           `json:"codex"`
	Spend   CodexSpend           `json:"codex_spend"`
}

var (
	home, _  = os.UserHomeDir()
	cacheDir = filepath.Join(home, ".cache", "burnrate")
	cfg      = loadConfig()
	now      = time.Now()
	today    = now.Format("2006-01-02")
	month    = now.Format("2006-01")
)

func main() {
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		subcommand(os.Args[1])
		return
	}

	jsonOut := flag.Bool("json", false, "print full state as JSON")
	menubar := flag.Bool("menubar", false, "print macOS menu bar items as JSON")
	flag.Parse()
	if flag.NArg() > 0 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}

	// The timer and a manual refresh can overlap; serialize them on the state file.
	if err := lock(); err != nil {
		fmt.Fprintln(os.Stderr, "lock:", err)
		os.Exit(1)
	}

	st := loadState()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)

	scanClaude(st, monthStart)
	scanCodex(st, monthStart)
	fetchQuotas(st)

	if err := saveState(st); err != nil {
		fmt.Fprintln(os.Stderr, "save state:", err)
	}
	reports := map[string]report{"claude": claudeReport(st), "codex": codexReport(st)}
	// <name>.json for Waybar, <name>.txt for i3status read_file.
	for name, r := range reports {
		j, _ := json.Marshal(r.waybar())
		if err := writeAtomic(filepath.Join(cacheDir, name+".json"), append(j, '\n')); err != nil {
			fmt.Fprintln(os.Stderr, "write bar:", err)
			os.Exit(1)
		}
		if err := writeAtomic(filepath.Join(cacheDir, name+".txt"), []byte(r.pango()+"\n")); err != nil {
			fmt.Fprintln(os.Stderr, "write bar:", err)
			os.Exit(1)
		}
	}

	if *menubar {
		out, _ := json.Marshal([]menubarItem{reports["claude"].menubar(), reports["codex"].menubar()})
		fmt.Println(string(out))
	}

	if *jsonOut {
		out, _ := json.MarshalIndent(map[string]any{
			"days": st.Days, "claude": st.Claude, "codex": st.Codex, "codex_spend": st.Spend,
		}, "", "  ")
		fmt.Println(string(out))
	}
}

// lock holds an exclusive lock on the state until the process exits.
// config is optional, read from ~/.config/burnrate/config.json.
type config struct {
	CodexCreditUSD float64 `json:"codex_credit_usd"` // set by your OpenAI agreement
}

func loadConfig() (c config) {
	if b, err := os.ReadFile(filepath.Join(home, ".config", "burnrate", "config.json")); err == nil {
		_ = json.Unmarshal(b, &c)
	}
	return c
}

func lock() error {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(cacheDir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

func loadState() *State {
	st := &State{}
	if b, err := os.ReadFile(filepath.Join(cacheDir, "state.json")); err == nil {
		_ = json.Unmarshal(b, st)
	}
	if st.Month != month || st.Version != stateVersion {
		// New month: drop per-file progress and dedup keys, keep quota caches.
		st.Version = stateVersion
		st.Month = month
		st.Files = map[string]FileState{}
		st.Seen = map[string]bool{}
		st.Days = map[string]*Day{}
	}
	if st.Files == nil {
		st.Files = map[string]FileState{}
	}
	if st.Seen == nil {
		st.Seen = map[string]bool{}
	}
	if st.Days == nil {
		st.Days = map[string]*Day{}
	}
	return st
}

func saveState(st *State) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(cacheDir, "state.json"), b)
}

func writeAtomic(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (st *State) day(ts time.Time) *Day {
	key := ts.Local().Format("2006-01-02")
	d := st.Days[key]
	if d == nil {
		d = &Day{}
		st.Days[key] = d
	}
	return d
}

// readNew calls fn for each complete line added to path since the last run.
func readNew(st *State, path string, fn func(line []byte, fs *FileState)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return
	}
	fstate := st.Files[path]
	if info.Size() < fstate.Offset {
		fstate = FileState{} // truncated or replaced
	}
	if info.Size() == fstate.Offset {
		return
	}
	if _, err := f.Seek(fstate.Offset, io.SeekStart); err != nil {
		return
	}
	r := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			break // partial last line: retry next run
		}
		fstate.Offset += int64(len(line))
		fn(line, &fstate)
	}
	st.Files[path] = fstate
}

// walkJSONL calls fn for each .jsonl file under root modified since `since`.
func walkJSONL(root string, since time.Time, fn func(path string)) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if info, err := d.Info(); err == nil && !info.ModTime().Before(since) {
			fn(path)
		}
		return nil
	})
}

type claudeLine struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	RequestID string    `json:"requestId"`
	Message   struct {
		ID    string       `json:"id"`
		Model string       `json:"model"`
		Usage *claudeUsage `json:"usage"`
	} `json:"message"`
}

func scanClaude(st *State, monthStart time.Time) {
	marker := []byte(`"type":"assistant"`)
	walkJSONL(filepath.Join(home, ".claude", "projects"), monthStart, func(path string) {
		readNew(st, path, func(line []byte, _ *FileState) {
			if !bytes.Contains(line, marker) {
				return
			}
			var l claudeLine
			if json.Unmarshal(line, &l) != nil || l.Message.Usage == nil || l.Timestamp.Before(monthStart) {
				return
			}
			// Streamed replies and resumed sessions repeat the same message.
			key := l.Message.ID + ":" + l.RequestID
			if st.Seen[key] {
				return
			}
			st.Seen[key] = true
			d := st.day(l.Timestamp)
			d.ClaudeCost += l.Message.Usage.cost(l.Message.Model)
		})
	})
}

type codexLine struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Payload   struct {
		Type           string `json:"type"`
		Model          string `json:"model"`   // turn_context
		TurnID         string `json:"turn_id"` // turn_context
		ThreadSettings *struct {
			ServiceTier string `json:"service_tier"`
		} `json:"thread_settings"` // thread_settings_applied
		Info *struct {
			Total json.RawMessage `json:"total_token_usage"`
			Last  codexUsage      `json:"last_token_usage"`
		} `json:"info"`
		RateLimits *struct {
			Primary *struct {
				UsedPercent   float64 `json:"used_percent"`
				WindowMinutes int64   `json:"window_minutes"`
				ResetsAt      int64   `json:"resets_at"`
			} `json:"primary"`
		} `json:"rate_limits"`
	} `json:"payload"`
}

func scanCodex(st *State, monthStart time.Time) {
	markers := [][]byte{[]byte(`"token_count"`), []byte(`"turn_context"`), []byte(`"thread_settings_applied"`)}
	root := filepath.Join(home, ".codex", "sessions")
	// Sessions live in YYYY/MM/DD. Only this and last month can hold new lines.
	for _, m := range []time.Time{monthStart.AddDate(0, -1, 0), monthStart} {
		walkJSONL(filepath.Join(root, m.Format("2006/01")), monthStart, func(path string) {
			readNew(st, path, func(line []byte, fstate *FileState) {
				if !slices.ContainsFunc(markers, func(m []byte) bool { return bytes.Contains(line, m) }) {
					return
				}
				var l codexLine
				if json.Unmarshal(line, &l) != nil {
					return
				}
				switch {
				case l.Type == "turn_context":
					fstate.Model, fstate.Turn = l.Payload.Model, l.Payload.TurnID
					return
				case l.Payload.ThreadSettings != nil:
					fstate.Priority = l.Payload.ThreadSettings.ServiceTier == "priority"
					return
				case l.Payload.Type != "token_count":
					return
				}
				if rl := l.Payload.RateLimits; rl != nil && rl.Primary != nil && l.Timestamp.After(st.Codex.SeenAt) {
					st.Codex = CodexQuota{
						UsedPercent:   rl.Primary.UsedPercent,
						WindowMinutes: rl.Primary.WindowMinutes,
						ResetsAt:      rl.Primary.ResetsAt,
						SeenAt:        l.Timestamp,
					}
				}
				info := l.Payload.Info
				// Zero-token events report context size, not a request.
				if info == nil || info.Last.Input+info.Last.Output == 0 || l.Timestamp.Before(monthStart) {
					return
				}
				// Repeats and forked sessions re-emit an event with the same turn and running total.
				key := "codex:" + fstate.Turn + ":" + string(info.Total)
				if len(info.Total) == 0 {
					key = "codex:" + l.Timestamp.String()
				}
				if st.Seen[key] {
					return
				}
				st.Seen[key] = true
				d := st.day(l.Timestamp)
				d.CodexTokens += info.Last.Input + info.Last.Output
				d.CodexCost += info.Last.cost(fstate.Model, fstate.Priority)
			})
		})
	}
}

func fetchQuotas(st *State) {
	if time.Since(st.Claude.FetchedAt) >= apiInterval {
		if err := fetchClaudeAPI(&st.Claude); err != nil {
			fmt.Fprintln(os.Stderr, "claude usage api:", err)
		}
	}
	if time.Since(st.Spend.FetchedAt) >= apiInterval {
		if err := fetchCodexAPI(&st.Spend); err != nil {
			fmt.Fprintln(os.Stderr, "codex usage api:", err)
		}
	}
}

// getJSON decodes a GET response. Tokens go only in headers and are never logged.
func getJSON(url string, headers map[string]string, v any) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "burnrate")
	for k, val := range headers {
		req.Header.Set(k, val)
	}
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}

func fetchCodexAPI(q *CodexSpend) error {
	b, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return err
	}
	var auth struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(b, &auth); err != nil {
		return err
	}
	if auth.Tokens.AccessToken == "" {
		return fmt.Errorf("no token; run codex login")
	}
	var body struct {
		SpendControl struct {
			Limit *struct {
				Limit       float64 `json:"limit,string"`
				Used        float64 `json:"used,string"`
				UsedPercent float64 `json:"used_percent"`
				ResetAt     int64   `json:"reset_at"`
			} `json:"individual_limit"`
		} `json:"spend_control"`
	}
	err = getJSON(codexUsageURL, map[string]string{
		"Authorization":      "Bearer " + auth.Tokens.AccessToken,
		"ChatGPT-Account-Id": auth.Tokens.AccountID,
	}, &body)
	if err != nil {
		return err
	}
	*q = CodexSpend{FetchedAt: now}
	if l := body.SpendControl.Limit; l != nil {
		q.Used, q.Limit, q.Percent, q.ResetsAt = l.Used, l.Limit, l.UsedPercent, l.ResetAt
	}
	return nil
}

// claudeCredentials returns Claude Code's OAuth credentials JSON.
// macOS keeps it in the login Keychain; Linux keeps it in a file.
func claudeCredentials() ([]byte, error) {
	if runtime.GOOS == "darwin" {
		out, err := exec.Command("/usr/bin/security", "find-generic-password", "-s", "Claude Code-credentials", "-w").Output()
		if err == nil {
			return out, nil
		}
	}
	return os.ReadFile(filepath.Join(home, ".claude", ".credentials.json"))
}

func fetchClaudeAPI(q *ClaudeQuota) error {
	b, err := claudeCredentials()
	if err != nil {
		return err
	}
	var creds struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			ExpiresAt   int64  `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(b, &creds); err != nil {
		return err
	}
	// Claude Code refreshes the token when it runs. We never refresh it ourselves.
	if creds.OAuth.AccessToken == "" || time.UnixMilli(creds.OAuth.ExpiresAt).Before(now) {
		return fmt.Errorf("no valid token; run claude once to refresh")
	}

	var body struct {
		Extra *struct {
			MonthlyLimit float64 `json:"monthly_limit"`
			UsedCredits  float64 `json:"used_credits"`
			Utilization  float64 `json:"utilization"`
			Decimals     int     `json:"decimal_places"`
		} `json:"extra_usage"`
	}
	err = getJSON(claudeUsageURL, map[string]string{
		"Authorization":  "Bearer " + creds.OAuth.AccessToken,
		"anthropic-beta": "oauth-2025-04-20",
	}, &body)
	if err != nil {
		return err
	}
	if body.Extra == nil {
		return fmt.Errorf("no extra_usage in response")
	}
	div := 1.0
	for range body.Extra.Decimals {
		div *= 10
	}
	q.Used = body.Extra.UsedCredits / div
	q.Limit = body.Extra.MonthlyLimit / div
	q.Percent = body.Extra.Utilization
	q.FetchedAt = now
	return nil
}

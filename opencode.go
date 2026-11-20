package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// OpenCode V2 keeps every session in one SQLite file. burnrate reads it with the
// sqlite3 CLI, read-only, so it needs no SQLite driver.
func openCodeDB() string {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "opencode", "opencode.db")
}

// Skipped sessions and providers, so nothing is counted twice:
//   - sessions OpenCode imported from Claude Code or Codex (metadata.importedFrom);
//     burnrate reads those tools' own logs.
//   - claude-agent, which runs Claude Code. Its usage lands in ~/.claude/projects.
const openCodeQuery = `
select m.time_created as t,
       json_extract(m.data, '$.model.providerID') as provider,
       json_extract(m.data, '$.model.id') as model,
       coalesce(json_extract(m.data, '$.cost'), 0) as cost,
       json_extract(m.data, '$.tokens') as tokens
from session_message m join session_v2 s on s.id = m.session_id
where m.type = 'assistant'
  and m.time_created >= %d
  and json_extract(m.data, '$.tokens') is not null
  and json_extract(s.metadata, '$.importedFrom') is null
  and coalesce(json_extract(m.data, '$.model.providerID'), '') != 'claude-agent'`

type openCodeTokens struct {
	Input     int64 `json:"input"` // excludes cache reads and writes
	Output    int64 `json:"output"`
	Reasoning int64 `json:"reasoning"`
	Cache     struct {
		Read  int64 `json:"read"`
		Write int64 `json:"write"`
	} `json:"cache"`
}

func (t openCodeTokens) total() int64 {
	return t.Input + t.Output + t.Reasoning + t.Cache.Read + t.Cache.Write
}

// openCodeCost is what OpenCode billed, such as Zen or an API key. A
// subscription login bills $0, so price those at API list prices, as for
// Claude and Codex. Other providers at $0 stay $0.
func openCodeCost(provider, model string, billed float64, t openCodeTokens) float64 {
	if billed > 0 {
		return billed
	}
	switch provider {
	case "openai":
		u := codexUsage{Input: t.Input + t.Cache.Read + t.Cache.Write, Cached: t.Cache.Read,
			CacheWrite: t.Cache.Write, Output: t.Output + t.Reasoning}
		return u.cost(model, false)
	case "anthropic":
		u := claudeUsage{Input: t.Input, Output: t.Output + t.Reasoning, CacheCreate: t.Cache.Write, CacheRead: t.Cache.Read}
		return u.cost(model)
	}
	return 0
}

// scanOpenCode recomputes this month from the database on every run. The
// query takes well under a second, and streaming replies gain tokens as they
// finish, so there is no offset to keep.
func scanOpenCode(st *State, monthStart time.Time) {
	db := openCodeDB()
	if _, err := os.Stat(db); err != nil {
		return
	}
	out, err := exec.Command("sqlite3", "-readonly", "-json", db,
		fmt.Sprintf(openCodeQuery, monthStart.UnixMilli())).Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "opencode:", err)
		return
	}
	var rows []struct {
		T        int64   `json:"t"`
		Provider string  `json:"provider"`
		Model    string  `json:"model"`
		Cost     float64 `json:"cost"`
		Tokens   string  `json:"tokens"`
	}
	if len(out) > 0 && json.Unmarshal(out, &rows) != nil {
		fmt.Fprintln(os.Stderr, "opencode: unexpected sqlite3 output")
		return
	}
	st.OpenCode = true
	for _, d := range st.Days {
		d.OpenCodeCost, d.OpenCodeTokens, d.OpenCodeBy = 0, 0, nil
	}
	for _, r := range rows {
		var t openCodeTokens
		if json.Unmarshal([]byte(r.Tokens), &t) != nil {
			continue
		}
		d := st.day(time.UnixMilli(r.T))
		cost := openCodeCost(r.Provider, r.Model, r.Cost, t)
		d.OpenCodeCost += cost
		d.OpenCodeTokens += t.total()
		if d.OpenCodeBy == nil {
			d.OpenCodeBy = map[string]float64{}
		}
		d.OpenCodeBy[r.Provider] += cost
	}
}

func openCodeReport(st *State) report {
	r := report{Name: "OpenCode", Basis: "billed, or estimate at API prices for subscriptions",
		Glyph: "\uea85", GlyphFG: fgText, Today: todayUsage(st).OpenCodeCost} // cod-terminal; Nerd Fonts has no OpenCode logo
	month, by := 0.0, map[string]float64{}
	for _, d := range st.Days {
		month += d.OpenCodeCost
		for p, c := range d.OpenCodeBy {
			by[p] += c
		}
	}
	for _, k := range recentDays(st, 7) {
		d := st.Days[k]
		r.Rows = append(r.Rows, []string{k[5:], usd(d.OpenCodeCost), humanTokens(d.OpenCodeTokens)})
	}
	r.Rows = append(r.Rows, []string{"month", usd(month)})
	providers := make([]string, 0, len(by))
	for p := range by {
		providers = append(providers, p)
	}
	slices.SortFunc(providers, func(a, b string) int {
		if by[a] != by[b] {
			if by[a] > by[b] {
				return -1
			}
			return 1
		}
		if a < b {
			return -1
		}
		return 1
	})
	// The bar shows the workspace budget, as OpenCode's sidebar does.
	if b := st.Budget; b.Limit > 0 {
		resetsAt := time.UnixMilli(b.ResetsAt)
		spent := b.Spent
		if b.ResetsAt > 0 && now.After(resetsAt) {
			spent = 0
		}
		r.Quotas = []quota{{Percent: spent / b.Limit * 100, ResetsAt: resetsAt, Stale: time.Since(b.FetchedAt) > apiStale}}
		line := fmt.Sprintf("Budget: %s / %s used, %s left", usd(spent), usd(b.Limit), usd(b.Limit-spent))
		if b.ResetsAt > 0 {
			line += ", resets " + resetsAt.Local().Format("Mon 02 Jan 15:04")
		}
		if b.Workspace != "" {
			line = b.Workspace + " " + strings.ToLower(line[:1]) + line[1:]
		}
		r.Notes = append(r.Notes, line, "Updated "+b.FetchedAt.Local().Format("15:04"), "")
	}
	if len(providers) > 0 {
		r.Notes = append(r.Notes, "Month by provider:")
		for _, p := range providers {
			r.Notes = append(r.Notes, fmt.Sprintf("  %s %s", p, usd(by[p])))
		}
	}
	return r
}

// OpenCodeBudget is the OpenCode Console budget that applies to you, the one
// OpenCode's sidebar shows as "Limit ... remaining".
type OpenCodeBudget struct {
	Workspace string    `json:"workspace,omitempty"`
	Limit     float64   `json:"limit"` // dollars; 0 when no budget applies
	Spent     float64   `json:"spent"`
	ResetsAt  int64     `json:"resets_at"` // ms
	FetchedAt time.Time `json:"fetched_at"`
	TriedAt   time.Time `json:"tried_at"`
}

const openCodeConsole = "https://opencode.ai/console"

// openCodeLogin reads the active OpenCode Console login from OpenCode's
// credential table. The token is used for one request header only.
func openCodeLogin() (server, token, org, name string, err error) {
	out, err := exec.Command("sqlite3", "-readonly", "-json", openCodeDB(), `
select json_extract(value, '$.type') as type, json_extract(value, '$.access') as access,
       json_extract(value, '$.key') as key, json_extract(value, '$.metadata.server') as server,
       json_extract(value, '$.metadata.orgID') as org, json_extract(value, '$.metadata.orgName') as name
from credential where integration_id = 'opencode' and active = 1`).Output()
	if err != nil {
		return "", "", "", "", err
	}
	var rows []struct{ Type, Access, Key, Server, Org, Name string }
	if len(out) > 0 {
		if err := json.Unmarshal(out, &rows); err != nil {
			return "", "", "", "", fmt.Errorf("unexpected sqlite3 output")
		}
	}
	if len(rows) == 0 || rows[0].Org == "" {
		return "", "", "", "", fmt.Errorf("not signed in to OpenCode Console; run /connect in OpenCode")
	}
	l := rows[0]
	token = l.Key
	if l.Type == "oauth" {
		token = l.Access
	}
	// Same rule as OpenCode: the old console host maps to the current one.
	server = strings.TrimSuffix(l.Server, "/")
	if server == "" || strings.HasPrefix(server, "https://console.opencode.ai") {
		server = openCodeConsole
	}
	return server, token, l.Org, l.Name, nil
}

// openCodeTime is an RFC 3339 string or Unix milliseconds.
type openCodeTime int64

func (t *openCodeTime) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		ts, err := time.Parse(time.RFC3339Nano, s)
		*t = openCodeTime(ts.UnixMilli())
		return err
	}
	var ms int64
	err := json.Unmarshal(b, &ms)
	*t = openCodeTime(ms)
	return err
}

var openCodeBudgetURL = func(server string) string { return server + "/api/policies/current-budgets" }

func fetchOpenCodeBudget(q *OpenCodeBudget) error {
	server, token, org, name, err := openCodeLogin()
	if err != nil {
		return err
	}
	var budgets []struct {
		LimitDollars float64      `json:"limitDollars"`
		SpentDollars float64      `json:"spentDollars"`
		Picked       bool         `json:"picked"`
		ResetsAt     openCodeTime `json:"resetsAt"`
	}
	err = getJSON(openCodeBudgetURL(server), map[string]string{"Authorization": "Bearer " + token, "x-org-id": org}, &budgets)
	if err != nil {
		return err
	}
	*q = OpenCodeBudget{Workspace: name, FetchedAt: now}
	// The budget OpenCode picked for you, else the fullest one.
	best := -1.0
	for _, b := range budgets {
		if b.LimitDollars <= 0 {
			continue
		}
		used := b.SpentDollars / b.LimitDollars
		if b.Picked {
			used = 2
		}
		if used > best {
			best = used
			q.Limit, q.Spent, q.ResetsAt = b.LimitDollars, b.SpentDollars, int64(b.ResetsAt)
		}
	}
	return nil
}

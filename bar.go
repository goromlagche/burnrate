package main

import (
	_ "embed"
	"fmt"
	"strings"
	"time"
)

// Colors match a gruvbox-dark bar.
const (
	fgText   = "#ebdbb2"
	fgDim    = "#928374"
	fgGood   = "#36E592"
	fgWarn   = "#CE9726"
	fgBad    = "#CE4B4F"
	fgClaude = "#D97757"
)

// Template images for the macOS menu bar, rendered from the Nerd Font glyphs.
var (
	//go:embed icons/claude.png
	pngClaude []byte
	//go:embed icons/codex.png
	pngCodex []byte
)

// report is one tool's usage, rendered by each bar backend.
type report struct {
	Name    string
	Basis   string // how cost is counted; default "estimate at API prices"
	Glyph   string // Nerd Font icon for Linux bars
	GlyphFG string
	Tint    string // macOS icon color; empty keeps a monochrome template icon
	PNG     []byte // template icon for the macOS menu bar
	Today   float64
	Quotas  []quota    // one, or none without quota data
	Rows    [][]string // optional header, recent days, then the month total
	Notes   []string   // "" separates groups
}

// quota is one percent on the bar.
type quota struct {
	Tag      string // account, when there are several
	Percent  float64
	ResetsAt time.Time // zero when unknown
	Stale    bool
}

func todayUsage(st *State) *Day {
	if d := st.Days[today]; d != nil {
		return d
	}
	return &Day{}
}

// recentDays returns up to n days ending today, oldest first.
func recentDays(st *State, n int) []string {
	var days []string
	for i := n - 1; i >= 0; i-- {
		k := now.AddDate(0, 0, -i).Format("2006-01-02")
		if _, ok := st.Days[k]; ok {
			days = append(days, k)
		}
	}
	return days
}

// claudeReport shows one account on the bar: the one you last typed a prompt
// in. Its cost and quota sit next to its tag, so "$4.00 P 27%" is all
// personal. Replies do not count: a session can work on its own while you use
// the other one. The tooltip has every account and the total.
func claudeReport(st *State) report {
	r := report{Name: "Claude", Glyph: "", GlyphFG: fgClaude, Tint: fgClaude, PNG: pngClaude, Today: todayUsage(st).ClaudeCost}
	accounts := claudeAccounts()
	multi := len(accounts) > 1
	active := accounts[0]
	for _, a := range accounts[1:] {
		if st.Prompted[a.Name].After(st.Prompted[active.Name]) {
			active = a
		}
	}
	if multi {
		r.Today = todayUsage(st).ClaudeBy[active.Name]
	}

	// One cost column per account and a total, or a single column.
	cells := func(by map[string]float64, total float64) []string {
		if !multi {
			return []string{usd(total)}
		}
		var c []string
		for _, a := range accounts {
			c = append(c, usd(by[a.Name]))
		}
		return append(c, usd(total))
	}
	if multi {
		header := []string{""}
		for _, a := range accounts {
			header = append(header, a.Name)
		}
		r.Rows = append(r.Rows, append(header, "total"))
	}
	for _, k := range recentDays(st, 7) {
		d := st.Days[k]
		r.Rows = append(r.Rows, append([]string{k[5:]}, cells(d.ClaudeBy, d.ClaudeCost)...))
	}
	month, monthBy := 0.0, map[string]float64{}
	for _, d := range st.Days {
		month += d.ClaudeCost
		for name, c := range d.ClaudeBy {
			monthBy[name] += c
		}
	}
	r.Rows = append(r.Rows, append([]string{"month"}, cells(monthBy, month)...))

	for _, a := range accounts {
		c, ok := st.Claude[a.Name]
		if !ok || c.FetchedAt.IsZero() {
			continue
		}
		if a == active {
			q := quota{Stale: time.Since(c.FetchedAt) > apiStale}
			q.Percent, q.ResetsAt = c.pool()
			if multi {
				q.Tag = tag(a.Name, accounts)
			}
			r.Quotas = append(r.Quotas, q)
		}
		if multi {
			if len(r.Notes) > 0 {
				r.Notes = append(r.Notes, "")
			}
			mark := "○ "
			if a == active {
				mark = "● "
			}
			title := mark + a.Name
			if c.Plan != "" {
				title += " (" + strings.ToUpper(c.Plan[:1]) + c.Plan[1:] + ")"
			}
			r.Notes = append(r.Notes, title)
		}
		for _, l := range c.Limits {
			r.Notes = append(r.Notes, limitNote(l.Name, l.Percent, l.ResetsAt))
		}
		if c.Limit > 0 {
			r.Notes = append(r.Notes, creditsNote+fmt.Sprintf("%s / %s used", usd(c.Used), usd(c.Limit)))
		}
		r.Notes = append(r.Notes, "Updated "+c.FetchedAt.Local().Format("15:04"))
	}
	return r
}

// tag is the shortest uppercase prefix of name that no other account shares:
// "W" for work and personal, "WO" and "WI" for work and wife.
func tag(name string, accounts []claudeAccount) string {
	upper := []rune(strings.ToUpper(name))
	for n := 1; n < len(upper); n++ {
		prefix, shared := string(upper[:n]), false
		for _, a := range accounts {
			shared = shared || (a.Name != name && strings.HasPrefix(strings.ToUpper(a.Name), prefix))
		}
		if !shared {
			return prefix
		}
	}
	return string(upper)
}

// pool is the percent of what you spend from now, and whether that is credits:
//
//	no limit used up           → the fullest weekly limit
//	a limit used up, credits   → the monthly credits, which pay from then on
//	a limit used up, no credits → 100%: you are blocked until it resets
//
// The 5-hour session limit counts only once used up: it resets too often to watch.
func (c ClaudeQuota) pool() (percent float64, resetsAt time.Time) {
	var blockedUntil time.Time
	for _, l := range c.Limits {
		p := current(l.Percent, l.ResetsAt)
		if p >= 100 && l.ResetsAt.After(blockedUntil) {
			blockedUntil = l.ResetsAt
		}
		if l.Kind != "session" && (p > percent || resetsAt.IsZero()) {
			percent, resetsAt = p, l.ResetsAt
		}
	}
	switch {
	case !blockedUntil.IsZero() && c.Limit > 0:
		// The credits API gives no reset time.
		return c.Percent, time.Time{}
	case !blockedUntil.IsZero():
		return 100, blockedUntil
	}
	return percent, resetsAt
}

// creditsNote labels the monthly credits, which pay only for use past the plan limits.
const creditsNote = "Monthly credits (after limits): "

// current is the used percent, or 0 when the limit has reset since it was read.
// A failed refresh, such as an expired login token, can leave it past its reset.
func current(percent float64, resetsAt time.Time) float64 {
	if !resetsAt.IsZero() && now.After(resetsAt) {
		return 0
	}
	return percent
}

func limitNote(name string, percent float64, resetsAt time.Time) string {
	at := resetsAt.Round(time.Minute).Local().Format("Mon 02 Jan 15:04")
	switch {
	case resetsAt.IsZero():
		return fmt.Sprintf("%s: %.0f%% used", name, percent)
	case now.After(resetsAt):
		return fmt.Sprintf("%s: 0%% used, reset %s", name, at)
	}
	return fmt.Sprintf("%s: %.0f%% used, resets %s", name, percent, at)
}

func codexReport(st *State) report {
	r := report{Name: "Codex", Glyph: "", GlyphFG: fgText, PNG: pngCodex, Today: todayUsage(st).CodexCost}
	for _, k := range recentDays(st, 7) {
		d := st.Days[k]
		r.Rows = append(r.Rows, []string{k[5:], usd(d.CodexCost), humanTokens(d.CodexTokens)})
	}
	month := 0.0
	for _, d := range st.Days {
		month += d.CodexCost
	}
	r.Rows = append(r.Rows, []string{"month", usd(month)})
	// The bar shows the weekly limit, or the monthly credits once it is used up.
	// The weekly limit comes from the session logs, so only the credits go stale.
	if x := st.Codex; !x.SeenAt.IsZero() {
		resetsAt := time.Unix(x.ResetsAt, 0)
		r.Quotas = []quota{{Percent: current(x.UsedPercent, resetsAt), ResetsAt: resetsAt}}
		r.Notes = append(r.Notes, limitNote(window(x.WindowMinutes), x.UsedPercent, resetsAt))
	}
	if s := st.Spend; s.Limit > 0 {
		if len(r.Quotas) == 0 || r.Quotas[0].Percent >= 100 {
			r.Quotas = []quota{{Percent: s.Percent, ResetsAt: time.Unix(s.ResetsAt, 0), Stale: time.Since(s.FetchedAt) > apiStale}}
		}
		line := creditsNote + fmt.Sprintf("%.0f / %.0f used", s.Used, s.Limit)
		if rate := cfg.CodexCreditUSD; rate > 0 {
			line += fmt.Sprintf(" ≈ %s / %s", usd(s.Used*rate), usd(s.Limit*rate))
		}
		r.Notes = append(r.Notes, line+", resets "+time.Unix(s.ResetsAt, 0).Local().Format("02 Jan"))
	}
	return r
}

// level buckets quota use: ok below 60%, warn below 85%, crit above.
func (q quota) level() string {
	switch {
	case q.Stale:
		return "stale"
	case q.Percent >= 85:
		return "crit"
	case q.Percent >= 60:
		return "warn"
	}
	return "ok"
}

// level is the worst level of the report's quotas.
func (r report) level() string {
	rank := map[string]int{"ok": 0, "stale": 1, "warn": 2, "crit": 3}
	level := "ok"
	for _, q := range r.Quotas {
		if l := q.level(); rank[l] > rank[level] {
			level = l
		}
	}
	return level
}

// text is "78% 4d": the percent and the time to its reset, or "W 78% 4d"
// with several accounts.
func (q quota) text() string {
	s := fmt.Sprintf("%.0f%%", q.Percent)
	if q.Stale {
		s += "?"
	}
	if left := timeLeft(q.ResetsAt); left != "" {
		s += " " + left
	}
	if q.Tag != "" {
		s = q.Tag + " " + s
	}
	return s
}

// percentText joins the quotas: "W 6%  P 33%".
func (r report) percentText() string {
	var parts []string
	for _, q := range r.Quotas {
		parts = append(parts, q.text())
	}
	return strings.Join(parts, "  ")
}

// table lays out Rows in columns: the first left-aligned, the rest right-aligned.
func (r report) table() []string {
	var widths []int
	for _, row := range r.Rows {
		for i, cell := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], len([]rune(cell)))
		}
	}
	var lines []string
	for _, row := range r.Rows {
		line := fmt.Sprintf("%-*s", widths[0], row[0])
		for i, cell := range row[1:] {
			line += fmt.Sprintf("  %*s", widths[i+1], cell)
		}
		lines = append(lines, line)
	}
	return lines
}

// text is the plain line for launchers: "Claude $27.89 78%".
func (r report) text() string {
	s := r.Name + " " + usd(r.Today)
	if len(r.Quotas) > 0 {
		s += " " + r.percentText()
	}
	return s
}

// pango renders the bar text for Waybar and i3status (markup = pango).
func (r report) pango() string {
	colors := map[string]string{"ok": fgGood, "warn": fgWarn, "crit": fgBad, "stale": fgDim}
	out := fmt.Sprintf(`<span foreground="%s" font_family="Symbols Nerd Font Mono">%s</span> `, r.GlyphFG, r.Glyph) +
		fmt.Sprintf(`<span foreground="%s">%s</span>`, fgText, usd(r.Today))
	for _, q := range r.Quotas {
		out += fmt.Sprintf(`  <span foreground="%s">%s</span>`, colors[q.level()], q.text())
	}
	return out
}

// Waybar custom module output with return-type json.
type waybarBlock struct {
	Text    string `json:"text"`
	Tooltip string `json:"tooltip"`
	Class   string `json:"class"`
}

func (r report) waybar() waybarBlock {
	tip := fmt.Sprintf("<b>%s</b>  (%s)\n<tt>%s</tt>", r.Name, r.basis(), strings.Join(r.table(), "\n"))
	if len(r.Notes) > 0 {
		tip += "\n\n" + strings.Join(r.Notes, "\n")
	}
	return waybarBlock{Text: r.pango(), Tooltip: tip, Class: r.level()}
}

// menubarItem is one macOS menu bar item for contrib/macos/BurnrateBar.swift.
type menubarItem struct {
	Title string   `json:"title"`
	Color string   `json:"color,omitempty"`
	Icon  []byte   `json:"icon"` // black PNG on transparent
	Tint  string   `json:"tint,omitempty"`
	Lines []string `json:"lines"` // "---" is a separator
}

func (r report) menubar() menubarItem {
	m := menubarItem{Title: usd(r.Today), Icon: r.PNG, Tint: r.Tint}
	if len(r.Quotas) > 0 {
		m.Title += "  " + r.percentText()
	}
	switch r.level() {
	case "warn":
		m.Color = fgWarn
	case "crit":
		m.Color = fgBad
	}
	m.Lines = append([]string{r.Name + " (" + r.basis() + ")"}, r.table()...)
	if len(r.Notes) > 0 {
		m.Lines = append(m.Lines, "---")
		for _, n := range r.Notes {
			if n == "" {
				n = "---"
			}
			m.Lines = append(m.Lines, n)
		}
	}
	return m
}

func (r report) basis() string {
	if r.Basis != "" {
		return r.Basis
	}
	return "estimate at API prices"
}

// timeLeft is days to t, rounded to the nearest day, or hours in the last
// day; "" when unknown or past. Rounding keeps 3d 22h from reading as 3d.
func timeLeft(t time.Time) string {
	d := t.Sub(now)
	switch {
	case t.IsZero() || d <= 0:
		return ""
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int((d+12*time.Hour)/(24*time.Hour)))
	}
	return fmt.Sprintf("%dh", max(1, int((d+time.Hour-1)/time.Hour)))
}

func usd(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}

func window(min int64) string {
	switch min {
	case 300:
		return "Session"
	case 10080:
		return "Weekly"
	}
	return fmt.Sprintf("%dm limit", min)
}

func humanTokens(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.0fk", float64(n)/1e3)
	}
	return fmt.Sprint(n)
}

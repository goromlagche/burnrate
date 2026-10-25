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
	Name      string
	Glyph     string // Nerd Font icon for Linux bars
	GlyphFG   string
	PNG       []byte // template icon for the macOS menu bar
	Today     float64
	Percent   float64 // quota used, if HasQuota
	HasQuota  bool
	Stale     bool
	Window    string     // quota window label: mo, wk, 5h
	Rows      [][]string // recent days: date, cost[, tokens]
	MonthCost float64
	Notes     []string
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

func claudeReport(st *State) report {
	r := report{Name: "Claude", Glyph: "", GlyphFG: fgClaude, PNG: pngClaude, Today: todayUsage(st).ClaudeCost}
	for _, d := range st.Days {
		r.MonthCost += d.ClaudeCost
	}
	for _, k := range recentDays(st, 7) {
		r.Rows = append(r.Rows, []string{k[5:], usd(st.Days[k].ClaudeCost)})
	}
	if c := st.Claude; !c.FetchedAt.IsZero() {
		r.HasQuota, r.Percent, r.Window = true, c.Percent, "mo"
		r.Stale = time.Since(c.FetchedAt) > apiStale
		r.Notes = []string{
			fmt.Sprintf("Monthly cap: %s / %s (%.1f%%)", usd(c.Used), usd(c.Limit), c.Percent),
			"Updated " + c.FetchedAt.Local().Format("15:04"),
		}
	}
	return r
}

func codexReport(st *State) report {
	r := report{Name: "Codex", Glyph: "", GlyphFG: fgText, PNG: pngCodex, Today: todayUsage(st).CodexCost}
	for _, d := range st.Days {
		r.MonthCost += d.CodexCost
	}
	for _, k := range recentDays(st, 7) {
		d := st.Days[k]
		r.Rows = append(r.Rows, []string{k[5:], usd(d.CodexCost), humanTokens(d.CodexTokens)})
	}
	// The bar shows the monthly spend control when the workspace sets one, else the weekly limit.
	if s := st.Spend; s.Limit > 0 {
		r.HasQuota, r.Percent, r.Window = true, s.Percent, "mo"
		r.Stale = time.Since(s.FetchedAt) > apiStale
		capLine := fmt.Sprintf("Monthly cap: %.0f / %.0f credits (%.0f%%)", s.Used, s.Limit, s.Percent)
		if rate := cfg.CodexCreditUSD; rate > 0 {
			capLine += fmt.Sprintf(" ≈ %s / %s", usd(s.Used*rate), usd(s.Limit*rate))
		}
		r.Notes = append(r.Notes, capLine,
			"Credits are spent only beyond your plan's included usage",
			"Resets "+time.Unix(s.ResetsAt, 0).Local().Format("Mon 02 Jan 15:04"))
	}
	if x := st.Codex; !x.SeenAt.IsZero() {
		if !r.HasQuota {
			r.HasQuota, r.Percent, r.Window = true, x.UsedPercent, window(x.WindowMinutes)
		}
		r.Notes = append(r.Notes,
			fmt.Sprintf("%s limit: %.0f%% used", window(x.WindowMinutes), x.UsedPercent),
			"Resets "+time.Unix(x.ResetsAt, 0).Local().Format("Mon 02 Jan 15:04"))
	}
	return r
}

// level buckets quota use: ok below 60%, warn below 85%, crit above.
func (r report) level() string {
	switch {
	case !r.HasQuota:
		return "ok"
	case r.Stale:
		return "stale"
	case r.Percent >= 85:
		return "crit"
	case r.Percent >= 60:
		return "warn"
	}
	return "ok"
}

func (r report) percentText() string {
	s := fmt.Sprintf("%.0f%%", r.Percent)
	if r.Stale {
		s += "?"
	}
	return s
}

// table lays out Rows plus a month total in fixed-width columns.
func (r report) table() []string {
	var lines []string
	for _, row := range r.Rows {
		line := fmt.Sprintf("%-5s  %9s", row[0], row[1])
		if len(row) > 2 {
			line += fmt.Sprintf("  %6s", row[2])
		}
		lines = append(lines, line)
	}
	return append(lines, fmt.Sprintf("%-5s  %9s", "month", usd(r.MonthCost)))
}

// pango renders the bar text for Waybar and i3status (markup = pango).
func (r report) pango() string {
	colors := map[string]string{"ok": fgGood, "warn": fgWarn, "crit": fgBad, "stale": fgDim}
	out := fmt.Sprintf(`<span foreground="%s" font_family="Symbols Nerd Font Mono">%s</span> `, r.GlyphFG, r.Glyph) +
		fmt.Sprintf(`<span foreground="%s">%s</span>`, fgText, usd(r.Today))
	if r.HasQuota {
		out += fmt.Sprintf(`  <span foreground="%s">%s</span> <span foreground="%s" size="small">%s</span>`,
			colors[r.level()], r.percentText(), fgDim, r.Window)
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
	tip := fmt.Sprintf("<b>%s</b>  (estimate at API prices)\n<tt>%s</tt>", r.Name, strings.Join(r.table(), "\n"))
	if len(r.Notes) > 0 {
		tip += "\n\n" + strings.Join(r.Notes, "\n")
	}
	return waybarBlock{Text: r.pango(), Tooltip: tip, Class: r.level()}
}

// menubarItem is one macOS menu bar item for contrib/macos/BurnrateBar.swift.
type menubarItem struct {
	Title string   `json:"title"`
	Color string   `json:"color,omitempty"`
	Icon  []byte   `json:"icon"`  // template PNG
	Lines []string `json:"lines"` // "---" is a separator
}

func (r report) menubar() menubarItem {
	m := menubarItem{Title: usd(r.Today), Icon: r.PNG}
	if r.HasQuota {
		m.Title += "  " + r.percentText()
	}
	switch r.level() {
	case "warn":
		m.Color = fgWarn
	case "crit":
		m.Color = fgBad
	}
	m.Lines = append([]string{r.Name + " (estimate at API prices)"}, r.table()...)
	if len(r.Notes) > 0 {
		m.Lines = append(append(m.Lines, "---"), r.Notes...)
	}
	return m
}

func usd(v float64) string {
	return fmt.Sprintf("$%.2f", v)
}

func window(min int64) string {
	switch min {
	case 300:
		return "5h"
	case 10080:
		return "wk"
	}
	return fmt.Sprintf("%dm", min)
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

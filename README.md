<h1 align="center">burnrate</h1>

<p align="center">
  Today's <b>Claude Code</b> and <b>Codex</b> spend, and your quota, in the status bar.
</p>

<p align="center">
  <code>✳ $27.89  78% mo</code>&nbsp;&nbsp;&nbsp;<code>◎ $2.35  5% mo</code>
</p>

<p align="center">
  Waybar · i3status · macOS menu bar · no dependencies
</p>

---

## Features

- **Today's spend.** Each response is priced at API list prices.
- **Quota.** Claude's monthly cap and Codex's monthly workspace credits. The weekly limit is the fallback.
- **History.** Hover or click to see the last 7 days and the month total.
- **Fast.** Logs are read incrementally. A run takes milliseconds.
- **Exact counts.** Repeats, resumed sessions and forked sessions count once.

## Install

Requires Go 1.22 or later.

```sh
go install github.com/goromlagche/burnrate@latest
burnrate install
```

- **Linux:** starts a systemd user timer that runs every minute. Add the modules from [`contrib/waybar/config.jsonc`](contrib/waybar/config.jsonc) to Waybar. Icons need a [Nerd Font](https://www.nerdfonts.com/).
- **macOS:** builds a native menu bar app and starts it at login. Needs `xcode-select --install` once. On the first run, allow access to the `Claude Code-credentials` Keychain item.
- **Remove:** `burnrate uninstall`.

## How it works

| | Today's $ | Quota |
|---|---|---|
| Claude Code | `~/.claude/projects` logs × API prices | Monthly cap, from the endpoint that `/usage` uses |
| Codex | `~/.codex/sessions` logs × API prices | Monthly workspace credits, from the endpoint that `/status` uses |

- Dollar figures are estimates at public API prices. They are not your bill.
- Prices are in [`pricing.go`](pricing.go). Models without a price count as $0.
- Codex priority (fast) turns cost 2×.
- Deduplication follows [T3 Code](https://github.com/pingdotgg/t3code) and [Orca](https://github.com/stablyai/orca).

## Config

Optional. `~/.config/burnrate/config.json`:

```json
{ "codex_credit_usd": <USD per credit> }
```

OpenAI reports Codex credits, not dollars. Divide what you pay by the credits you get. burnrate then shows the cap in dollars.

## Privacy

- All work is local, except two read-only requests every 5 minutes:
  - `api.anthropic.com/api/oauth/usage` with your Claude Code token
  - `chatgpt.com/backend-api/wham/usage` with your Codex token
- burnrate does not refresh, store or log tokens.
- Both endpoints are undocumented and can change.
- Output goes to `~/.cache/burnrate/`. Only you can read it.

## Development

```sh
make test
make install   # build from this checkout and install
go run . --json
```

---

<sub>Claude, Codex and OpenAI are trademarks of their owners. Icons from <a href="https://www.nerdfonts.com/">Nerd Fonts</a> (Codicons). Not affiliated with Anthropic or OpenAI. MIT license.</sub>

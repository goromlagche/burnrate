<h1 align="center">burnrate</h1>

<p align="center">
  Today's <b>Claude Code</b> and <b>Codex</b> spend, and your quota, in the status bar.
</p>

<p align="center">
  <img src="docs/waybar.svg" alt="burnrate in Waybar: Claude $27.89 at 78% of the monthly cap, Codex $2.35 at 5%" width="320">
  &nbsp;
  <img src="docs/macos-menubar.png" alt="burnrate in the macOS menu bar: Codex $1.03 at 5%, Claude $0.48 at 78%" width="320">
</p>

<p align="center">
  Waybar · i3status · macOS menu bar
</p>

---

## Install

```sh
go install github.com/goromlagche/burnrate@v0.1.2
burnrate install
```

| | `burnrate install` does | You do once |
|---|---|---|
| **Linux** | Starts a systemd timer. It runs every minute. | Add [`contrib/waybar/config.jsonc`](contrib/waybar/config.jsonc) to Waybar. Use a [Nerd Font](https://www.nerdfonts.com/). |
| **macOS** | Builds a menu bar app. It starts at login. | Run `xcode-select --install`. Allow Keychain access on the first run. |

For i3status, set `markup = "pango"` and read `~/.cache/burnrate/claude.txt` and `codex.txt`.

Remove it with `burnrate uninstall`. Go 1.22 or later.

## What you see

- **Today's spend**, priced at API list prices.
- **Quota %**: Claude's monthly cap, and Codex's monthly workspace credits (else the weekly limit).
- **History** on hover or click: the last 7 days and the month total.

## How it works

- Reads `~/.claude/projects` and `~/.codex/sessions`. It reads only new lines, so a run takes milliseconds.
- Counts each response once, across repeats, resumed sessions and forked sessions.
- Gets the quota from the endpoints that Claude Code's `/usage` and Codex's `/status` use.
- Dollar figures are estimates, not your bill. Prices are in [`pricing.go`](pricing.go).

## Config

Optional, in `~/.config/burnrate/config.json`:

```json
{ "codex_credit_usd": <USD per credit> }
```

Codex reports credits, not dollars. Set the price of one credit to see the cap in dollars.

## Privacy

- Two read-only requests every 5 minutes, with the logins you already have:
  - `api.anthropic.com/api/oauth/usage`
  - `chatgpt.com/backend-api/wham/usage`
- Tokens are never refreshed, stored or logged.
- Both endpoints are undocumented and can change.
- Output stays in `~/.cache/burnrate/`, readable only by you.

## Development

```sh
make test
make install
```

---

<sub>Deduplication follows <a href="https://github.com/pingdotgg/t3code">T3 Code</a> and <a href="https://github.com/stablyai/orca">Orca</a>. Icons from <a href="https://www.nerdfonts.com/">Nerd Fonts</a>. Claude, Codex and OpenAI are trademarks of their owners. Not affiliated with Anthropic or OpenAI. MIT license.</sub>

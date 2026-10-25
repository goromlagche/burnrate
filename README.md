# burnrate

How fast are you burning through your AI budget? `burnrate` puts today's
**Claude Code** and **Codex** spend, plus your quota, in your status bar.

```
 ✳ $27.99  78% mo    ◎ $2.35  2% wk
```

Hover (Waybar) or click (macOS) for the last 7 days and the month total.

## What it shows

| | Today's $ | Quota |
|---|---|---|
| Claude Code | Estimated from `~/.claude/projects/**/*.jsonl` at API list prices | Monthly usage-credit cap, from the same endpoint Claude Code's `/usage` uses |
| Codex | Estimated from `~/.codex/sessions/**/*.jsonl` at API list prices | Monthly workspace spend control, from the same endpoint Codex's `/status` uses; else the weekly rate limit from the session logs |

Dollar figures are **estimates at public API prices**, not your bill. Prices
live in [`pricing.go`](pricing.go); update them when providers change rates.
Models without a listed price (for example `codex-auto-review`) count tokens
but no dollars. Codex turns on the priority (fast) tier cost 2x.

Each billed event is counted once: Claude by message and request ID, Codex by
turn and running total, so repeated lines, resumed sessions and forked
sessions do not double count. The approach follows
[T3 Code](https://github.com/pingdotgg/t3code) and
[Orca](https://github.com/stablyai/orca).

Logs are read incrementally (only new bytes since the last run), so a run takes
milliseconds after the first scan of the month.

## Install

Requires Go 1.22 or later.

### Linux (Waybar or i3status)

```sh
make install-linux   # symlinks the binary and a systemd user timer (runs every minute)
```

Then add the modules from [`contrib/waybar/config.jsonc`](contrib/waybar/config.jsonc)
to your Waybar config. Icons need a [Nerd Font](https://www.nerdfonts.com/).

For i3status, enable `markup = "pango"` and add a `read_file` block pointing at
`~/.cache/burnrate/claude.txt` and `codex.txt`.

### macOS (menu bar)

```sh
xcode-select --install   # once, for swiftc
make install-macos       # builds the menu bar app and starts it at login
```

[`BurnrateBar.swift`](contrib/macos/BurnrateBar.swift) is a small native app
with no dependencies. It runs `burnrate --menubar` every minute.

The first run asks for access to the `Claude Code-credentials` Keychain item.
Choose **Always Allow** to show the monthly quota.

## Config (optional)

`~/.config/burnrate/config.json`:

```json
{ "codex_credit_usd": 0.04 }
```

OpenAI reports the Codex spend control in credits. The price of a credit
depends on your agreement; set it to also show the cap in dollars.

## Privacy and security

- Everything runs locally except two read-only `GET`s, each every 5 minutes,
  with the login you already have:
  - `api.anthropic.com/api/oauth/usage` with the Claude Code OAuth token
  - `chatgpt.com/backend-api/wham/usage` with the token in `~/.codex/auth.json`
- burnrate never refreshes, stores or logs either token.
- Neither endpoint is a documented public API, so both may change.
- Output and state are written to `~/.cache/burnrate/` with owner-only permissions.

## Development

```sh
make test
go run . --json          # full state
go run . --menubar
```

Claude, Codex and OpenAI are trademarks of their owners. The icons come from
[Nerd Fonts](https://www.nerdfonts.com/) (Codicons). This project is not
affiliated with Anthropic or OpenAI.

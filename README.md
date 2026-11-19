<h1 align="center">burnrate</h1>

<p align="center">
  Track <b>Claude Code</b> and <b>Codex</b> usage from your status bar.<br>
  Today's estimated cost, quota usage and recent history, always visible.
</p>

<p align="center">
  <a href="https://github.com/goromlagche/burnrate/releases"><img src="https://img.shields.io/github/v/tag/goromlagche/burnrate?label=release" alt="release"></a>
  <a href="https://pkg.go.dev/github.com/goromlagche/burnrate"><img src="https://pkg.go.dev/badge/github.com/goromlagche/burnrate.svg" alt="Go reference"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/goromlagche/burnrate" alt="MIT license"></a>
</p>

<table align="center">
  <tr>
    <td align="center">
      <img src="docs/waybar.svg" alt="burnrate in Waybar: Claude $27.89 at 78% of the monthly cap, Codex $2.35 at 5%" width="320"><br>
      <sub>Linux · Waybar</sub>
    </td>
    <td align="center">
      <img src="docs/macos-bar.png" alt="burnrate in the macOS menu bar: Codex $1.29 at 5%, Claude $0.88 at 78%" width="320"><br>
      <sub>macOS · menu bar</sub>
    </td>
  </tr>
</table>

## Install

Ask your coding agent (Claude Code, Codex, ...):

```
Install burnrate from github.com/goromlagche/burnrate
```

It follows [`AGENTS.md`](AGENTS.md): it checks the requirements, installs, and asks before it edits your bar config.

### Manual

Requires Go 1.22 or later.

**Linux**

```sh
go install github.com/goromlagche/burnrate@v0.2.1
burnrate install
```

- Starts a systemd user timer. It runs every 15 seconds.
- Add the modules from [`contrib/waybar/config.jsonc`](contrib/waybar/config.jsonc) to Waybar.
- The icons need a [Nerd Font](https://www.nerdfonts.com/).

<details>
<summary>i3status instead of Waybar</summary>

Set `markup = "pango"` in `general`. Add `read_file` blocks for `~/.cache/burnrate/claude.txt` and `codex.txt`, with absolute paths.

</details>

**macOS**

```sh
xcode-select --install   # once, to build the menu bar app
go install github.com/goromlagche/burnrate@v0.2.1
burnrate install
```

- Builds a native menu bar app. It starts at login.
- On the first run, allow access to the `Claude Code-credentials` Keychain item.

Remove it with `burnrate uninstall`.

## What you see

- **Today's cost**, estimated at API list prices. It is not your bill.
- **Quota used (%)**: what you spend from now, for Claude and for Codex. Hover or click to see each limit.
  - Inside your plan: the fullest weekly limit, for example `19%`.
  - A limit used up, with credits on: the monthly credits, marked `$`, for example `12%$`.
  - A limit used up, with credits off: `100%` in red, until it resets.
  - The 5-hour session limit counts only once it is used up.
- **History** on hover or click: the last 7 days and the month total.

## Privacy and how it works

Reads local Claude Code and Codex logs incrementally. Deduplicates responses across resumed and forked sessions. Prices are in [`pricing.go`](pricing.go). Output stays in `~/.cache/burnrate/`, readable only by you.

With several Claude accounts, burnrate also notes the time of your last prompt in each, to pick the account for the bar. It keeps the time only, never the text.

<details>
<summary>Usage API details</summary>

Every 5 minutes, burnrate sends two read-only requests with the logins you already have:

- `api.anthropic.com/api/oauth/usage`, the endpoint that Claude Code's `/usage` uses
- `chatgpt.com/backend-api/wham/usage`, the endpoint that Codex's `/status` uses

It never refreshes, stores or logs the tokens. Both endpoints are undocumented and can change.

</details>

## Optional configuration

`~/.config/burnrate/config.json`:

```json
{ "codex_credit_usd": 0.05 }
```

Codex reports credits, not dollars. Set the price of one credit to see the cap in dollars. `0.05` is an example; use your own price.

### Several Claude accounts

If you run Claude Code with more than one `CLAUDE_CONFIG_DIR`, for example work and personal, list them:

```json
{
  "claude_accounts": [
    { "name": "work",     "dir": "~/.claude" },
    { "name": "personal", "dir": "~/.claude-personal" }
  ]
}
```

- `burnrate install` finds your Claude Code config dirs and prints this list for you to paste.
- `dir` takes `~/` and `$VARS`. `name` is optional and defaults to the dir name.
- **Cost** adds up all accounts. The tooltip shows one column per account and a total.
- **Quota** is per account. The bar shows the account you last typed a prompt in, tagged with the start of its name: `$27.30  W 19%`. A session that keeps working on its own does not take over the bar. The tooltip lists the limits of each account.
- Without `claude_accounts`, burnrate reads `~/.claude` only.
- On macOS, burnrate reads the Keychain for `~/.claude` only. For other dirs it reads `<dir>/.credentials.json`, so their quota can be missing.

## Development

```sh
make test
make install
```

---

<sub>Deduplication follows <a href="https://github.com/pingdotgg/t3code">T3 Code</a> and <a href="https://github.com/stablyai/orca">Orca</a>. Icons from <a href="https://www.nerdfonts.com/">Nerd Fonts</a>. Claude, Codex and OpenAI are trademarks of their owners. Not affiliated with Anthropic or OpenAI. MIT license.</sub>

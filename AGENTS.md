# Install burnrate (for coding agents)

Follow these steps when a user asks you to install burnrate. Ask before you edit any file the user owns.

## Rules

- Never print, copy or log `~/.claude/.credentials.json`, `~/.codex/auth.json` or the macOS Keychain item.
- Do not guess the Codex credit price. Ask the user.
- Stop and report if a step fails. Do not work around it.

## 1. Check

```sh
go version   # needs 1.22 or later
```

If Go is missing, ask the user to install it. On macOS, also check `xcode-select -p`. If that fails, ask the user to run `xcode-select --install`.

## 2. Install

```sh
go install github.com/goromlagche/burnrate@latest
"$(go env GOPATH)/bin/burnrate" install
```

Success prints `installed:` and two lines, for example `$27.89 78%` and `$2.35 5%`.

- **Linux:** this starts the systemd user timer `burnrate.timer`. It runs every minute.
- **macOS:** this builds the menu bar app and starts it at login. Tell the user to choose **Always Allow** when macOS asks for the `Claude Code-credentials` Keychain item.

## 3. Show it in the bar (Linux)

Find the bar: `pgrep -a waybar` or `pgrep -a i3status`. Ask the user before you edit its config.

- **Waybar:** copy the two modules from [`contrib/waybar/config.jsonc`](contrib/waybar/config.jsonc) into the user's config. Add `"custom/claude"` and `"custom/codex"` to a modules list. Reload with `pkill -SIGUSR2 waybar`. The icons need a Nerd Font.
- **i3status:** set `markup = "pango"` in `general`. Add `read_file` blocks for `~/.cache/burnrate/claude.txt` and `codex.txt`, with absolute paths.
- **Other bars:** each run writes `~/.cache/burnrate/claude.json` and `codex.json` as `{"text", "tooltip", "class"}`.

macOS needs no step here.

## 4. Optional: Codex dollars

Codex reports its monthly cap in credits. Ask the user for the price of one credit. Then write `~/.config/burnrate/config.json`:

```json
{ "codex_credit_usd": 0.05 }
```

Replace `0.05` with the price the user gives.

## 5. Optional: Vorssaint (macOS)

If the user runs [Vorssaint](https://github.com/vorssaint/vorssaint-utils), offer to show burnrate in its Command Bar.

Link burnrate as `burnrate-text`. Under that name it prints one line and ignores the text Vorssaint passes:

```sh
mkdir -p ~/.local/bin
ln -sf "$(command -v burnrate || echo "$(go env GOPATH)/bin/burnrate")" ~/.local/bin/burnrate-text
~/.local/bin/burnrate-text ""   # prints: Claude $27.89 78% · Codex $5.27 5%
```

Then tell the user to add it in Vorssaint. Do not edit Vorssaint's settings yourself:

1. Add a Command Bar script named `burn`.
2. Set the destination to `~/.local/bin/burnrate-text`.
3. Turn on running without an argument.
4. Type `burn` in the Command Bar.

## 6. Verify

```sh
"$(go env GOPATH)/bin/burnrate" --json | head -20
```

Expect `days`, `claude` and `codex` keys. The quota fields can be empty if the user is not logged in to Claude Code or Codex.

## Remove

```sh
"$(go env GOPATH)/bin/burnrate" uninstall
```

# Expander — Windows Keyboard Text Expander

A single Go binary that runs in your Windows session, watches global keyboard
input via a low-level hook, and expands short trigger sequences into full text
using `SendInput`. Expansion values (passwords, snippets, addresses) are
**AES-GCM encrypted** at rest; the master password is kept in **Windows
Credential Manager**. A system-tray icon provides runtime controls, and the app
auto-starts on login via the `HKCU\…\Run` registry key.

> Windows only (`GOOS=windows GOARCH=amd64`), no CGo. See [`docs/PRD.md`](docs/PRD.md)
> for the full specification and [`PLAN.md`](PLAN.md) for the implementation plan.

## How it works

Type a trigger (e.g. `gg`) followed by its terminator key (`Tab`, `Space`, or
`Enter`) within a timing window (default 500 ms). Expander erases the trigger
with backspaces, suppresses the terminator, and types the decrypted expansion.

## Features

- Prefix + per-shortcut terminator trigger model
- `SendInput` keystroke simulation (works everywhere, incl. RDP/terminals — not clipboard paste)
- AES-GCM encrypted expansions (`ENC:` prefix), PBKDF2-SHA256 key derivation (100k iters, 16-byte salt)
- Master password stored in Windows Credential Manager (silent startup)
- Tray icon with three states — Active (green), Paused (yellow), Error (red)
- Tray menu: Pause/Resume · Reload Config · Open Config File · Exit
- Auto-start on login via `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

## Build

```powershell
go build -ldflags="-H windowsgui" -o expander.exe .
```

The `-H windowsgui` flag makes it a GUI-subsystem binary so the running agent
shows no console window.

## Install & first-run setup

1. Copy `expander.exe` to a stable location, e.g. `%LOCALAPPDATA%\Expander\expander.exe`.
2. Run the setup wizard:

   ```powershell
   expander.exe setup
   ```

   **A separate console window titled “Expander Setup” opens** — that is where
   the wizard runs. (Because the agent is a GUI binary it has no console of its
   own, so setup allocates a dedicated one; your original terminal returns to its
   prompt immediately, which is expected.) In that window you:
   - enter and confirm a master password,
   - add one or more shortcuts (trigger, description, terminator, expansion).

   Press **Ctrl+C** at any time to cancel without saving. Press **Enter** on an
   empty trigger to finish; the window pauses at the end so you can read the
   summary.

   Setup writes `%APPDATA%\Expander\config.yml` + `config.salt`, stores the
   master password in Credential Manager, and registers auto-start.

3. Launch the agent (or just reboot — it auto-starts):

   ```powershell
   expander.exe
   ```

   A green tray icon appears. Use the tray menu to pause, reload config after
   edits, open the config file, or exit.

## Configuration

`%APPDATA%\Expander\config.yml` (see [`config.example.yml`](config.example.yml)):

```yaml
settings:
  timing_window_ms: 500   # window from first trigger char to terminator

shortcuts:
  - trigger: "gg"
    description: "Main account password"
    terminator: "Tab"            # Tab | Space | Enter
    expansion: "ENC:<base64…>"   # AES-GCM ciphertext (or plaintext for testing)
```

- `expansion` values prefixed with `ENC:` are encrypted. Plaintext values also
  work (handy for testing) and are re-encrypted next time you run `setup`.
- After editing the file, use the tray **Reload Config** item to apply changes
  without restarting.

## Project layout

```
main.go                 entry point — routes `setup` vs agent
cmd/                    setup wizard + agent wiring + console/paths
internal/config/        YAML model + loader/validation
internal/crypto/        AES-GCM + PBKDF2 + Credential Manager
internal/hook/          WH_KEYBOARD_LL hook + trigger matcher
internal/expander/      backspace-erase + Unicode SendInput injection
internal/tray/          systray icon + menu + state transitions
internal/winutil/       HKCU\Run auto-start + ShellExecute
assets/                 embedded tray icons (+ generator)
```

## Security notes

- The master password is never stored in the config; only Credential Manager
  holds it. Expansions are AES-256-GCM with a per-value random nonce.
- The derived key and decrypted expansions live only in memory while the agent
  runs.

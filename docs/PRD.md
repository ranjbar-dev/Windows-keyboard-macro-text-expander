# Expander — Windows Keyboard Text Expander

A single Go binary that runs in the Windows user session, intercepts keyboard
input globally via a low-level hook, and expands configured trigger sequences
into full text using `SendInput`. Sensitive expansion values (passwords,
snippets) are AES-GCM encrypted in `config.yml`; the master password is stored
in Windows Credential Manager. A system tray icon provides runtime controls.
Auto-start is wired via the `HKCU\Run` registry key.

---

## Goal

- User types a short trigger string (e.g. `gg`) followed by a configured
  terminator key (e.g. `Tab`) within a 500 ms window
- App erases the trigger + terminator via backspaces, then injects the full
  expansion string via `SendInput` keystroke simulation
- Expansion values are AES-GCM encrypted at rest in `config.yml`; decrypted
  at runtime using a master password retrieved from Windows Credential Manager
- System tray icon shows active/paused/error state and exposes: Pause/Resume,
  Reload Config, Open Config File, Exit
- App auto-starts on Windows login via `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
- A `setup` subcommand provides a CLI wizard for first-run: store master
  password in Credential Manager, encrypt and write shortcut entries to config

---

## Out of scope

- Windows Service (Session 0 cannot use keyboard hooks or tray icons)
- Clipboard-based injection (`Ctrl+V` paste method)
- Hot-reload of config file (tray "Reload Config" is sufficient)
- Hotkey combo triggers (e.g. `Ctrl+Alt+G`)
- Idle-timeout auto-expansion (no terminator key)
- GUI config editor (config is edited directly in `config.yml`)
- Multi-user or networked deployment
- macOS / Linux support
- Auto-update mechanism

---

## Context

### Stack & versions

| Concern | Choice |
|---|---|
| Language | Go 1.22+ |
| Config parsing | `gopkg.in/yaml.v3` |
| Tray icon | `github.com/getlantern/systray` |
| Windows syscalls | `golang.org/x/sys/windows` |
| Icon embedding | `github.com/tc-hib/go-winres` |
| Encryption | stdlib `crypto/aes` + `crypto/cipher` (AES-GCM) |
| Key derivation | stdlib `golang.org/x/crypto/pbkdf2` + SHA-256 |
| Credential Manager | `golang.org/x/sys/windows` syscall wrappers |

### Project layout

```
expander/
├── main.go                        # Entry point — routes agent vs setup subcommand
├── cmd/
│   ├── agent.go                   # Starts tray + keyboard hook + expansion engine
│   └── setup.go                   # CLI wizard: encrypt values, store master password
├── internal/
│   ├── config/
│   │   ├── loader.go              # Reads & parses config.yml, validates schema
│   │   └── model.go               # Config and Shortcut structs
│   ├── crypto/
│   │   ├── aes.go                 # AES-GCM encrypt(plaintext, key) / decrypt(ciphertext, key)
│   │   └── credman.go             # Windows Credential Manager read/write via syscall
│   ├── hook/
│   │   ├── keyboard.go            # SetWindowsHookEx WH_KEYBOARD_LL install/uninstall
│   │   └── matcher.go             # Trigger buffer, per-shortcut timing + terminator logic
│   ├── expander/
│   │   └── engine.go              # Backspace + SendInput injection orchestration
│   └── tray/
│       └── tray.go                # systray setup, menu items, state transitions
├── assets/
│   └── icon.ico                   # Bundled tray icon (compiled via go-winres)
├── config.example.yml             # Documented example config shipped with release
├── go.mod
└── go.sum
```

### External dependencies

- **Windows Credential Manager** — stores master password under target name
  `Expander_MasterPassword`
- **`%APPDATA%\Expander\config.yml`** — runtime config file; created on first
  `expander.exe setup` run
- **`%APPDATA%\Expander\config.salt`** — 16-byte random salt for PBKDF2;
  written once during setup, never changed
- **Registry key** —
  `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\Expander` written during
  setup to the full path of `expander.exe`

---

## Approach

### Trigger matching

The keyboard hook maintains an in-memory buffer of printable characters typed
since the last buffer-clearing event (Backspace, Escape, non-printable key, or
successful expansion). On every printable keypress the character is appended
and a `lastCharAt` timestamp is updated. On every terminator keypress
(`Tab` / `Space` / `Enter`) the buffer is compared against all configured
triggers whose `terminator` matches the pressed key. If a match is found and
`(now - firstCharAt) <= timing_window_ms` the expansion fires; otherwise the
buffer clears and the event passes through. Buffer length is capped at
`maxTriggerLen + 1` to prevent unbounded growth.

### Injection sequence

When an expansion fires: (1) suppress the terminator key by returning 1 from
the hook callback, (2) send `len(trigger)` `VK_BACK` keypresses via
`SendInput` to erase the trigger, (3) sleep 15 ms to let the target app
process the backspaces, (4) send the decrypted expansion string character by
character via `SendInput` `KEYEVENTF_UNICODE` events, (5) clear the buffer.

### Encryption model

At setup: a 16-byte random salt is written to `%APPDATA%\Expander\config.salt`.
The master password entered by the user is stored in Windows Credential
Manager. At runtime: master password is retrieved from Credential Manager,
PBKDF2-SHA256 with the stored salt and 100,000 iterations derives a 32-byte
AES key. Each expansion value is encrypted with AES-GCM (random 12-byte nonce
prepended to ciphertext, whole thing base64-encoded, stored as `ENC:<b64>`).
Decryption is done once at startup (or on Reload Config) and held in memory.

### Tray and lifecycle

The tray runs on the main goroutine (required by `systray`). The keyboard hook
and message pump run on a dedicated OS-locked goroutine (`runtime.LockOSThread`
required for `SetWindowsHookEx`). A shared `atomic.Bool` paused flag lets the
tray toggle the hook without locking. On Reload Config the hook is temporarily
unregistered, config is re-read and re-decrypted, then the hook is
re-registered. On Exit, the hook is unregistered cleanly before `os.Exit(0)`.

---

## Assumptions

- Build target is `GOOS=windows GOARCH=amd64`; no 32-bit support needed
- Single Windows user account; no per-user isolation beyond HKCU scope
- `expander.exe` is installed to a stable path (e.g.
  `%LOCALAPPDATA%\Expander\expander.exe`) before `setup` is run, so the
  registry value written is valid across reboots
- `timing_window_ms` defaults to 500 and is configurable in `config.yml`
  under `settings.timing_window_ms`
- Supported terminators: `Tab`, `Space`, `Enter` — mapped to `VK_TAB` (0x09),
  `VK_SPACE` (0x20), `VK_RETURN` (0x0D)
- Tray icon has three states: green (active), yellow (paused), red (error) —
  three separate `.ico` files embedded via `go-winres`
- PBKDF2 iteration count (100,000) and salt length (16 bytes) are compile-time
  constants, not configurable
- `expander.exe setup` is idempotent: re-running it updates the Credential
  Manager entry and regenerates the salt + re-encrypts all existing shortcuts

---

## Config file format

```yaml
# %APPDATA%\Expander\config.yml

settings:
  timing_window_ms: 500

shortcuts:
  - trigger: "gg"
    description: "Main account password"
    terminator: "Tab"
    expansion: "ENC:BASE64ENCODEDCIPHERTEXT..."

  - trigger: "em1"
    description: "Work email address"
    terminator: "Space"
    expansion: "ENC:BASE64ENCODEDCIPHERTEXT..."

  - trigger: "addr"
    description: "Home address"
    terminator: "Enter"
    expansion: "ENC:BASE64ENCODEDCIPHERTEXT..."
```

**Rules:**
- `expansion` values prefixed with `ENC:` are AES-GCM encrypted + base64
- `terminator` must be one of: `Tab`, `Space`, `Enter`
- `trigger` must be 1–32 printable ASCII characters, no whitespace
- `description` is optional free text

---

## Decision log

| # | Decision | Alternatives considered | Reason chosen |
|---|---|---|---|
| 1 | Prefix + terminator trigger model | Hotkey combos, idle-timeout | Most natural typing flow; explicit confirmation via terminator |
| 2 | `SendInput` keystroke simulation | Clipboard paste, hybrid | Works universally across all apps including RDP and terminals |
| 3 | Auto-backspace trigger erasure | Leave trigger in place | Cleanest UX — only expansion text remains |
| 4 | Config in `%APPDATA%\Expander\config.yml` | Next to exe, hot-reload | Standard Windows convention; tray reload is sufficient |
| 5 | Per-shortcut terminator | Global terminator only | Different expansion types naturally need different terminators |
| 6 | Terminator always consumed/suppressed | Pass-through, configurable | Simpler mental model — expansion always owns the output |
| 7 | AES-GCM + PBKDF2 encryption | Plain text, Windows DPAPI | Strong encryption; user controls master password; portable |
| 8 | Master password in Windows Credential Manager | Prompt on startup, env var, keyfile | Silent startup; secure OS-managed storage; no user friction |
| 9 | Single binary + `setup` subcommand | Two binaries, RPC plugin model | YAGNI — simplest viable architecture, easy to ship |
| 10 | Registry `HKCU\Run` auto-start | Windows Service, Task Scheduler | Services can't use keyboard hooks in Session 0; registry is the correct solution |
| 11 | Full tray menu (Exit + Pause + Reload + Open Config) | Exit only | Covers all day-to-day operational needs without a separate UI |

---

## Steps

### Step 1 — Initialise Go module and install dependencies

**Files:** `go.mod`, `go.sum`

**Change:** Run `go mod init expander`. Add dependencies:
- `gopkg.in/yaml.v3`
- `github.com/getlantern/systray`
- `golang.org/x/sys`
- `golang.org/x/crypto`
- `github.com/tc-hib/go-winres`

Install `go-winres` CLI tool globally: `go install github.com/tc-hib/go-winres/cmd/go-winres@latest`.
Create `assets/` directory and place three tray icon files:
`icon_active.ico`, `icon_paused.ico`, `icon_error.ico`.
Run `go-winres init` and configure `winres.json` to embed icons and set
Windows manifest (request `asInvoker` execution level, mark as GUI app so no
console window appears).

**Verify:** `go build ./...` succeeds with no errors.

---

### Step 2 — Implement `internal/config` package

**Files:** `internal/config/model.go` (new), `internal/config/loader.go` (new)

**Change:** `model.go` defines:
```go
type Settings struct {
    TimingWindowMs int `yaml:"timing_window_ms"`
}
type Shortcut struct {
    Trigger     string `yaml:"trigger"`
    Description string `yaml:"description"`
    Terminator  string `yaml:"terminator"` // "Tab" | "Space" | "Enter"
    Expansion   string `yaml:"expansion"`  // "ENC:<b64>" or plaintext
}
type Config struct {
    Settings  Settings   `yaml:"settings"`
    Shortcuts []Shortcut `yaml:"shortcuts"`
}
```

`loader.go` exposes `Load(path string) (*Config, error)`. It reads the YAML
file, unmarshals into `Config`, validates that each shortcut has a non-empty
`trigger`, a valid `terminator`, and a non-empty `expansion`. Returns a
descriptive error for any violation. Defaults `TimingWindowMs` to 500 if
zero.

**Verify:** Unit test in `internal/config/loader_test.go` covering: valid
config parses correctly; missing terminator returns error; missing expansion
returns error; `timing_window_ms` defaults to 500.

---

### Step 3 — Implement `internal/crypto` package

**Files:** `internal/crypto/aes.go` (new), `internal/crypto/credman.go` (new)

**Change:**

`aes.go` exposes:
```go
func DeriveKey(password string, salt []byte) []byte
func Encrypt(plaintext string, key []byte) (string, error)  // returns "ENC:<b64>"
func Decrypt(ciphertext string, key []byte) (string, error) // accepts "ENC:<b64>"
```
`DeriveKey` uses `pbkdf2.Key([]byte(password), salt, 100_000, 32, sha256.New)`.
`Encrypt` generates a random 12-byte nonce, encrypts with AES-GCM, prepends
nonce to ciphertext, base64-encodes, prepends `ENC:`.
`Decrypt` strips `ENC:`, base64-decodes, splits nonce (first 12 bytes) from
ciphertext, decrypts with AES-GCM.

`credman.go` exposes:
```go
func SavePassword(targetName, username, password string) error
func LoadPassword(targetName, username string) (string, error)
func DeletePassword(targetName string) error
```
Uses `windows.CryptProtectData` / Windows Credential Manager API via
`golang.org/x/sys/windows` syscall. Target name constant: `Expander_MasterPassword`.

**Verify:** Unit tests in `internal/crypto/aes_test.go`: encrypt then decrypt
round-trips correctly; wrong key returns error; non-`ENC:` prefix returns
error. Manual test for `credman.go`: `SavePassword` writes, `LoadPassword`
retrieves, `DeletePassword` removes.

---

### Step 4 — Implement `internal/hook` package

**Files:** `internal/hook/keyboard.go` (new), `internal/hook/matcher.go` (new)

**Change:**

`keyboard.go` exposes:
```go
func Install(handler func(vkCode uint32, char rune) bool) error
func Uninstall() error
```
Installs a `WH_KEYBOARD_LL` hook via `SetWindowsHookEx`. The handler receives
the virtual key code and resolved rune for each `WM_KEYDOWN` event. Returning
`true` from the handler suppresses the key (calls `CallNextHookEx` with
non-zero); `false` passes it through. Hook must run on an OS-locked thread
with a Win32 message pump (`GetMessage` loop). Expose `RunMessagePump()` to
drive the pump; intended to be called on a dedicated goroutine with
`runtime.LockOSThread()`.

`matcher.go` exposes:
```go
type Matcher struct { /* unexported */ }
func NewMatcher(shortcuts []config.Shortcut, windowMs int) *Matcher
func (m *Matcher) OnKey(vkCode uint32, char rune) (expansion string, triggerLen int, suppress bool)
```
Maintains a `[]rune` buffer (capped at `maxTriggerLen+1`) and a
`firstCharAt time.Time`. On printable char: append, update timestamp, return
`suppress=false`. On terminator VK (`VK_TAB`, `VK_SPACE`, `VK_RETURN`): scan
shortcuts for trigger+terminator match; if match and timing satisfied return
`expansion`, `triggerLen`, `suppress=true`; else clear buffer, return
`suppress=false`. On `VK_BACK` / `VK_ESCAPE` / any non-printable: clear
buffer, return `suppress=false`.

**Verify:** Unit tests in `internal/hook/matcher_test.go` covering:
exact trigger+terminator match within window fires expansion;
same trigger with wrong terminator does not fire;
trigger typed too slowly (mock `time.Now`) does not fire;
Backspace clears buffer;
buffer cap prevents unbounded growth.

---

### Step 5 — Implement `internal/expander` package

**Files:** `internal/expander/engine.go` (new)

**Change:** Exposes:
```go
type Engine struct { /* unexported */ }
func New(shortcuts []config.Shortcut, windowMs int) *Engine
func (e *Engine) HandleKey(vkCode uint32, char rune) bool
func (e *Engine) SetPaused(paused bool)
func (e *Engine) ReloadShortcuts(shortcuts []config.Shortcut, windowMs int)
```
`HandleKey` is the hook callback. If `e.paused`, return `false` immediately.
Calls `matcher.OnKey`. If expansion returned: fire goroutine that (1) sends
`triggerLen` `VK_BACK` via `SendInput`, (2) sleeps 15 ms, (3) sends each
rune of expansion via `SendInput` `INPUT_KEYBOARD` with `KEYEVENTF_UNICODE`.
`SendInput` wrapper accepts `[]INPUT` slice; build it in one call for
backspaces and one call per Unicode char. Returns `suppress` from matcher.

**Verify:** Integration test using a mock `SendInput` interceptor: given a
shortcut `trigger="gg"`, `terminator="Tab"`, `expansion="hello"`, simulate
key events `g`, `g`, `VK_TAB` within 400 ms — verify mock received 2
backspace inputs then `h`,`e`,`l`,`l`,`o` Unicode inputs and hook returned
`suppress=true` for the Tab.

---

### Step 6 — Implement `internal/tray` package

**Files:** `internal/tray/tray.go` (new)

**Change:** Exposes:
```go
func Run(cfg *tray.Config) // blocks; drives systray.Run on main goroutine
type Config struct {
    OnPauseToggle  func(paused bool)
    OnReloadConfig func() error
    OnOpenConfig   func()
    OnExit         func()
    ConfigPath     string
}
func SetState(state TrayState) // TrayState: Active | Paused | Error
```
`Run` calls `systray.Run(onReady, onExit)`. `onReady` sets the icon
(`icon_active.ico`), tooltip `"Expander — Active"`, and adds menu items:
- `"⏸ Pause"` / `"▶ Resume"` (toggle label + icon on click)
- `"🔄 Reload Config"` — calls `OnReloadConfig`; on error sets Error state
- `"📝 Open Config File"` — calls `OnOpenConfig` which runs
  `ShellExecute(configPath)`
- `"❌ Exit"` — calls `OnExit` then `systray.Quit()`

`SetState` updates icon and tooltip from any goroutine (thread-safe via channel).

**Verify:** Manual smoke test: run `expander.exe`, verify tray icon appears,
all four menu items are clickable, Pause toggles label and icon, Exit closes
the process cleanly.

---

### Step 7 — Implement `cmd/setup.go` — first-run CLI wizard

**Files:** `cmd/setup.go` (new)

**Change:** `RunSetup()` function called when `os.Args[1] == "setup"`. Flow:

1. Prompt for master password (masked input via `golang.org/x/term`)
2. Generate 16-byte random salt, write to `%APPDATA%\Expander\config.salt`
3. Derive AES key from password + salt via `crypto.DeriveKey`
4. Store master password in Credential Manager via `credman.SavePassword`
5. Loop: prompt for trigger, description, terminator (validated), plaintext
   expansion; encrypt via `crypto.Encrypt`; append to in-memory shortcut list
6. Write complete `config.yml` to `%APPDATA%\Expander\config.yml`
7. Write registry key
   `HKCU\Software\Microsoft\Windows\CurrentVersion\Run\Expander` = path to
   current executable
8. Print success summary

If `config.yml` already exists, load existing shortcuts, offer to add more or
re-encrypt all (idempotent re-run support).

**Verify:** Run `expander.exe setup`, complete wizard, confirm:
- `%APPDATA%\Expander\config.yml` exists with `ENC:` prefixed expansions
- `%APPDATA%\Expander\config.salt` exists (16 bytes)
- Credential Manager entry `Expander_MasterPassword` is visible in Windows
  Credential Manager UI
- Registry key is present: `reg query HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v Expander`

---

### Step 8 — Implement `cmd/agent.go` and `main.go` — wire everything together

**Files:** `cmd/agent.go` (new), `main.go` (new)

**Change:**

`main.go`:
```go
func main() {
    if len(os.Args) > 1 && os.Args[1] == "setup" {
        cmd.RunSetup(); return
    }
    cmd.RunAgent()
}
```

`cmd/agent.go` `RunAgent()`:
1. Resolve config path `%APPDATA%\Expander\config.yml` and salt path
2. Load salt from `config.salt`
3. Retrieve master password from Credential Manager
4. Derive AES key
5. Load and validate `config.yml` via `config.Load`
6. Decrypt all `ENC:` expansion values; hold decrypted shortcuts in memory
7. Construct `expander.Engine` with decrypted shortcuts
8. Start hook goroutine: `runtime.LockOSThread()`, `hook.Install(engine.HandleKey)`,
   `hook.RunMessagePump()`
9. Start `tray.Run(...)` on main goroutine with callbacks:
   - `OnPauseToggle` → `engine.SetPaused(paused)`
   - `OnReloadConfig` → uninstall hook, reload + re-decrypt config,
     `engine.ReloadShortcuts`, reinstall hook
   - `OnOpenConfig` → `ShellExecute(configPath)`
   - `OnExit` → `hook.Uninstall()`, `os.Exit(0)`

On any fatal error during startup (Credential Manager missing, config parse
failure, decryption failure): set tray Error state and log to
`%APPDATA%\Expander\error.log`.

**Verify:** `go build -ldflags="-H windowsgui" -o expander.exe .` succeeds.
Run `expander.exe`: tray icon appears green, no console window.

---

## End-to-end verification

1. Run `expander.exe setup`:
   - Enter master password
   - Add shortcut: trigger=`gg`, terminator=`Tab`, expansion=`helloworld`
   - Confirm registry key written and config file created

2. Run `expander.exe` (or reboot to verify auto-start):
   - Tray icon appears green

3. Open Notepad, type `gg` then press `Tab` within 500 ms:
   - `gg` and `Tab` are erased; `helloworld` appears in Notepad

4. Open tray menu → Pause:
   - Icon turns yellow; typing `gg` + `Tab` no longer expands

5. Open tray menu → Resume:
   - Icon turns green; expansion works again

6. Edit `%APPDATA%\Expander\config.yml` (via tray → Open Config File), add a
   new shortcut manually (requires re-running setup to encrypt, or use a
   plaintext expansion for testing)

7. Open tray menu → Reload Config:
   - New shortcut is active without restarting the app

8. Open tray menu → Exit:
   - Process exits cleanly; no ghost tray icon remains

---

## Build command

```bash
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o expander.exe .
```

The `-H windowsgui` flag suppresses the console window. Distribute
`expander.exe` as a single file; users run `expander.exe setup` once, then
launch `expander.exe` directly or let the registry key handle it on next login.
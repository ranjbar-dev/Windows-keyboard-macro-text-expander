# Expander — Implementation Plan

Derived from `docs/PRD.md`. PRD is the source of truth; if this plan and the
PRD ever disagree, the PRD wins.

Target: `GOOS=windows GOARCH=amd64`, Go 1.22+, **no CGo** (all Win32 via
`golang.org/x/sys/windows` + lazy DLL syscalls).

---

## Phase dependency graph

```
P1 (module + deps + icons)
        │
        ├──────────────┬───────────────┐
        ▼              ▼               ▼
P2 (config)      P3 (crypto)     P6 (tray)
        │              │               │
        └──────┬───────┘               │
               ▼                        │
        P4 (hook: matcher + keyboard)   │
               ▼                        │
        P5 (expander engine)            │
               │                        │
               ├────────────────────────┘
               ▼
        P7 (cmd/setup.go)
               ▼
        P8 (cmd/agent.go + main.go + final build)
```

Implemented in numeric order; each phase builds + tests green before the next.

---

## Phase 1 — Module, dependencies, icon assets

**Files:** `go.mod`, `go.sum`, `assets/genicons.go` (`//go:build ignore`),
`assets/icon_active.ico`, `assets/icon_paused.ico`, `assets/icon_error.ico`,
`assets/icons.go` (go:embed), `config.example.yml`.

- `go mod init expander`; add `gopkg.in/yaml.v3`, `github.com/getlantern/systray`,
  `golang.org/x/sys`, `golang.org/x/crypto`.
- Generate three 32bpp BMP-backed `.ico` files (green / yellow / red) via the
  `genicons` script and embed them with `go:embed` (`assets/icons.go` exposes
  `Active`, `Paused`, `Error []byte`).
- **Deviation from PRD note:** PRD lists `go-winres` for icon embedding. The
  *functional* requirement is a 3-state **tray** icon, which `systray.SetIcon`
  drives from in-memory `.ico` bytes — so tray icons are embedded via
  `go:embed`, keeping the build a single `go build` with no extra tooling. The
  exe's Explorer icon (cosmetic, not a PRD functional requirement) is omitted.

**Verify:** `go build ./...` succeeds.

---

## Phase 2 — `internal/config`

**Files:** `internal/config/model.go`, `internal/config/loader.go`,
`internal/config/loader_test.go`.

```go
type Settings struct { TimingWindowMs int `yaml:"timing_window_ms"` }
type Shortcut struct {
    Trigger, Description, Terminator, Expansion string
}
type Config struct { Settings Settings; Shortcuts []Shortcut }

func Load(path string) (*Config, error)
func (c *Config) Validate() error      // also defaults TimingWindowMs to 500
func ValidTerminator(s string) bool
```

Validates: non-empty trigger (1–32 printable ASCII, no whitespace), terminator
in {Tab,Space,Enter}, non-empty expansion. Defaults `TimingWindowMs` to 500.

**Verify:** `go test ./internal/config/` — valid parse; missing terminator
errors; missing expansion errors; default window = 500; bad trigger errors.

---

## Phase 3 — `internal/crypto`

**Files:** `internal/crypto/aes.go`, `internal/crypto/credman.go` (`windows`),
`internal/crypto/aes_test.go`.

```go
func DeriveKey(password string, salt []byte) []byte        // PBKDF2-SHA256, 100k, 32B
func Encrypt(plaintext string, key []byte) (string, error) // "ENC:<b64(nonce|ct)>"
func Decrypt(ciphertext string, key []byte) (string, error)
const EncPrefix = "ENC:"

// credman.go (advapi32 CredWriteW/CredReadW/CredDeleteW)
func SavePassword(targetName, username, password string) error
func LoadPassword(targetName, username string) (string, error)
func DeletePassword(targetName string) error
const MasterPasswordTarget = "Expander_MasterPassword"
```

**Verify:** `go test ./internal/crypto/` — round-trip; wrong key fails; missing
`ENC:` prefix fails; tampered ciphertext fails. (credman = manual smoke test.)

---

## Phase 4 — `internal/hook`

**Files:** `internal/hook/matcher.go`, `internal/hook/keyboard.go` (`windows`),
`internal/hook/matcher_test.go`.

```go
// matcher.go (pure Go, no syscalls — fully unit tested)
type Matcher struct { /* ... */ }
func NewMatcher(shortcuts []config.Shortcut, windowMs int) *Matcher
func (m *Matcher) OnKey(vkCode uint32, char rune) (expansion string, triggerLen int, suppress bool)
func (m *Matcher) now() time.Time   // injectable clock for tests

// keyboard.go (windows)
func Install(handler func(vkCode uint32, char rune) bool) error
func Uninstall() error
func RunMessagePump()
```

Matcher: rune buffer capped at `maxTriggerLen+1`, `firstCharAt`. Terminator VK
→ scan matching shortcuts within window. Backspace/Escape/non-printable clear.
Ignores injected events handled in `keyboard.go` (`LLKHF_INJECTED`).

**Verify:** `go test ./internal/hook/` — match within window fires; wrong
terminator no-fire; too-slow (mock clock) no-fire; backspace clears; buffer cap.

---

## Phase 5 — `internal/expander`

**Files:** `internal/expander/engine.go`,
`internal/expander/sender_windows.go` (`windows`),
`internal/expander/engine_test.go`.

```go
type EventKind int            // EventBackspace | EventUnicode
type Event struct { Kind EventKind; Char rune }
type Sender interface { Send(events []Event) error }

type Engine struct { /* matcher, sender, paused atomic.Bool, mu */ }
func New(shortcuts []config.Shortcut, windowMs int) *Engine   // real Windows sender
func (e *Engine) HandleKey(vkCode uint32, char rune) bool
func (e *Engine) SetPaused(paused bool)
func (e *Engine) ReloadShortcuts(shortcuts []config.Shortcut, windowMs int)
func (e *Engine) inject(triggerLen int, expansion string)     // backspaces, 15ms, unicode
```

`HandleKey`: paused → false. Else `matcher.OnKey`; on expansion spawn
`go e.inject(...)` and return suppress. `sender_windows.go` translates Events →
`INPUT` and calls `SendInput` (backspaces as one batch, then one call per rune).

**Verify:** `go test ./internal/expander/` — white-box mock `Sender`: keys
`g,g,Tab` (in-window) → Tab suppressed; `inject(2,"hello")` records
`[Back,Back,Uni h,e,l,l,o]`; paused → no suppress.

---

## Phase 6 — `internal/tray`

**Files:** `internal/tray/tray.go` (`windows` — depends on assets + systray).

```go
type TrayState int               // Active | Paused | Error
type Config struct {
    OnPauseToggle  func(paused bool)
    OnReloadConfig func() error
    OnOpenConfig   func()
    OnExit         func()
    ConfigPath     string
}
func Run(cfg *Config)            // blocks; systray.Run on main goroutine
func SetState(state TrayState)   // thread-safe via systray's own goroutine guard
```

Menu: Pause/Resume toggle, Reload Config (Error state on failure), Open Config
File, Exit. `SetState` swaps embedded icon + tooltip.

**Verify:** `go build ./...`; manual smoke test (tray appears, items clickable).

---

## Phase 7 — `cmd/setup.go`

**Files:** `cmd/setup.go` (`windows`), `cmd/paths.go` (`windows`),
`internal/winutil/` helpers as needed (`registry.go`, `shellexec.go`).

`RunSetup()` wizard: masked password (`golang.org/x/term`); 16-byte salt →
`%APPDATA%\Expander\config.salt`; derive key; `credman.SavePassword`; loop to
collect trigger/description/terminator/plaintext → `crypto.Encrypt`; write
`config.yml`; write `HKCU\...\Run\Expander` = exe path; idempotent re-run
(re-reads, re-salts, re-encrypts plaintext entries). Paths via
`os.Getenv("APPDATA")`.

**Verify:** `go build`; manual: config.yml (ENC: values), config.salt (16B),
Credential Manager entry, registry value present.

---

## Phase 8 — `cmd/agent.go` + `main.go` + final build

**Files:** `cmd/agent.go` (`windows`), `main.go`.

`main.go`: `os.Args[1]=="setup"` → `cmd.RunSetup()` else `cmd.RunAgent()`.
`RunAgent()`: resolve paths; load salt; `credman.LoadPassword`; derive key;
`config.Load`; decrypt all `ENC:` expansions; build `expander.Engine`; start
hook goroutine (`runtime.LockOSThread`, `hook.Install`, `hook.RunMessagePump`);
`tray.Run` on main goroutine with callbacks (pause/reload/open/exit). Fatal
startup errors → tray Error state + append `%APPDATA%\Expander\error.log`.

**Verify (final):**
- `go build -ldflags="-H windowsgui" -o expander.exe .` → exits 0, produces exe
- `go test ./...` → all pass
- `go vet ./...` → no warnings

---

## Final build command

```bash
GOOS=windows GOARCH=amd64 go build -ldflags="-H windowsgui" -o expander.exe .
```

(On this Windows host `GOOS/GOARCH` are already native, so the bare
`go build -ldflags="-H windowsgui" -o expander.exe .` is equivalent.)

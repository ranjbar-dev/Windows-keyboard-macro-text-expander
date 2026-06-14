//go:build windows

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"expander/internal/config"
	"expander/internal/crypto"
	"expander/internal/expander"
	"expander/internal/hook"
	"expander/internal/tray"
	"expander/internal/winutil"
)

// agent holds the runtime state needed to serve the tray and reload config.
type agent struct {
	key        []byte
	configPath string
	engine     *expander.Engine
}

// RunAgent loads config, installs the keyboard hook, and runs the tray. On a
// fatal startup error it logs to error.log and shows the tray in the Error
// state instead of exiting silently.
func RunAgent() {
	a, err := newAgent()
	if err != nil {
		logFatal(err)
		runErrorTray()
		return
	}

	// The hooks must be installed and pumped on one OS-locked thread.
	go func() {
		runtime.LockOSThread()
		if err := hook.Install(a.engine.HandleKey); err != nil {
			logFatal(fmt.Errorf("install keyboard hook: %w", err))
			tray.SetState(tray.Error)
			return
		}
		// Reset the trigger buffer on mouse clicks (caret moves / selection
		// replacement the keyboard hook can't observe). Non-fatal if it fails.
		if err := hook.InstallMouse(a.engine.Reset); err != nil {
			logFatal(fmt.Errorf("install mouse hook (click-reset disabled): %w", err))
		}
		hook.RunMessagePump()
	}()

	tray.Run(&tray.Config{
		ConfigPath:     a.configPath,
		OnSetup:        a.openSetup,
		OnPauseToggle:  a.engine.SetPaused,
		OnReloadConfig: a.reload,
		OnOpenConfig:   a.openConfig,
		OnOpenErrorLog: openErrorLogFile,
		OnExit: func() {
			_ = hook.Uninstall()
			_ = hook.UninstallMouse()
		},
	})
}

func newAgent() (*agent, error) {
	cfgPath, err := configPath()
	if err != nil {
		return nil, err
	}
	sltPath, err := saltPath()
	if err != nil {
		return nil, err
	}

	salt, err := os.ReadFile(sltPath)
	if err != nil {
		return nil, fmt.Errorf("read salt (run `expander.exe setup` first): %w", err)
	}
	if len(salt) != crypto.SaltLen {
		return nil, fmt.Errorf("salt file is %d bytes, expected %d", len(salt), crypto.SaltLen)
	}

	pw, err := crypto.LoadPassword(crypto.MasterPasswordTarget, appDirName)
	if err != nil {
		return nil, fmt.Errorf("load master password from Credential Manager: %w", err)
	}
	key := crypto.DeriveKey(pw, salt)

	shortcuts, windowMs, err := loadDecrypted(cfgPath, key)
	if err != nil {
		return nil, err
	}
	return &agent{
		key:        key,
		configPath: cfgPath,
		engine:     expander.New(shortcuts, windowMs),
	}, nil
}

// loadDecrypted reads config.yml and decrypts every ENC: expansion value,
// returning shortcuts whose Expansion fields hold plaintext.
func loadDecrypted(path string, key []byte) ([]config.Shortcut, int, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, 0, err
	}
	out := make([]config.Shortcut, len(cfg.Shortcuts))
	for i, s := range cfg.Shortcuts {
		if strings.HasPrefix(s.Expansion, crypto.EncPrefix) {
			dec, err := crypto.Decrypt(s.Expansion, key)
			if err != nil {
				return nil, 0, fmt.Errorf("decrypt shortcut %q: %w", s.Trigger, err)
			}
			s.Expansion = dec
		}
		out[i] = s
	}
	return out, cfg.Settings.TimingWindowMs, nil
}

func (a *agent) reload() error {
	shortcuts, windowMs, err := loadDecrypted(a.configPath, a.key)
	if err != nil {
		logFatal(fmt.Errorf("reload config: %w", err))
		return err
	}
	a.engine.ReloadShortcuts(shortcuts, windowMs)
	return nil
}

func (a *agent) openConfig() {
	if err := winutil.OpenFile(a.configPath); err != nil {
		logFatal(fmt.Errorf("open config: %w", err))
	}
}

// openSetup launches the setup GUI in a separate process and, when it exits
// successfully (config saved), reloads the config so the new shortcuts take
// effect without restarting the agent.
func (a *agent) openSetup() {
	c, err := spawnSetupGUI()
	if err != nil {
		logFatal(fmt.Errorf("launch setup GUI: %w", err))
		return
	}
	go func() {
		if err := c.Wait(); err != nil {
			return // non-zero exit: cancelled or GUI error, nothing to reload
		}
		if err := a.reload(); err != nil {
			tray.SetState(tray.Error)
			return
		}
		if tray.IsPaused() {
			tray.SetState(tray.Paused)
		} else {
			tray.SetState(tray.Active)
		}
	}()
}

// runErrorTray shows a red tray icon after a startup failure, letting the user
// run setup (e.g. on first launch) or open the error log. On a successful setup
// it starts a fresh agent process and exits.
func runErrorTray() {
	tray.Run(&tray.Config{
		InitialState:   tray.Error,
		OnSetup:        errorTraySetup,
		OnOpenErrorLog: openErrorLogFile,
		OnExit:         func() {},
	})
}

// errorTraySetup runs the setup GUI from the error tray. The agent never
// started here, so on success it relaunches a fresh agent and quits this one.
func errorTraySetup() {
	c, err := spawnSetupGUI()
	if err != nil {
		logFatal(fmt.Errorf("launch setup GUI: %w", err))
		return
	}
	go func() {
		if err := c.Wait(); err != nil {
			return // cancelled or error: stay in the error tray
		}
		if exe, err := exePath(); err == nil {
			_ = exec.Command(exe).Start()
		}
		tray.Quit()
	}()
}

// spawnSetupGUI starts `expander.exe setup-gui` and returns the running command.
func spawnSetupGUI() (*exec.Cmd, error) {
	exe, err := exePath()
	if err != nil {
		return nil, err
	}
	c := exec.Command(exe, "setup-gui")
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("start setup GUI: %w", err)
	}
	return c, nil
}

// openErrorLogFile ensures error.log exists, then opens it in the default editor.
func openErrorLogFile() {
	p, err := errorLogPath()
	if err != nil {
		return
	}
	if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600); err == nil {
		_ = f.Close()
	}
	if err := winutil.OpenFile(p); err != nil {
		logFatal(fmt.Errorf("open error log: %w", err))
	}
}

// logFatal appends a timestamped fatal error to error.log (best effort).
func logFatal(err error) {
	p, e := errorLogPath()
	if e != nil {
		return
	}
	f, e := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if e != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s FATAL: %v\n", time.Now().Format(time.RFC3339), err)
}

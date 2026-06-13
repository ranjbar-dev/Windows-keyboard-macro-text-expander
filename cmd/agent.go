//go:build windows

package cmd

import (
	"fmt"
	"os"
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
		OnPauseToggle:  a.engine.SetPaused,
		OnReloadConfig: a.reload,
		OnOpenConfig:   a.openConfig,
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

// runErrorTray shows a red tray icon after a startup failure, letting the user
// open the error log and exit.
func runErrorTray() {
	cfgPath, _ := configPath()
	logPath, _ := errorLogPath()
	tray.Run(&tray.Config{
		InitialState: tray.Error,
		ConfigPath:   cfgPath,
		OnOpenConfig: func() {
			if logPath != "" {
				_ = winutil.OpenFile(logPath)
			}
		},
		OnExit: func() {},
	})
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

//go:build windows

// Package tray renders the system-tray icon and menu and forwards menu actions
// to the agent via callbacks. systray.Run must own the main goroutine.
package tray

import (
	"sync/atomic"

	"github.com/getlantern/systray"

	"expander/assets"
)

// TrayState selects the icon + tooltip shown in the notification area.
type TrayState int

const (
	Active TrayState = iota
	Paused
	Error
)

// Config holds the callbacks the agent supplies for menu actions. Each menu
// item is rendered only when its callback is non-nil, so the error tray (which
// supplies only OnSetup/OnOpenErrorLog/OnExit) shows a reduced menu.
type Config struct {
	OnSetup        func()
	OnPauseToggle  func(paused bool)
	OnReloadConfig func() error
	OnOpenConfig   func()
	OnOpenErrorLog func()
	OnExit         func()
	ConfigPath     string
	// InitialState is the icon state shown when the tray becomes ready
	// (Error when the agent failed to start). Defaults to Active.
	InitialState TrayState
}

var (
	cfg       *Config
	ready     atomic.Bool
	paused    atomic.Bool
	pauseItem *systray.MenuItem
)

// Run starts the tray and blocks until Exit is chosen. Must be called on the
// main goroutine.
func Run(c *Config) {
	cfg = c
	systray.Run(onReady, onExit)
}

// IsPaused reports whether expansion is currently paused. Used by the agent to
// restore the correct icon after a config reload.
func IsPaused() bool { return paused.Load() }

// Quit stops the tray; Run returns shortly after and the process can exit.
func Quit() { systray.Quit() }

// SetState updates the icon and tooltip. Safe to call from any goroutine; it
// is a no-op until the tray is ready.
func SetState(state TrayState) {
	if !ready.Load() {
		return
	}
	switch state {
	case Paused:
		systray.SetIcon(assets.Paused)
		systray.SetTooltip("Expander — Paused")
	case Error:
		systray.SetIcon(assets.Error)
		systray.SetTooltip("Expander — Error")
	default:
		systray.SetIcon(assets.Active)
		systray.SetTooltip("Expander — Active")
	}
}

func onReady() {
	ready.Store(true)
	if cfg.InitialState == Error {
		paused.Store(false)
	}
	SetState(cfg.InitialState)

	// Each item is added only when its callback is set; the unset channels stay
	// nil and never fire in the select below. A nil channel blocks forever, so
	// the error tray's reduced menu works without extra branching.
	var setupCh, pauseCh, reloadCh, openCh, errLogCh <-chan struct{}

	if cfg.OnSetup != nil {
		setupCh = systray.AddMenuItem("⚙ Setup…", "Open the setup window to manage shortcuts").ClickedCh
	}
	if cfg.OnPauseToggle != nil {
		pauseItem = systray.AddMenuItem("⏸ Pause", "Pause expansion")
		pauseCh = pauseItem.ClickedCh
	}
	if cfg.OnReloadConfig != nil {
		reloadCh = systray.AddMenuItem("🔄 Reload Config", "Re-read and re-decrypt config.yml").ClickedCh
	}
	if cfg.OnOpenConfig != nil {
		openCh = systray.AddMenuItem("📝 Open Config File", "Open config.yml in the default editor").ClickedCh
	}
	if cfg.OnOpenErrorLog != nil {
		errLogCh = systray.AddMenuItem("📄 Open Error Log", "Open error.log in the default editor").ClickedCh
	}
	systray.AddSeparator()
	exitCh := systray.AddMenuItem("❌ Exit", "Stop Expander and exit").ClickedCh

	go func() {
		for {
			select {
			case <-setupCh:
				if cfg.OnSetup != nil {
					cfg.OnSetup()
				}
			case <-pauseCh:
				togglePause()
			case <-reloadCh:
				handleReload()
			case <-openCh:
				if cfg.OnOpenConfig != nil {
					cfg.OnOpenConfig()
				}
			case <-errLogCh:
				if cfg.OnOpenErrorLog != nil {
					cfg.OnOpenErrorLog()
				}
			case <-exitCh:
				if cfg.OnExit != nil {
					cfg.OnExit()
				}
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	// Menu actions already ran their callbacks; nothing extra to tear down.
}

func togglePause() {
	p := !paused.Load()
	paused.Store(p)
	if cfg.OnPauseToggle != nil {
		cfg.OnPauseToggle(p)
	}
	if p {
		pauseItem.SetTitle("▶ Resume")
		SetState(Paused)
	} else {
		pauseItem.SetTitle("⏸ Pause")
		SetState(Active)
	}
}

func handleReload() {
	if cfg.OnReloadConfig == nil {
		return
	}
	if err := cfg.OnReloadConfig(); err != nil {
		SetState(Error)
		return
	}
	if paused.Load() {
		SetState(Paused)
	} else {
		SetState(Active)
	}
}

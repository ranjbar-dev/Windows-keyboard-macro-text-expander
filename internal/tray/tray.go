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

// Config holds the callbacks the agent supplies for menu actions.
type Config struct {
	OnPauseToggle  func(paused bool)
	OnReloadConfig func() error
	OnOpenConfig   func()
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

	pauseItem = systray.AddMenuItem("⏸ Pause", "Pause expansion")
	reloadItem := systray.AddMenuItem("🔄 Reload Config", "Re-read and re-decrypt config.yml")
	openItem := systray.AddMenuItem("📝 Open Config File", "Open config.yml in the default editor")
	systray.AddSeparator()
	exitItem := systray.AddMenuItem("❌ Exit", "Stop Expander and exit")

	go func() {
		for {
			select {
			case <-pauseItem.ClickedCh:
				togglePause()
			case <-reloadItem.ClickedCh:
				handleReload()
			case <-openItem.ClickedCh:
				if cfg.OnOpenConfig != nil {
					cfg.OnOpenConfig()
				}
			case <-exitItem.ClickedCh:
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

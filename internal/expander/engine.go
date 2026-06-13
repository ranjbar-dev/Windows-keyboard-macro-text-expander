// Package expander wires the trigger matcher to keystroke injection: it erases
// the typed trigger and types the decrypted expansion via SendInput.
package expander

import (
	"sync"
	"sync/atomic"
	"time"

	"expander/internal/config"
	"expander/internal/hook"
)

// backspaceSettle is the pause after erasing the trigger, giving the target
// app time to process the backspaces before the expansion is typed.
const backspaceSettle = 15 * time.Millisecond

// EventKind distinguishes the two kinds of injected keystrokes.
type EventKind int

const (
	EventBackspace EventKind = iota
	EventUnicode
)

// Event is a single logical keystroke to inject. Char is only set for
// EventUnicode.
type Event struct {
	Kind EventKind
	Char rune
}

// Sender injects a batch of keystroke events. The production implementation
// uses Win32 SendInput; tests substitute a recorder.
type Sender interface {
	Send(events []Event) error
}

// Engine is the hook callback target. It is safe for the hook goroutine to
// call HandleKey concurrently with tray-driven SetPaused / ReloadShortcuts.
type Engine struct {
	mu      sync.RWMutex
	matcher *hook.Matcher
	sender  Sender
	paused  atomic.Bool
}

// New builds an Engine with the (already decrypted) shortcuts and timing
// window, using the real Windows keystroke sender.
func New(shortcuts []config.Shortcut, windowMs int) *Engine {
	return &Engine{
		matcher: hook.NewMatcher(shortcuts, windowMs),
		sender:  defaultSender(),
	}
}

// HandleKey is the hook callback. It returns true to suppress (consume) the key.
func (e *Engine) HandleKey(vkCode uint32, char rune) bool {
	if e.paused.Load() {
		return false
	}
	e.mu.RLock()
	expansion, triggerLen, suppress := e.matcher.OnKey(vkCode, char)
	e.mu.RUnlock()

	if suppress && expansion != "" {
		// Inject asynchronously: the hook proc must return promptly, and our
		// injected keystrokes must not be processed within this callback.
		go e.inject(triggerLen, expansion)
	}
	return suppress
}

// SetPaused enables or disables expansion without uninstalling the hook.
func (e *Engine) SetPaused(paused bool) { e.paused.Store(paused) }

// Reset clears the in-progress trigger buffer. The agent calls this on mouse
// clicks, which may move the caret or replace a selection the keyboard hook
// cannot see. Safe to call from the hook thread alongside HandleKey.
func (e *Engine) Reset() {
	e.mu.RLock()
	defer e.mu.RUnlock()
	e.matcher.Reset()
}

// Paused reports whether expansion is currently suspended.
func (e *Engine) Paused() bool { return e.paused.Load() }

// ReloadShortcuts swaps the active shortcut set atomically. The hook stays
// installed; the next keypress uses the new matcher.
func (e *Engine) ReloadShortcuts(shortcuts []config.Shortcut, windowMs int) {
	e.mu.Lock()
	e.matcher = hook.NewMatcher(shortcuts, windowMs)
	e.mu.Unlock()
}

// inject erases triggerLen characters then types the expansion. Send errors
// are non-actionable from the hook context and are intentionally dropped.
func (e *Engine) inject(triggerLen int, expansion string) {
	if triggerLen > 0 {
		events := make([]Event, triggerLen)
		for i := range events {
			events[i] = Event{Kind: EventBackspace}
		}
		_ = e.sender.Send(events)
		time.Sleep(backspaceSettle)
	}
	for _, r := range expansion {
		_ = e.sender.Send([]Event{{Kind: EventUnicode, Char: r}})
	}
}

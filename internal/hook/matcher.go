// Package hook installs the low-level keyboard hook and matches typed trigger
// sequences against configured shortcuts.
package hook

import (
	"time"

	"expander/internal/config"
)

// Virtual-key codes used by the matcher and injector.
const (
	VKBack   uint32 = 0x08
	VKTab    uint32 = 0x09
	VKReturn uint32 = 0x0D
	VKEscape uint32 = 0x1B
	VKSpace  uint32 = 0x20
)

// terminatorVK maps a terminator name to its virtual-key code.
var terminatorVK = map[string]uint32{
	"Tab":   VKTab,
	"Space": VKSpace,
	"Enter": VKReturn,
}

// Matcher accumulates printable characters and decides when a terminator
// keypress should fire an expansion. It is not safe for concurrent use; the
// keyboard hook calls it from a single goroutine.
type Matcher struct {
	shortcuts   []config.Shortcut
	windowMs    int
	maxTrigger  int
	buf         []rune
	firstCharAt time.Time
	nowFn       func() time.Time // injectable clock for tests
}

// NewMatcher builds a matcher for the given shortcuts and timing window.
func NewMatcher(shortcuts []config.Shortcut, windowMs int) *Matcher {
	maxLen := 0
	for _, s := range shortcuts {
		if n := len([]rune(s.Trigger)); n > maxLen {
			maxLen = n
		}
	}
	return &Matcher{
		shortcuts:  shortcuts,
		windowMs:   windowMs,
		maxTrigger: maxLen,
		nowFn:      time.Now,
	}
}

// OnKey processes one key-down event. It returns the expansion text and the
// number of trigger characters to erase when an expansion fires, along with
// whether the key should be suppressed (consumed) by the caller.
func (m *Matcher) OnKey(vkCode uint32, char rune) (expansion string, triggerLen int, suppress bool) {
	switch {
	case vkCode == VKTab || vkCode == VKSpace || vkCode == VKReturn:
		return m.onTerminator(vkCode)
	case isModifier(vkCode):
		// Shift/Ctrl/Alt/Win/lock keys do not themselves change the typed
		// token; ignore them so e.g. Shift-typed capitals keep the buffer.
		return "", 0, false
	case isPrintable(char):
		m.appendRune(char)
		return "", 0, false
	default:
		// Backspace, Escape, arrows, Home/End/Delete and Ctrl/Alt chords (which
		// resolve to non-printable characters, e.g. Ctrl+A) move the caret or
		// replace a selection — the buffered token is no longer valid.
		m.clear()
		return "", 0, false
	}
}

// Reset clears the in-progress trigger buffer. The agent calls this when an
// out-of-band event such as a mouse click moves the caret or replaces a
// selection that the keyboard hook cannot observe.
func (m *Matcher) Reset() { m.clear() }

func (m *Matcher) onTerminator(vk uint32) (string, int, bool) {
	buf := string(m.buf)
	withinWindow := !m.firstCharAt.IsZero() &&
		m.nowFn().Sub(m.firstCharAt) <= time.Duration(m.windowMs)*time.Millisecond
	if withinWindow {
		for _, s := range m.shortcuts {
			if terminatorVK[s.Terminator] == vk && s.Trigger == buf {
				m.clear()
				return s.Expansion, len([]rune(s.Trigger)), true
			}
		}
	}
	m.clear()
	return "", 0, false
}

func (m *Matcher) appendRune(r rune) {
	if len(m.buf) == 0 {
		m.firstCharAt = m.nowFn()
	}
	m.buf = append(m.buf, r)
	// Cap at maxTrigger+1: long tokens stay one rune longer than the longest
	// trigger so they can never alias to a shorter trigger via the window.
	if limit := m.maxTrigger + 1; len(m.buf) > limit {
		m.buf = m.buf[len(m.buf)-limit:]
	}
}

func (m *Matcher) clear() {
	m.buf = m.buf[:0]
	m.firstCharAt = time.Time{}
}

func isPrintable(r rune) bool {
	return r >= 0x20 && r != 0x7F
}

// isModifier reports whether vk is a modifier or lock key that should neither
// extend nor clear the trigger buffer.
func isModifier(vk uint32) bool {
	switch vk {
	case 0x10, 0xA0, 0xA1, // Shift / LShift / RShift
		0x11, 0xA2, 0xA3, // Ctrl / LCtrl / RCtrl
		0x12, 0xA4, 0xA5, // Alt(Menu) / LMenu / RMenu
		0x5B, 0x5C, // LWin / RWin
		0x14, 0x90, 0x91: // CapsLock / NumLock / ScrollLock
		return true
	}
	return false
}

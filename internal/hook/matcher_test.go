package hook

import (
	"testing"
	"time"

	"expander/internal/config"
)

func shortcuts() []config.Shortcut {
	return []config.Shortcut{
		{Trigger: "gg", Terminator: "Tab", Expansion: "hello"},
		{Trigger: "em1", Terminator: "Space", Expansion: "me@example.com"},
		{Trigger: "addr", Terminator: "Enter", Expansion: "123 Main St"},
	}
}

// clockMatcher returns a matcher whose clock is driven by *now.
func clockMatcher(t *testing.T, now *time.Time) *Matcher {
	t.Helper()
	m := NewMatcher(shortcuts(), 500)
	m.nowFn = func() time.Time { return *now }
	return m
}

// typeRunes feeds printable characters at the current clock.
func typeRunes(m *Matcher, s string) {
	for _, r := range s {
		m.OnKey(uint32(r), r)
	}
}

func TestMatchWithinWindowFires(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	typeRunes(m, "gg")
	now = now.Add(200 * time.Millisecond)
	exp, triggerLen, suppress := m.OnKey(VKTab, '\t')

	if !suppress {
		t.Fatal("expected terminator to be suppressed")
	}
	if exp != "hello" {
		t.Errorf("expansion = %q, want %q", exp, "hello")
	}
	if triggerLen != 2 {
		t.Errorf("triggerLen = %d, want 2", triggerLen)
	}
}

func TestWrongTerminatorDoesNotFire(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	typeRunes(m, "gg") // "gg" expects Tab, not Space
	exp, _, suppress := m.OnKey(VKSpace, ' ')
	if suppress || exp != "" {
		t.Errorf("expected no fire for wrong terminator, got suppress=%v exp=%q", suppress, exp)
	}
}

func TestTooSlowDoesNotFire(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	typeRunes(m, "gg")
	now = now.Add(600 * time.Millisecond) // beyond 500ms window
	exp, _, suppress := m.OnKey(VKTab, '\t')
	if suppress || exp != "" {
		t.Errorf("expected no fire when too slow, got suppress=%v exp=%q", suppress, exp)
	}
}

func TestBackspaceClearsBuffer(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	typeRunes(m, "g")
	m.OnKey(VKBack, 0) // clears
	typeRunes(m, "g")  // buffer now just "g", not "gg"
	exp, _, suppress := m.OnKey(VKTab, '\t')
	if suppress || exp != "" {
		t.Errorf("expected no fire after backspace cleared buffer, got suppress=%v exp=%q", suppress, exp)
	}
}

func TestPrefixCharDoesNotAlias(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	// Typing "xgg" must NOT match trigger "gg" — exact token match only.
	typeRunes(m, "xgg")
	exp, _, suppress := m.OnKey(VKTab, '\t')
	if suppress || exp != "" {
		t.Errorf("expected no fire for non-exact token, got suppress=%v exp=%q", suppress, exp)
	}
}

func TestBufferCapPreventsUnboundedGrowth(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	// Longest trigger is "addr" (4) → cap is 5.
	typeRunes(m, "aaaaaaaaaaaaaaaaaaaa")
	if got := len(m.buf); got > m.maxTrigger+1 {
		t.Errorf("buffer length = %d, want <= %d", got, m.maxTrigger+1)
	}
}

func TestCtrlAClearsBuffer(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	typeRunes(m, "gg")
	// Ctrl down (modifier, ignored) then 'A' resolving to Ctrl+A control code.
	m.OnKey(0x11, 0)    // VK_CONTROL
	m.OnKey(0x41, 0x01) // 'A' under Ctrl -> SOH (non-printable)
	// Selection replaced; user re-types the trigger.
	typeRunes(m, "gg")
	exp, _, suppress := m.OnKey(VKTab, '\t')
	if !suppress || exp != "hello" {
		t.Errorf("expected fire after Ctrl+A reset + retype, got suppress=%v exp=%q", suppress, exp)
	}
}

func TestModifierDoesNotClearBuffer(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	m.OnKey(0x10, 0) // Shift down before a capital — must NOT clear
	typeRunes(m, "gg")
	m.OnKey(0xA1, 0) // RShift again mid-stream
	_, _, suppress := m.OnKey(VKTab, '\t')
	if !suppress {
		t.Error("modifier keys must not clear the buffer")
	}
}

func TestNavigationKeyClearsBuffer(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	typeRunes(m, "gg")
	m.OnKey(0x25, 0) // VK_LEFT arrow (non-printable, non-modifier) -> clear
	_, _, suppress := m.OnKey(VKTab, '\t')
	if suppress {
		t.Error("arrow key should have cleared the buffer")
	}
}

func TestResetClearsBuffer(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	typeRunes(m, "gg")
	m.Reset() // e.g. a mouse click
	_, _, suppress := m.OnKey(VKTab, '\t')
	if suppress {
		t.Error("Reset should have cleared the buffer")
	}
}

func TestEnterTerminatorMatches(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	typeRunes(m, "addr")
	exp, triggerLen, suppress := m.OnKey(VKReturn, '\r')
	if !suppress || exp != "123 Main St" || triggerLen != 4 {
		t.Errorf("addr+Enter: suppress=%v exp=%q triggerLen=%d", suppress, exp, triggerLen)
	}
}

func TestSuccessfulExpansionClearsBuffer(t *testing.T) {
	now := time.Now()
	m := clockMatcher(t, &now)

	typeRunes(m, "gg")
	m.OnKey(VKTab, '\t') // fires, clears
	// Pressing Tab again with empty buffer must not re-fire.
	_, _, suppress := m.OnKey(VKTab, '\t')
	if suppress {
		t.Error("expected no re-fire after buffer cleared by expansion")
	}
}

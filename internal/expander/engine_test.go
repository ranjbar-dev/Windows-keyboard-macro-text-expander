package expander

import (
	"sync"
	"testing"
	"time"

	"expander/internal/config"
	"expander/internal/hook"
)

// mockSender records every injected event for assertion.
type mockSender struct {
	mu     sync.Mutex
	events []Event
}

func (m *mockSender) Send(events []Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, events...)
	return nil
}

// waitFor polls until at least n events are recorded or the timeout elapses.
func (m *mockSender) waitFor(n int, timeout time.Duration) []Event {
	deadline := time.Now().Add(timeout)
	for {
		m.mu.Lock()
		if len(m.events) >= n {
			out := append([]Event(nil), m.events...)
			m.mu.Unlock()
			return out
		}
		m.mu.Unlock()
		if time.Now().After(deadline) {
			return nil
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func testEngine(sender Sender) *Engine {
	return &Engine{
		matcher: hook.NewMatcher([]config.Shortcut{
			{Trigger: "gg", Terminator: "Tab", Expansion: "hello"},
		}, 500),
		sender: sender,
	}
}

func TestHandleKeyExpandsAndSuppresses(t *testing.T) {
	mock := &mockSender{}
	e := testEngine(mock)

	if e.HandleKey('g', 'g') {
		t.Error("printable key should not be suppressed")
	}
	if e.HandleKey('g', 'g') {
		t.Error("printable key should not be suppressed")
	}
	if !e.HandleKey(hook.VKTab, '\t') {
		t.Fatal("terminator completing a trigger must be suppressed")
	}

	// 2 backspaces + 5 unicode chars = 7 logical events.
	got := mock.waitFor(7, time.Second)
	want := []Event{
		{Kind: EventBackspace},
		{Kind: EventBackspace},
		{Kind: EventUnicode, Char: 'h'},
		{Kind: EventUnicode, Char: 'e'},
		{Kind: EventUnicode, Char: 'l'},
		{Kind: EventUnicode, Char: 'l'},
		{Kind: EventUnicode, Char: 'o'},
	}
	if len(got) != len(want) {
		t.Fatalf("recorded %d events, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestInjectSequence(t *testing.T) {
	mock := &mockSender{}
	e := testEngine(mock)

	e.inject(2, "hi") // synchronous: deterministic event ordering

	want := []Event{
		{Kind: EventBackspace},
		{Kind: EventBackspace},
		{Kind: EventUnicode, Char: 'h'},
		{Kind: EventUnicode, Char: 'i'},
	}
	if len(mock.events) != len(want) {
		t.Fatalf("got %d events, want %d", len(mock.events), len(want))
	}
	for i := range want {
		if mock.events[i] != want[i] {
			t.Errorf("event[%d] = %+v, want %+v", i, mock.events[i], want[i])
		}
	}
}

func TestPausedDoesNotExpand(t *testing.T) {
	mock := &mockSender{}
	e := testEngine(mock)
	e.SetPaused(true)

	e.HandleKey('g', 'g')
	e.HandleKey('g', 'g')
	if e.HandleKey(hook.VKTab, '\t') {
		t.Error("paused engine must not suppress the terminator")
	}
	if got := mock.waitFor(1, 50*time.Millisecond); got != nil {
		t.Errorf("paused engine injected events: %+v", got)
	}
}

func TestReloadShortcuts(t *testing.T) {
	mock := &mockSender{}
	e := testEngine(mock)
	e.ReloadShortcuts([]config.Shortcut{
		{Trigger: "x", Terminator: "Space", Expansion: "replaced"},
	}, 500)

	// Old trigger no longer fires.
	e.HandleKey('g', 'g')
	if e.HandleKey(hook.VKTab, '\t') {
		t.Error("old trigger should not fire after reload")
	}
	// New trigger fires.
	e.HandleKey('x', 'x')
	if !e.HandleKey(hook.VKSpace, ' ') {
		t.Error("new trigger should fire after reload")
	}
}

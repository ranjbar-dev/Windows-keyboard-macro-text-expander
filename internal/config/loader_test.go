package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return p
}

func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeTemp(t, `
settings:
  timing_window_ms: 400
shortcuts:
  - trigger: "gg"
    description: "pw"
    terminator: "Tab"
    expansion: "ENC:abc"
  - trigger: "em1"
    terminator: "Space"
    expansion: "me@example.com"
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Settings.TimingWindowMs != 400 {
		t.Errorf("timing window = %d, want 400", cfg.Settings.TimingWindowMs)
	}
	if len(cfg.Shortcuts) != 2 {
		t.Fatalf("got %d shortcuts, want 2", len(cfg.Shortcuts))
	}
	if cfg.Shortcuts[0].Trigger != "gg" || cfg.Shortcuts[0].Terminator != "Tab" {
		t.Errorf("shortcut[0] parsed wrong: %+v", cfg.Shortcuts[0])
	}
}

func TestTimingWindowDefaults(t *testing.T) {
	cfg, err := Load(writeTemp(t, `
shortcuts:
  - trigger: "gg"
    terminator: "Tab"
    expansion: "ENC:abc"
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Settings.TimingWindowMs != DefaultTimingWindowMs {
		t.Errorf("default timing window = %d, want %d", cfg.Settings.TimingWindowMs, DefaultTimingWindowMs)
	}
}

func TestMissingTerminatorErrors(t *testing.T) {
	_, err := Load(writeTemp(t, `
shortcuts:
  - trigger: "gg"
    expansion: "ENC:abc"
`))
	if err == nil {
		t.Fatal("expected error for missing terminator, got nil")
	}
}

func TestInvalidTerminatorErrors(t *testing.T) {
	_, err := Load(writeTemp(t, `
shortcuts:
  - trigger: "gg"
    terminator: "Ctrl"
    expansion: "ENC:abc"
`))
	if err == nil {
		t.Fatal("expected error for invalid terminator, got nil")
	}
}

func TestMissingExpansionErrors(t *testing.T) {
	_, err := Load(writeTemp(t, `
shortcuts:
  - trigger: "gg"
    terminator: "Tab"
`))
	if err == nil {
		t.Fatal("expected error for missing expansion, got nil")
	}
}

func TestInvalidTriggerErrors(t *testing.T) {
	cases := map[string]string{
		"whitespace": "g g",
		"empty":      "",
		"too long":   "thisistoolongggggggggggggggggggggg",
	}
	for name, trig := range cases {
		t.Run(name, func(t *testing.T) {
			if err := ValidateTrigger(trig); err == nil {
				t.Errorf("expected error for %s trigger %q", name, trig)
			}
		})
	}
}

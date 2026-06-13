package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// MaxTriggerLen is the maximum allowed length of a trigger string.
const MaxTriggerLen = 32

// Load reads and parses the YAML config at path, then validates it. The
// returned Config has TimingWindowMs defaulted to DefaultTimingWindowMs when
// the file omits it.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return &cfg, nil
}

// Validate checks every shortcut and applies defaults in place. It returns a
// descriptive error on the first violation found.
func (c *Config) Validate() error {
	if c.Settings.TimingWindowMs <= 0 {
		c.Settings.TimingWindowMs = DefaultTimingWindowMs
	}
	if len(c.Shortcuts) == 0 {
		return fmt.Errorf("no shortcuts defined")
	}
	for i, s := range c.Shortcuts {
		if err := ValidateTrigger(s.Trigger); err != nil {
			return fmt.Errorf("shortcut %d (%q): %w", i, s.Trigger, err)
		}
		if !ValidTerminator(s.Terminator) {
			return fmt.Errorf("shortcut %d (%q): terminator %q must be one of Tab, Space, Enter",
				i, s.Trigger, s.Terminator)
		}
		if s.Expansion == "" {
			return fmt.Errorf("shortcut %d (%q): expansion is empty", i, s.Trigger)
		}
	}
	return nil
}

// ValidateTrigger enforces 1-32 printable ASCII characters with no whitespace.
func ValidateTrigger(t string) error {
	if t == "" {
		return fmt.Errorf("trigger is empty")
	}
	if len(t) > MaxTriggerLen {
		return fmt.Errorf("trigger longer than %d characters", MaxTriggerLen)
	}
	for _, r := range t {
		if r < 0x21 || r > 0x7E {
			return fmt.Errorf("trigger must be printable ASCII with no whitespace")
		}
	}
	return nil
}

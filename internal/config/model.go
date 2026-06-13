package config

// Settings holds global tunables read from the `settings:` block.
type Settings struct {
	// TimingWindowMs is the maximum time (ms) between the first trigger
	// character and the terminator keypress for an expansion to fire.
	TimingWindowMs int `yaml:"timing_window_ms"`
}

// Shortcut is a single trigger -> expansion mapping.
type Shortcut struct {
	Trigger     string `yaml:"trigger"`
	Description string `yaml:"description"`
	Terminator  string `yaml:"terminator"` // "Tab" | "Space" | "Enter"
	Expansion   string `yaml:"expansion"`  // "ENC:<b64>" or plaintext
}

// Config is the full parsed config.yml.
type Config struct {
	Settings  Settings   `yaml:"settings"`
	Shortcuts []Shortcut `yaml:"shortcuts"`
}

// DefaultTimingWindowMs is applied when settings.timing_window_ms is absent/zero.
const DefaultTimingWindowMs = 500

// Supported terminator names, mapped to their virtual-key codes in the hook layer.
var validTerminators = map[string]struct{}{
	"Tab":   {},
	"Space": {},
	"Enter": {},
}

// ValidTerminator reports whether s is one of the supported terminator names.
func ValidTerminator(s string) bool {
	_, ok := validTerminators[s]
	return ok
}

//go:build windows

package cmd

import (
	"bufio"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
	"gopkg.in/yaml.v3"

	"expander/internal/config"
	"expander/internal/crypto"
	"expander/internal/winutil"
)

// stdinReader is the single buffered reader for all line input. It is created
// after setupConsole has (re)bound os.Stdin to the wizard's console.
var stdinReader *bufio.Reader

// RunSetup runs the first-run/idempotent setup wizard.
func RunSetup() {
	setupConsole()
	stdinReader = bufio.NewReader(os.Stdin)

	err := runSetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nSetup failed: %v\n", err)
	}
	if allocatedConsole {
		// Keep the freshly allocated window open so the user can read the result.
		waitForEnter()
	}
	if err != nil {
		os.Exit(1)
	}
}

func runSetup() error {
	cfgPath, err := configPath()
	if err != nil {
		return err
	}
	sltPath, err := saltPath()
	if err != nil {
		return err
	}

	fmt.Println("=== Expander setup ===")
	fmt.Println("Press Ctrl+C at any time to cancel without saving.")

	existing, timingWindow, err := loadExisting(cfgPath)
	if err != nil {
		return err
	}
	oldSalt, _ := os.ReadFile(sltPath) // absent on first run

	pw, err := readPasswordConfirmed()
	if err != nil {
		return err
	}

	var oldKey []byte
	if len(oldSalt) == crypto.SaltLen {
		oldKey = crypto.DeriveKey(pw, oldSalt)
	}
	newSalt := make([]byte, crypto.SaltLen)
	if _, err := rand.Read(newSalt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}
	newKey := crypto.DeriveKey(pw, newSalt)

	migrated, err := migrateShortcuts(existing, oldKey, newKey)
	if err != nil {
		return err
	}
	added, err := collectNewShortcuts(newKey)
	if err != nil {
		return err
	}
	all := append(migrated, added...)
	if len(all) == 0 {
		return errors.New("no shortcuts configured; nothing to write")
	}

	if err := os.WriteFile(sltPath, newSalt, 0o600); err != nil {
		return fmt.Errorf("write salt: %w", err)
	}
	if err := crypto.SavePassword(crypto.MasterPasswordTarget, appDirName, pw); err != nil {
		return fmt.Errorf("store master password: %w", err)
	}
	cfg := &config.Config{
		Settings:  config.Settings{TimingWindowMs: timingWindow},
		Shortcuts: all,
	}
	if err := writeConfigFile(cfgPath, cfg); err != nil {
		return err
	}
	exe, err := exePath()
	if err != nil {
		return err
	}
	if err := winutil.SetRunKey(appDirName, exe); err != nil {
		return fmt.Errorf("write auto-start registry key: %w", err)
	}

	printSummary(cfgPath, sltPath, exe, len(all))
	return nil
}

// loadExisting returns the existing shortcuts and timing window for an
// idempotent re-run, or empty values on first run.
func loadExisting(path string) ([]config.Shortcut, int, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, config.DefaultTimingWindowMs, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, 0, fmt.Errorf("existing config is invalid (fix or delete %q): %w", path, err)
	}
	return cfg.Shortcuts, cfg.Settings.TimingWindowMs, nil
}

// migrateShortcuts re-encrypts existing shortcuts under newKey. Encrypted
// values are first decrypted with oldKey; plaintext values pass straight
// through.
func migrateShortcuts(existing []config.Shortcut, oldKey, newKey []byte) ([]config.Shortcut, error) {
	out := make([]config.Shortcut, 0, len(existing))
	for _, s := range existing {
		plain := s.Expansion
		if strings.HasPrefix(s.Expansion, crypto.EncPrefix) {
			if oldKey == nil {
				return nil, fmt.Errorf("shortcut %q is encrypted but no salt exists to decrypt it; delete config.yml to start fresh", s.Trigger)
			}
			dec, err := crypto.Decrypt(s.Expansion, oldKey)
			if err != nil {
				return nil, fmt.Errorf("cannot decrypt shortcut %q (wrong master password?): %w", s.Trigger, err)
			}
			plain = dec
		}
		enc, err := crypto.Encrypt(plain, newKey)
		if err != nil {
			return nil, err
		}
		s.Expansion = enc
		out = append(out, s)
	}
	return out, nil
}

// collectNewShortcuts interactively gathers new shortcuts and encrypts them.
func collectNewShortcuts(key []byte) ([]config.Shortcut, error) {
	var out []config.Shortcut
	fmt.Println("\nAdd shortcuts. Press Enter on an empty trigger to finish.")
	for {
		trigger := readLine("\n  Trigger (e.g. gg, empty to finish): ")
		if trigger == "" {
			return out, nil
		}
		if err := config.ValidateTrigger(trigger); err != nil {
			fmt.Printf("    ! %v\n", err)
			continue
		}
		desc := readLine("  Description (optional): ")
		termName := readLine("  Terminator [Tab|Space|Enter]: ")
		if !config.ValidTerminator(termName) {
			fmt.Println("    ! terminator must be Tab, Space, or Enter")
			continue
		}
		value, err := readPassword("  Expansion value (hidden): ")
		if err != nil {
			return nil, err
		}
		if value == "" {
			fmt.Println("    ! expansion cannot be empty")
			continue
		}
		enc, err := crypto.Encrypt(value, key)
		if err != nil {
			return nil, err
		}
		out = append(out, config.Shortcut{Trigger: trigger, Description: desc, Terminator: termName, Expansion: enc})
		fmt.Printf("    + added %q (%s)\n", trigger, termName)
	}
}

func readPasswordConfirmed() (string, error) {
	for {
		pw, err := readPassword("\nMaster password: ")
		if err != nil {
			return "", err
		}
		if pw == "" {
			fmt.Println("  ! password cannot be empty")
			continue
		}
		confirm, err := readPassword("Confirm password: ")
		if err != nil {
			return "", err
		}
		if pw != confirm {
			fmt.Println("  ! passwords do not match, try again")
			continue
		}
		return pw, nil
	}
}

// readPassword reads a secret without echo on a real console. When input is
// redirected (a pipe/file, e.g. automated testing) it falls back to a plain
// line read since there is no terminal to suppress echo on.
func readPassword(prompt string) (string, error) {
	fmt.Print(prompt)
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		line, _ := stdinReader.ReadString('\n')
		return strings.TrimRight(line, "\r\n"), nil
	}
	b, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("read password: %w", err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

func readLine(prompt string) string {
	fmt.Print(prompt)
	line, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(line)
}

func waitForEnter() {
	fmt.Print("\nPress Enter to close this window...")
	stdinReader.ReadString('\n')
}

func writeConfigFile(path string, cfg *config.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	const header = "# Expander configuration — generated by `expander.exe setup`.\n" +
		"# ENC: values are AES-GCM encrypted. Re-run setup to add/re-encrypt values.\n\n"
	if err := os.WriteFile(path, append([]byte(header), data...), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func printSummary(cfgPath, sltPath, exe string, n int) {
	fmt.Println("\n=== Setup complete ===")
	fmt.Printf("  Config:      %s\n", cfgPath)
	fmt.Printf("  Salt:        %s\n", sltPath)
	fmt.Printf("  Credential:  %s (Windows Credential Manager)\n", crypto.MasterPasswordTarget)
	fmt.Printf("  Auto-start:  HKCU\\...\\Run\\%s = %s\n", appDirName, exe)
	fmt.Printf("  Shortcuts:   %d\n", n)
	fmt.Println("\nLaunch expander.exe (no arguments) to start the agent.")
}

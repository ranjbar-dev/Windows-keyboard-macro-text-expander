//go:build windows

// Package winutil holds small Windows-only helpers (registry auto-start,
// ShellExecute) shared by the cmd layer.
package winutil

import (
	"fmt"

	"golang.org/x/sys/windows/registry"
)

const runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`

// SetRunKey writes an HKCU\...\Run value so the app auto-starts on login.
func SetRunKey(name, exePath string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("open Run key: %w", err)
	}
	defer k.Close()
	if err := k.SetStringValue(name, exePath); err != nil {
		return fmt.Errorf("set Run value %q: %w", name, err)
	}
	return nil
}

// RunKeyValue reads back the configured auto-start path (used for diagnostics).
func RunKeyValue(name string) (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return "", fmt.Errorf("open Run key: %w", err)
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return "", err
	}
	return v, nil
}

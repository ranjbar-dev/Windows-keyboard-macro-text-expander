//go:build windows

// Package cmd holds the two entry points: the silent agent and the setup wizard.
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	appDirName     = "Expander"
	configFileName = "config.yml"
	saltFileName   = "config.salt"
	errorLogName   = "error.log"
)

// appDataDir returns %APPDATA%\Expander, creating it if necessary.
func appDataDir() (string, error) {
	base := os.Getenv("APPDATA")
	if base == "" {
		return "", fmt.Errorf("APPDATA environment variable is not set")
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %q: %w", dir, err)
	}
	return dir, nil
}

func configPath() (string, error) {
	dir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFileName), nil
}

func saltPath() (string, error) {
	dir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, saltFileName), nil
}

func errorLogPath() (string, error) {
	dir, err := appDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, errorLogName), nil
}

// exePath returns the absolute path of the running executable.
func exePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	return filepath.Clean(p), nil
}

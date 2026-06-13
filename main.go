//go:build windows

// Command expander is a Windows keyboard text-expander. With no arguments it
// runs the tray agent; `expander.exe setup` runs the first-run wizard.
package main

import (
	"os"

	"expander/cmd"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "setup" {
		cmd.RunSetup()
		return
	}
	cmd.RunAgent()
}

//go:build windows

// Command expander is a Windows keyboard text-expander. With no arguments it
// runs the tray agent; `expander.exe setup-gui` runs the graphical setup window
// (launched from the tray's Setup menu item).
package main

import (
	"os"

	"expander/cmd"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "setup-gui" {
		cmd.RunSetupGUI()
		return
	}
	cmd.RunAgent()
}

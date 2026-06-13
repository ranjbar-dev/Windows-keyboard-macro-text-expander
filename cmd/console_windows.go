//go:build windows

package cmd

import (
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procAllocConsole    = kernel32.NewProc("AllocConsole")
	procFreeConsole     = kernel32.NewProc("FreeConsole")
	procAttachConsole   = kernel32.NewProc("AttachConsole")
	procSetConsoleTitle = kernel32.NewProc("SetConsoleTitleW")
)

const attachParentProcess = ^uintptr(0) // (DWORD)-1 == ATTACH_PARENT_PROCESS

// allocatedConsole records whether setupConsole created a fresh console window
// (so RunSetup knows it must pause before that window disappears).
var allocatedConsole bool

// setupConsole gives the GUI-subsystem binary a usable console for the
// interactive setup wizard.
//
// If stdin is redirected (a pipe or file), the existing streams are used as is.
// Otherwise a brand-new console window is allocated. We deliberately do NOT
// attach to the launching shell's console: a GUI process does not block the
// shell, so sharing its console makes the wizard's output interleave with the
// shell prompt and makes the two processes fight over keyboard input — which is
// exactly the "garbled text / freeze on typing" failure.
func setupConsole() {
	if stdinIsRedirected() {
		return
	}
	procFreeConsole.Call() // detach from any inherited console (no-op if none)
	if r, _, _ := procAllocConsole.Call(); r == 0 {
		// Last-resort fallback if a fresh console cannot be created.
		procAttachConsole.Call(attachParentProcess)
	}
	rebindStdHandles()
	if title, err := windows.UTF16PtrFromString("Expander Setup"); err == nil {
		procSetConsoleTitle.Call(uintptr(unsafe.Pointer(title)))
	}
	allocatedConsole = true
}

// stdinIsRedirected reports whether stdin is a pipe or file (i.e. the wizard is
// being driven non-interactively and should use the existing streams).
func stdinIsRedirected() bool {
	h := windows.Handle(os.Stdin.Fd())
	if h == 0 || h == windows.InvalidHandle {
		return false
	}
	ft, err := windows.GetFileType(h)
	if err != nil {
		return false
	}
	return ft == windows.FILE_TYPE_DISK || ft == windows.FILE_TYPE_PIPE
}

// rebindStdHandles points the standard streams at the (new) console so fmt and
// term read and write it.
func rebindStdHandles() {
	if in, err := os.OpenFile("CONIN$", os.O_RDWR, 0); err == nil {
		os.Stdin = in
	}
	if out, err := os.OpenFile("CONOUT$", os.O_RDWR, 0); err == nil {
		os.Stdout = out
		os.Stderr = out
	}
}

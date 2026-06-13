//go:build windows

package winutil

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const swShowNormal = 1

var (
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	procShellExecute = shell32.NewProc("ShellExecuteW")
)

// OpenFile opens path with its associated default application (the shell "open"
// verb), e.g. config.yml in the default text editor.
func OpenFile(path string) error {
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	ret, _, _ := procShellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(p)),
		0,
		0,
		swShowNormal,
	)
	// ShellExecuteW returns a value > 32 on success.
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW(%q) failed (code %d)", path, ret)
	}
	return nil
}

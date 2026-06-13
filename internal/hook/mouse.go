//go:build windows

package hook

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	whMouseLL     = 14
	wmLButtonDown = 0x0201
	wmRButtonDown = 0x0204
	wmMButtonDown = 0x0207
)

var (
	mouseHookHandle uintptr
	mouseActivity   func()
	mouseProcCb     = syscall.NewCallback(mouseProc)
)

// InstallMouse registers a WH_MOUSE_LL hook that calls onClick for every mouse
// button-down. A click can move the caret or start a selection, so the agent
// uses it to reset the trigger buffer. Must be called on the same OS-locked,
// message-pumping goroutine as Install. The hook never suppresses input.
func InstallMouse(onClick func()) error {
	mouseActivity = onClick
	hmod, _, _ := procGetModuleHandle.Call(0)
	ret, _, callErr := procSetWindowsHookEx.Call(uintptr(whMouseLL), mouseProcCb, hmod, 0)
	if ret == 0 {
		return fmt.Errorf("SetWindowsHookExW(WH_MOUSE_LL) failed: %w", callErr)
	}
	mouseHookHandle = ret
	return nil
}

// UninstallMouse removes the mouse hook. It is a no-op if none is installed.
func UninstallMouse() error {
	if mouseHookHandle == 0 {
		return nil
	}
	ret, _, callErr := procUnhookWindowsHookEx.Call(mouseHookHandle)
	mouseHookHandle = 0
	if ret == 0 {
		return fmt.Errorf("UnhookWindowsHookEx(mouse) failed: %w", callErr)
	}
	return nil
}

func mouseProc(nCode uintptr, wParam uintptr, lParam unsafe.Pointer) uintptr {
	if nCode == hcAction && mouseActivity != nil {
		switch wParam {
		case wmLButtonDown, wmRButtonDown, wmMButtonDown:
			mouseActivity()
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, nCode, wParam, uintptr(lParam))
	return ret
}

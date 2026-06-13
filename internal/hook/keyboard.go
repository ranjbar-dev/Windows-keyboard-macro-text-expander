//go:build windows

package hook

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	whKeyboardLL  = 13
	wmKeyDown     = 0x0100
	wmSysKeyDown  = 0x0104
	llkhfInjected = 0x10
	hcAction      = 0
)

// kbdllhookstruct mirrors KBDLLHOOKSTRUCT.
type kbdllhookstruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// msg mirrors the Win32 MSG structure (including LPrivate for correct size).
type msg struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	PtX      int32
	PtY      int32
	LPrivate uint32
}

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessage     = user32.NewProc("DispatchMessageW")
	procToUnicodeEx         = user32.NewProc("ToUnicodeEx")
	procGetKeyboardState    = user32.NewProc("GetKeyboardState")
	procGetKeyboardLayout   = user32.NewProc("GetKeyboardLayout")

	kernel32            = windows.NewLazySystemDLL("kernel32.dll")
	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
)

var (
	hookHandle uintptr
	handler    func(vkCode uint32, char rune) bool
	hookProcCb = syscall.NewCallback(hookProc)
)

// Install registers a WH_KEYBOARD_LL hook. The handler is invoked for each
// non-injected key-down event; returning true suppresses (consumes) the key.
//
// Install and RunMessagePump must be called on the same goroutine, which must
// hold runtime.LockOSThread for the lifetime of the hook.
func Install(h func(vkCode uint32, char rune) bool) error {
	handler = h
	hmod, _, _ := procGetModuleHandle.Call(0)
	ret, _, callErr := procSetWindowsHookEx.Call(uintptr(whKeyboardLL), hookProcCb, hmod, 0)
	if ret == 0 {
		return fmt.Errorf("SetWindowsHookExW failed: %w", callErr)
	}
	hookHandle = ret
	return nil
}

// Uninstall removes the keyboard hook. It is a no-op if no hook is installed.
func Uninstall() error {
	if hookHandle == 0 {
		return nil
	}
	ret, _, callErr := procUnhookWindowsHookEx.Call(hookHandle)
	hookHandle = 0
	if ret == 0 {
		return fmt.Errorf("UnhookWindowsHookEx failed: %w", callErr)
	}
	return nil
}

// RunMessagePump drives the Win32 message loop required to receive low-level
// hook callbacks. It blocks until the thread's message queue receives WM_QUIT.
func RunMessagePump() {
	var m msg
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 { // 0 = WM_QUIT, -1 = error
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// hookProc receives lParam typed as unsafe.Pointer (the OS passes a pointer to
// a KBDLLHOOKSTRUCT) so we never convert a raw uintptr back into a pointer.
func hookProc(nCode uintptr, wParam uintptr, lParam unsafe.Pointer) uintptr {
	if nCode == hcAction && (wParam == wmKeyDown || wParam == wmSysKeyDown) {
		kb := (*kbdllhookstruct)(lParam)
		// Ignore keystrokes we inject ourselves, otherwise the expansion would
		// feed back into the matcher.
		if kb.Flags&llkhfInjected == 0 && handler != nil {
			if handler(kb.VkCode, resolveRune(kb.VkCode, kb.ScanCode)) {
				return 1 // suppress
			}
		}
	}
	ret, _, _ := procCallNextHookEx.Call(0, nCode, wParam, uintptr(lParam))
	return ret
}

// resolveRune translates a virtual-key + scan code into a printable rune using
// the current keyboard state and layout. Returns 0 for non-character keys and
// dead keys.
func resolveRune(vkCode, scanCode uint32) rune {
	var state [256]byte
	procGetKeyboardState.Call(uintptr(unsafe.Pointer(&state[0])))
	hkl, _, _ := procGetKeyboardLayout.Call(0)

	var buf [8]uint16
	ret, _, _ := procToUnicodeEx.Call(
		uintptr(vkCode),
		uintptr(scanCode),
		uintptr(unsafe.Pointer(&state[0])),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		0,
		hkl,
	)
	if int32(ret) == 1 {
		return rune(buf[0])
	}
	return 0
}

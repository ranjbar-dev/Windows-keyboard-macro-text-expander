//go:build windows

package expander

import (
	"fmt"
	"unsafe"

	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const (
	inputKeyboard     = 1
	keyeventfKeyUp    = 0x0002
	keyeventfUnicode  = 0x0004
	vkBack         uint16 = 0x08
)

// keybdInput mirrors KEYBDINPUT.
type keybdInput struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// input mirrors INPUT. The trailing padding makes the Go struct match the
// size of the C union (MOUSEINPUT is the largest member) so SendInput's cbSize
// is correct on amd64.
type input struct {
	Type uint32
	_    uint32
	Ki   keybdInput
	_    [8]byte
}

var (
	user32        = windows.NewLazySystemDLL("user32.dll")
	procSendInput = user32.NewProc("SendInput")
)

// winSender injects keystrokes via Win32 SendInput.
type winSender struct{}

func defaultSender() Sender { return winSender{} }

func (winSender) Send(events []Event) error {
	var inputs []input
	for _, ev := range events {
		switch ev.Kind {
		case EventBackspace:
			inputs = append(inputs,
				keyInput(vkBack, 0, 0),
				keyInput(vkBack, 0, keyeventfKeyUp),
			)
		case EventUnicode:
			// Encode to UTF-16 so characters outside the BMP send as surrogate pairs.
			for _, u := range utf16.Encode([]rune{ev.Char}) {
				inputs = append(inputs,
					keyInput(0, u, keyeventfUnicode),
					keyInput(0, u, keyeventfUnicode|keyeventfKeyUp),
				)
			}
		}
	}
	if len(inputs) == 0 {
		return nil
	}
	n, _, callErr := procSendInput.Call(
		uintptr(len(inputs)),
		uintptr(unsafe.Pointer(&inputs[0])),
		unsafe.Sizeof(inputs[0]),
	)
	if int(n) != len(inputs) {
		return fmt.Errorf("SendInput injected %d of %d events: %w", n, len(inputs), callErr)
	}
	return nil
}

func keyInput(vk, scan uint16, flags uint32) input {
	return input{
		Type: inputKeyboard,
		Ki:   keybdInput{Vk: vk, Scan: scan, Flags: flags},
	}
}

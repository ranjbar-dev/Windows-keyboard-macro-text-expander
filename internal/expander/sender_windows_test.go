//go:build windows

package expander

import (
	"testing"
	"unsafe"
)

// The Win32 INPUT struct is 40 bytes on amd64 (DWORD type + 4 padding + the
// 32-byte union sized to MOUSEINPUT). SendInput's cbSize must match exactly.
func TestInputStructSize(t *testing.T) {
	if got := unsafe.Sizeof(input{}); got != 40 {
		t.Fatalf("sizeof(input) = %d, want 40 (amd64 INPUT layout is wrong)", got)
	}
	// The keyboard union member must begin at offset 8 (after type + padding).
	if off := unsafe.Offsetof(input{}.Ki); off != 8 {
		t.Fatalf("offsetof(input.Ki) = %d, want 8", off)
	}
}

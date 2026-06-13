//go:build windows

package crypto

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// MasterPasswordTarget is the Credential Manager target name for the master password.
const MasterPasswordTarget = "Expander_MasterPassword"

const (
	credTypeGeneric        = 1 // CRED_TYPE_GENERIC
	credPersistLocalMachine = 2 // CRED_PERSIST_LOCAL_MACHINE
	errorNotFound          = 1168 // ERROR_NOT_FOUND
)

// credential mirrors the Win32 CREDENTIALW structure.
type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

var (
	advapi32       = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

// SavePassword writes (or overwrites) a generic credential. The password is
// stored as UTF-16LE bytes so it is also readable from the Windows
// Credential Manager UI.
func SavePassword(targetName, username, password string) error {
	target, err := windows.UTF16PtrFromString(targetName)
	if err != nil {
		return fmt.Errorf("encode target name: %w", err)
	}
	user, err := windows.UTF16PtrFromString(username)
	if err != nil {
		return fmt.Errorf("encode username: %w", err)
	}
	blob := utf16Bytes(password)

	cred := credential{
		Type:               credTypeGeneric,
		TargetName:         target,
		Persist:            credPersistLocalMachine,
		UserName:           user,
		CredentialBlobSize: uint32(len(blob)),
	}
	if len(blob) > 0 {
		cred.CredentialBlob = &blob[0]
	}

	ret, _, callErr := procCredWriteW.Call(uintptr(unsafe.Pointer(&cred)), 0)
	if ret == 0 {
		return fmt.Errorf("CredWriteW failed: %w", callErr)
	}
	return nil
}

// LoadPassword reads the generic credential's password. The username argument
// is accepted for API symmetry; lookup is by target name only.
func LoadPassword(targetName, username string) (string, error) {
	target, err := windows.UTF16PtrFromString(targetName)
	if err != nil {
		return "", fmt.Errorf("encode target name: %w", err)
	}
	var credPtr *credential
	ret, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(target)),
		credTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&credPtr)),
	)
	if ret == 0 {
		if en, ok := callErr.(windows.Errno); ok && uint32(en) == errorNotFound {
			return "", fmt.Errorf("credential %q not found", targetName)
		}
		return "", fmt.Errorf("CredReadW failed: %w", callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credPtr)))

	if credPtr.CredentialBlobSize == 0 || credPtr.CredentialBlob == nil {
		return "", nil
	}
	blob := unsafe.Slice(credPtr.CredentialBlob, credPtr.CredentialBlobSize)
	return stringFromUTF16Bytes(blob), nil
}

// DeletePassword removes the generic credential. Missing credentials are not
// treated as an error.
func DeletePassword(targetName string) error {
	target, err := windows.UTF16PtrFromString(targetName)
	if err != nil {
		return fmt.Errorf("encode target name: %w", err)
	}
	ret, _, callErr := procCredDeleteW.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if ret == 0 {
		if en, ok := callErr.(windows.Errno); ok && uint32(en) == errorNotFound {
			return nil
		}
		return fmt.Errorf("CredDeleteW failed: %w", callErr)
	}
	return nil
}

// utf16Bytes encodes s as UTF-16LE bytes (without a trailing NUL).
func utf16Bytes(s string) []byte {
	u := windows.StringToUTF16(s) // includes trailing NUL
	if len(u) > 0 {
		u = u[:len(u)-1] // drop NUL
	}
	out := make([]byte, len(u)*2)
	for i, r := range u {
		out[i*2] = byte(r)
		out[i*2+1] = byte(r >> 8)
	}
	return out
}

// stringFromUTF16Bytes decodes UTF-16LE bytes into a Go string.
func stringFromUTF16Bytes(b []byte) string {
	n := len(b) / 2
	u := make([]uint16, n)
	for i := range n {
		u[i] = uint16(b[i*2]) | uint16(b[i*2+1])<<8
	}
	return windows.UTF16ToString(u)
}

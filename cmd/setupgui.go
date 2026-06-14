//go:build windows

package cmd

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/lxn/walk"
	//lint:ignore ST1001 walk's declarative API is idiomatically dot-imported.
	. "github.com/lxn/walk/declarative"
	"gopkg.in/yaml.v3"

	"expander/internal/config"
	"expander/internal/crypto"
	"expander/internal/winutil"
)

// terminatorOptions are the terminator names offered in the Add dialog.
var terminatorOptions = []string{"Tab", "Space", "Enter"}

// setupModel holds the mutable state shared across the setup GUI screens.
type setupModel struct {
	cfgPath  string
	sltPath  string
	salt     []byte
	key      []byte
	password string // captured only on first run, to store in Credential Manager
	firstRun bool
	timingMs int
}

// RunSetupGUI runs the graphical setup: a master-password gate followed by a
// window to add and remove shortcut records. It is launched as a separate
// process (`expander.exe setup-gui`) so its message loop stays isolated from the
// tray and keyboard-hook threads. Exit code 0 means the config was saved; 1
// means the user cancelled or an error occurred.
func RunSetupGUI() {
	runtime.LockOSThread()
	code, err := runSetupGUI()
	if err != nil {
		walk.MsgBox(nil, "Expander Setup", "Setup error: "+err.Error(), walk.MsgBoxIconError)
		os.Exit(1)
	}
	os.Exit(code)
}

func runSetupGUI() (int, error) {
	cfgPath, err := configPath()
	if err != nil {
		return 1, err
	}
	sltPath, err := saltPath()
	if err != nil {
		return 1, err
	}

	m := &setupModel{cfgPath: cfgPath, sltPath: sltPath, timingMs: config.DefaultTimingWindowMs}
	if salt, err := os.ReadFile(sltPath); err == nil && len(salt) == crypto.SaltLen {
		m.salt = salt
	}
	existing, timing := loadExistingLenient(cfgPath)
	if timing > 0 {
		m.timingMs = timing
	}
	m.firstRun = len(m.salt) != crypto.SaltLen

	ok, err := runPasswordDialog(m, existing)
	if err != nil {
		return 1, err
	}
	if !ok {
		return 1, nil // cancelled at the password gate
	}

	saved, err := runRecordsWindow(m, existing)
	if err != nil {
		return 1, err
	}
	if !saved {
		return 1, nil
	}
	return 0, nil
}

// loadExistingLenient reads the raw shortcuts and timing window from config.yml
// without enforcing config.Validate's "at least one shortcut" rule, so the GUI
// can start from an empty or absent file. Encrypted expansions are left as-is.
func loadExistingLenient(path string) ([]config.Shortcut, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0
	}
	var cfg config.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, 0
	}
	return cfg.Shortcuts, cfg.Settings.TimingWindowMs
}

// runPasswordDialog shows the master-password gate. On first run it asks for a
// password and confirmation and seeds a fresh salt/key; otherwise it verifies
// the entered password and derives the key from the existing salt. Returns
// false if the user cancels.
func runPasswordDialog(m *setupModel, existing []config.Shortcut) (bool, error) {
	var dlg *walk.Dialog
	var okBtn, cancelBtn *walk.PushButton
	var pwEdit, confirmEdit *walk.LineEdit
	accepted := false

	prompt := "Enter your master password:"
	if m.firstRun {
		prompt = "Create a master password:"
	}

	children := []Widget{
		Label{Text: prompt},
		LineEdit{AssignTo: &pwEdit, PasswordMode: true},
	}
	if m.firstRun {
		children = append(children,
			Label{Text: "Confirm password:"},
			LineEdit{AssignTo: &confirmEdit, PasswordMode: true},
		)
	}
	children = append(children, Composite{
		Layout: HBox{MarginsZero: true},
		Children: []Widget{
			HSpacer{},
			PushButton{AssignTo: &okBtn, Text: "OK", OnClicked: func() {
				pw := pwEdit.Text()
				if pw == "" {
					walk.MsgBox(dlg, "Password required", "Password cannot be empty.", walk.MsgBoxIconWarning)
					return
				}
				if m.firstRun {
					if pw != confirmEdit.Text() {
						walk.MsgBox(dlg, "Mismatch", "Passwords do not match.", walk.MsgBoxIconWarning)
						return
					}
					if err := initFirstRun(m, pw); err != nil {
						walk.MsgBox(dlg, "Error", err.Error(), walk.MsgBoxIconError)
						return
					}
				} else if err := verifyPassword(m, existing, pw); err != nil {
					walk.MsgBox(dlg, "Incorrect password", err.Error(), walk.MsgBoxIconWarning)
					return
				}
				accepted = true
				dlg.Accept()
			}},
			PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
		},
	})

	_, err := Dialog{
		AssignTo:      &dlg,
		Title:         "Expander — Master Password",
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		MinSize:       Size{Width: 360, Height: 150},
		Layout:        VBox{},
		Children:      children,
	}.Run(nil)
	return accepted, err
}

// initFirstRun generates a fresh salt and derives the key for a brand-new setup.
func initFirstRun(m *setupModel, pw string) error {
	salt := make([]byte, crypto.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}
	m.salt = salt
	m.key = crypto.DeriveKey(pw, salt)
	m.password = pw
	return nil
}

// verifyPassword confirms pw against the existing config: it decrypts the first
// ENC: expansion as a check, falling back to the Credential Manager copy when no
// encrypted entry exists. On success it stores the derived key on m.
func verifyPassword(m *setupModel, existing []config.Shortcut, pw string) error {
	key := crypto.DeriveKey(pw, m.salt)
	for _, s := range existing {
		if strings.HasPrefix(s.Expansion, crypto.EncPrefix) {
			if _, err := crypto.Decrypt(s.Expansion, key); err != nil {
				return errors.New("incorrect master password")
			}
			m.key = key
			return nil
		}
	}
	if stored, err := crypto.LoadPassword(crypto.MasterPasswordTarget, appDirName); err == nil && stored != pw {
		return errors.New("incorrect master password")
	}
	m.key = key
	return nil
}

// recordModel backs the records TableView. items holds the shortcuts with their
// expansions still encrypted (ENC:).
type recordModel struct {
	walk.TableModelBase
	items []config.Shortcut
}

func (m *recordModel) RowCount() int { return len(m.items) }

func (m *recordModel) Value(row, col int) any {
	s := m.items[row]
	switch col {
	case 0:
		return s.Trigger
	case 1:
		return s.Description
	case 2:
		return s.Terminator
	default:
		return "••••••• (encrypted)"
	}
}

// runRecordsWindow shows the main window listing shortcuts with Add/Remove/Save.
// Returns true if the user saved.
func runRecordsWindow(m *setupModel, existing []config.Shortcut) (bool, error) {
	model := &recordModel{items: append([]config.Shortcut(nil), existing...)}
	var mw *walk.MainWindow
	var tv *walk.TableView
	var timingEdit *walk.NumberEdit
	saved := false

	_, err := MainWindow{
		AssignTo: &mw,
		Title:    "Expander Setup",
		MinSize:  Size{Width: 580, Height: 360},
		Size:     Size{Width: 640, Height: 420},
		Layout:   VBox{},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					Label{Text: "Timing window (ms):"},
					NumberEdit{AssignTo: &timingEdit, MinValue: 100, MaxValue: 60000, Value: float64(m.timingMs)},
					HSpacer{},
				},
			},
			TableView{
				AssignTo: &tv,
				Model:    model,
				Columns: []TableViewColumn{
					{Title: "Trigger", Width: 100},
					{Title: "Description", Width: 240},
					{Title: "Terminator", Width: 90},
					{Title: "Expansion", Width: 140},
				},
				MinSize: Size{Height: 220},
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{Text: "Add…", OnClicked: func() {
						s, ok := runAddDialog(mw, m.key)
						if ok {
							model.items = append(model.items, s)
							model.PublishRowsReset()
						}
					}},
					PushButton{Text: "Remove", OnClicked: func() {
						i := tv.CurrentIndex()
						if i < 0 {
							return
						}
						model.items = append(model.items[:i], model.items[i+1:]...)
						model.PublishRowsReset()
					}},
					HSpacer{},
					PushButton{Text: "Save", OnClicked: func() {
						m.timingMs = int(timingEdit.Value())
						if err := saveConfig(m, model.items); err != nil {
							walk.MsgBox(mw, "Save failed", err.Error(), walk.MsgBoxIconError)
							return
						}
						saved = true
						mw.Close()
					}},
					PushButton{Text: "Cancel", OnClicked: func() { mw.Close() }},
				},
			},
		},
	}.Run()
	return saved, err
}

// runAddDialog collects one new shortcut, validates it, and encrypts the
// expansion with key. Returns false if cancelled.
func runAddDialog(owner walk.Form, key []byte) (config.Shortcut, bool) {
	var dlg *walk.Dialog
	var okBtn, cancelBtn *walk.PushButton
	var trigEdit, descEdit, valEdit *walk.LineEdit
	var termCombo *walk.ComboBox
	var result config.Shortcut
	accepted := false

	Dialog{
		AssignTo:      &dlg,
		Title:         "Add shortcut",
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		MinSize:       Size{Width: 400, Height: 250},
		Layout:        VBox{},
		Children: []Widget{
			Label{Text: "Trigger (1–32 printable ASCII, no spaces):"},
			LineEdit{AssignTo: &trigEdit},
			Label{Text: "Description (optional):"},
			LineEdit{AssignTo: &descEdit},
			Label{Text: "Terminator:"},
			ComboBox{AssignTo: &termCombo, Model: terminatorOptions, CurrentIndex: 0},
			Label{Text: "Expansion value (hidden):"},
			LineEdit{AssignTo: &valEdit, PasswordMode: true},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					HSpacer{},
					PushButton{AssignTo: &okBtn, Text: "Add", OnClicked: func() {
						trig := strings.TrimSpace(trigEdit.Text())
						if err := config.ValidateTrigger(trig); err != nil {
							walk.MsgBox(dlg, "Invalid trigger", err.Error(), walk.MsgBoxIconWarning)
							return
						}
						term := termCombo.Text()
						if !config.ValidTerminator(term) {
							walk.MsgBox(dlg, "Invalid terminator", "Choose Tab, Space, or Enter.", walk.MsgBoxIconWarning)
							return
						}
						val := valEdit.Text()
						if val == "" {
							walk.MsgBox(dlg, "Empty value", "Expansion cannot be empty.", walk.MsgBoxIconWarning)
							return
						}
						enc, err := crypto.Encrypt(val, key)
						if err != nil {
							walk.MsgBox(dlg, "Encryption failed", err.Error(), walk.MsgBoxIconError)
							return
						}
						result = config.Shortcut{
							Trigger:     trig,
							Description: strings.TrimSpace(descEdit.Text()),
							Terminator:  term,
							Expansion:   enc,
						}
						accepted = true
						dlg.Accept()
					}},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Run(owner)

	return result, accepted
}

// saveConfig persists the shortcuts: it writes the salt and stores the master
// password on first run, writes config.yml, and ensures the auto-start key.
func saveConfig(m *setupModel, shortcuts []config.Shortcut) error {
	if len(shortcuts) == 0 {
		return errors.New("add at least one shortcut before saving")
	}
	if m.firstRun {
		if err := os.WriteFile(m.sltPath, m.salt, 0o600); err != nil {
			return fmt.Errorf("write salt: %w", err)
		}
		if err := crypto.SavePassword(crypto.MasterPasswordTarget, appDirName, m.password); err != nil {
			return fmt.Errorf("store master password: %w", err)
		}
	}
	cfg := &config.Config{
		Settings:  config.Settings{TimingWindowMs: m.timingMs},
		Shortcuts: shortcuts,
	}
	if err := writeConfigFile(m.cfgPath, cfg); err != nil {
		return err
	}
	exe, err := exePath()
	if err != nil {
		return err
	}
	if err := winutil.SetRunKey(appDirName, exe); err != nil {
		return fmt.Errorf("write auto-start registry key: %w", err)
	}
	return nil
}

// writeConfigFile marshals cfg to YAML with a header comment and 0o600 perms.
func writeConfigFile(path string, cfg *config.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	const header = "# Expander configuration — managed by the Setup GUI.\n" +
		"# ENC: values are AES-GCM encrypted.\n\n"
	if err := os.WriteFile(path, append([]byte(header), data...), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

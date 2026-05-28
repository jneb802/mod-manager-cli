package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mmcli/internal/config"
	"mmcli/internal/profile"
)

func testTUIProfileModel(t *testing.T) model {
	t.Helper()

	tmp := t.TempDir()
	paths := config.Paths{
		ConfigDir:    filepath.Join(tmp, "config"),
		RegistryFile: filepath.Join(tmp, "config", "registry.json"),
		ProfilesDir:  filepath.Join(tmp, "config", "profiles"),
		ValheimDir:   filepath.Join(tmp, "Valheim"),
	}
	if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := profile.Create(paths, "active"); err != nil {
		t.Fatal(err)
	}
	if err := profile.Create(paths, "delete-me"); err != nil {
		t.Fatal(err)
	}

	reg := config.NewRegistry()
	reg.EnsureProfile("active")
	reg.EnsureProfile("delete-me")

	return model{
		paths: paths,
		cfg: config.Config{
			ActiveProfile: "active",
		},
		reg: &reg,
		local: localModel{
			updates: make(map[string]string),
		},
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestProfilePickerDeletesInactiveProfile(t *testing.T) {
	m := testTUIProfileModel(t)

	opened, _ := m.openProfilePicker()
	m = opened.(model)
	for i, name := range m.mods.profiles {
		if name == "delete-me" {
			m.mods.profileCursor = i
			break
		}
	}

	updated, _ := m.handleModsProfilePicker(keyRunes("d"))
	m = updated.(model)
	if !m.confirm.Active {
		t.Fatal("delete should ask for confirmation")
	}

	confirmed, _ := m.handleConfirm(keyRunes("y"))
	m = confirmed.(model)

	if _, err := os.Stat(m.paths.ProfileDir("delete-me")); !os.IsNotExist(err) {
		t.Fatalf("profile directory still exists after delete: %v", err)
	}
	if _, ok := m.reg.Profiles["delete-me"]; ok {
		t.Fatal("deleted profile still exists in registry profiles")
	}
	if _, ok := m.reg.Settings["delete-me"]; ok {
		t.Fatal("deleted profile still exists in registry settings")
	}
	for _, name := range m.mods.profiles {
		if name == "delete-me" {
			t.Fatal("deleted profile still appears in picker")
		}
	}
	if m.mods.statusMsg != "Profile 'delete-me' deleted." {
		t.Fatalf("statusMsg = %q", m.mods.statusMsg)
	}
	if !strings.Contains(m.viewProfilePicker(), "Profile 'delete-me' deleted.") {
		t.Fatal("profile picker should show delete success")
	}
}

func TestProfilePickerRefusesActiveProfileDelete(t *testing.T) {
	m := testTUIProfileModel(t)

	opened, _ := m.openProfilePicker()
	m = opened.(model)
	for i, name := range m.mods.profiles {
		if name == "active" {
			m.mods.profileCursor = i
			break
		}
	}

	updated, _ := m.handleModsProfilePicker(keyRunes("d"))
	m = updated.(model)

	if m.confirm.Active {
		t.Fatal("active profile delete should not ask for confirmation")
	}
	if m.mods.err == nil || !strings.Contains(m.mods.err.Error(), "cannot delete active profile") {
		t.Fatalf("err = %v", m.mods.err)
	}
	if !strings.Contains(m.viewProfilePicker(), "cannot delete active profile") {
		t.Fatal("profile picker should show active-profile delete error")
	}
	if _, err := os.Stat(m.paths.ProfileDir("active")); err != nil {
		t.Fatalf("active profile should still exist: %v", err)
	}
}

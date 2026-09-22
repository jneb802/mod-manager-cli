package thunderstore

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testR2X = `profileName: full valheim
mods:
- name: denikson-BepInExPack_Valheim
  version:
    major: 5
    minor: 4
    patch: 2350
  enabled: true
- name: Azumatt-AzuAutoStore
  version:
    major: 3
    minor: 1
    patch: 6
  enabled: true
  source: Hexium
- name: JoeyBadManners-OreSpreadQoL
  version:
    major: 1
    minor: 0
    patch: 4
  enabled: false
`

// writeR2Z builds a .r2z archive containing export.r2x plus a config file.
func writeR2Z(t *testing.T, dir, name string) string {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for path, body := range map[string]string{
		"export.r2x":                 testR2X,
		"BepInEx/config/AzuAuto.cfg": "[General]\n",
	} {
		w, err := zw.Create(path)
		if err != nil {
			t.Fatalf("zip create %s: %v", path, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", path, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestIsProfileFile(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "r2z", input: "full valheim.r2z", want: true},
		{name: "r2x", input: "export.r2x", want: true},
		{name: "uppercase extension", input: "EXPORT.R2Z", want: true},
		{name: "path with directories", input: "/Users/me/Downloads/full valheim.r2z", want: true},
		{name: "zip is not a profile file", input: "mod.zip", want: false},
		{name: "modpack query", input: "Author-ModpackName", want: false},
		{name: "profile code", input: "a1b2c3d4-e5f6-7890-abcd-ef1234567890", want: false},
		{name: "empty", input: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsProfileFile(tt.input); got != tt.want {
				t.Errorf("IsProfileFile(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestReadProfileFileR2Z(t *testing.T) {
	path := writeR2Z(t, t.TempDir(), "full valheim.r2z")

	profileName, mods, zipData, err := ReadProfileFile(path)
	if err != nil {
		t.Fatalf("ReadProfileFile: %v", err)
	}
	if profileName != "full valheim" {
		t.Errorf("profileName = %q, want %q", profileName, "full valheim")
	}
	if len(mods) != 3 {
		t.Fatalf("got %d mods, want 3", len(mods))
	}
	if mods[1].Name != "Azumatt-AzuAutoStore" || mods[1].Version != "3.1.6" {
		t.Errorf("mods[1] = %+v, want Azumatt-AzuAutoStore 3.1.6", mods[1])
	}
	if !mods[1].Enabled {
		t.Error("mods[1] should be enabled")
	}
	if mods[2].Enabled {
		t.Error("mods[2] is marked enabled: false and should be disabled")
	}
	if len(zipData) == 0 {
		t.Error("zip data should be returned so configs can be extracted")
	}
}

func TestReadProfileFileR2X(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.r2x")
	if err := os.WriteFile(path, []byte(testR2X), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	profileName, mods, zipData, err := ReadProfileFile(path)
	if err != nil {
		t.Fatalf("ReadProfileFile: %v", err)
	}
	if profileName != "full valheim" {
		t.Errorf("profileName = %q, want %q", profileName, "full valheim")
	}
	if len(mods) != 3 {
		t.Errorf("got %d mods, want 3", len(mods))
	}
	if zipData != nil {
		t.Error("a bare .r2x carries no config files, so zip data should be nil")
	}
}

// A profile code payload saved straight to disk keeps its header and base64 body.
func TestReadProfileFileAcceptsProfileCodePayload(t *testing.T) {
	dir := t.TempDir()
	raw, err := os.ReadFile(writeR2Z(t, dir, "source.r2z"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	path := filepath.Join(dir, "payload.r2z")
	body := "#r2modman\n" + base64.StdEncoding.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	profileName, mods, zipData, err := ReadProfileFile(path)
	if err != nil {
		t.Fatalf("ReadProfileFile: %v", err)
	}
	if profileName != "full valheim" {
		t.Errorf("profileName = %q, want %q", profileName, "full valheim")
	}
	if len(mods) != 3 {
		t.Errorf("got %d mods, want 3", len(mods))
	}
	if len(zipData) == 0 {
		t.Error("decoded zip data should be returned")
	}
}

func TestReadProfileFileMissing(t *testing.T) {
	_, _, _, err := ReadProfileFile(filepath.Join(t.TempDir(), "nope.r2z"))
	if err == nil {
		t.Fatal("expected an error for a missing file")
	}
	if !strings.Contains(err.Error(), "failed to read profile file") {
		t.Errorf("error = %q, want it to mention the read failure", err)
	}
}

func TestReadProfileFileNotAZip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.r2z")
	if err := os.WriteFile(path, []byte("not a zip"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, _, err := ReadProfileFile(path)
	if err == nil {
		t.Fatal("expected an error for a file that is not a zip")
	}
}

func TestReadProfileFileZipWithoutExportR2X(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("BepInEx/config/Some.cfg")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	w.Write([]byte("x"))
	zw.Close()

	path := filepath.Join(t.TempDir(), "noexport.r2z")
	if err := os.WriteFile(path, buf.Bytes(), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, _, err = ReadProfileFile(path)
	if err == nil || !strings.Contains(err.Error(), "missing export.r2x") {
		t.Fatalf("error = %v, want it to report the missing export.r2x", err)
	}
}

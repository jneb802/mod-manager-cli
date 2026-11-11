package bepinex

import (
	"archive/zip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mmcli/internal/config"
)

// startScript mirrors the lines of BepInExPack 5.4.2360's start_game_bepinex.sh
// that PatchRunScript must change or must leave alone.
const startScript = `#!/bin/sh
executable_name="valheim.x86_64"
archpreference="arm64,x86_64"
if [ -x "$1" ] ; then
    executable_name="$1"
    shift
fi
arch=""
        real_executable_name="$(abs_path "$executable_name")"
        exec "$executable_path" "$@"
        exec arch \
            -e DYLD_LIBRARY_PATH="${dyld_library_path}" \
            -e DYLD_INSERT_LIBRARIES="${dyld_insert_libraries}" \
            "$executable_path" "$@"
`

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "pack.zip")
	f, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for name, body := range files {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestPatchRunScriptKeepsNativeLaunch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("PatchRunScript only patches on macOS")
	}
	paths := config.Paths{ValheimDir: t.TempDir()}
	if err := os.WriteFile(paths.RunBepInExScript(), []byte(startScript), 0644); err != nil {
		t.Fatal(err)
	}

	if err := PatchRunScript(paths); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, paths.RunBepInExScript())
	want := strings.Replace(startScript, `executable_name="valheim.x86_64"`, `executable_name="Valheim.app"`, 1)
	if got != want {
		t.Fatalf("patched script changed more than executable_name:\n%s", got)
	}
}

func TestInstallKeepsBepInExConfigAndRemovesLegacyDoorstop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows installs the BepInEx tree into profiles")
	}
	paths := config.Paths{ValheimDir: t.TempDir()}
	cfgPath := filepath.Join(paths.ValheimDir, "BepInEx", "config", "BepInEx.cfg")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("user settings"), 0644); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(paths.ValheimDir, legacyDoorstopLib)
	if err := os.WriteFile(legacyPath, []byte("old x86_64 doorstop"), 0644); err != nil {
		t.Fatal(err)
	}

	zipPath := writeZip(t, map[string]string{
		"manifest.json": "{}",
		"BepInExPack_Valheim/start_game_bepinex.sh":               startScript,
		"BepInExPack_Valheim/doorstop_libs/libdoorstop_x64.dylib": "universal doorstop",
		"BepInExPack_Valheim/BepInEx/core/BepInEx.Preloader.dll":  "preloader",
		"BepInExPack_Valheim/BepInEx/config/BepInEx.cfg":          "pack defaults",
	})

	if err := Install(paths, zipPath, nil); err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, cfgPath); got != "user settings" {
		t.Errorf("BepInEx.cfg = %q, want existing user settings kept", got)
	}
	if got := readFile(t, paths.RunBepInExScript()); got != startScript {
		t.Errorf("run_bepinex.sh was not installed from start_game_bepinex.sh")
	}
	if got := readFile(t, filepath.Join(paths.BepInExCoreDir(), "BepInEx.Preloader.dll")); got != "preloader" {
		t.Errorf("BepInEx.Preloader.dll = %q, want pack file", got)
	}
	if got := readFile(t, filepath.Join(paths.ValheimDir, macDoorstopLib)); got != "universal doorstop" {
		t.Errorf("doorstop library = %q, want pack file", got)
	}
	if _, err := os.Stat(filepath.Join(paths.ValheimDir, "manifest.json")); !os.IsNotExist(err) {
		t.Errorf("manifest.json should be skipped")
	}

	_, err := os.Stat(legacyPath)
	if runtime.GOOS == "darwin" && !os.IsNotExist(err) {
		t.Errorf("legacy %s should be removed on macOS", legacyDoorstopLib)
	}
	if runtime.GOOS != "darwin" && err != nil {
		t.Errorf("legacy %s should be left alone outside macOS: %v", legacyDoorstopLib, err)
	}
}

func TestInstallWritesDefaultBepInExConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows installs the BepInEx tree into profiles")
	}
	paths := config.Paths{ValheimDir: t.TempDir()}
	zipPath := writeZip(t, map[string]string{
		"BepInExPack_Valheim/BepInEx/config/BepInEx.cfg": "pack defaults",
	})

	if err := Install(paths, zipPath, nil); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(paths.ValheimDir, "BepInEx", "config", "BepInEx.cfg")
	if got := readFile(t, cfgPath); got != "pack defaults" {
		t.Errorf("BepInEx.cfg = %q, want pack defaults on a fresh install", got)
	}
}

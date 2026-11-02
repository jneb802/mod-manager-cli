package bepinex

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"mmcli/internal/config"
)

const bepinexPackage = "denikson/BepInExPack_Valheim"

type tsVersion struct {
	VersionNumber string `json:"version_number"`
	DownloadURL   string `json:"download_url"`
}

type tsPackage struct {
	Versions []tsVersion `json:"versions"`
}

// LatestVersion fetches the latest BepInExPack_Valheim version info.
func LatestVersion() (version string, downloadURL string, err error) {
	url := fmt.Sprintf("https://thunderstore.io/api/experimental/package/%s/", bepinexPackage)
	resp, err := http.Get(url)
	if err != nil {
		return "", "", fmt.Errorf("failed to query Thunderstore: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("Thunderstore returned HTTP %d", resp.StatusCode)
	}

	var pkg struct {
		Latest struct {
			VersionNumber string `json:"version_number"`
			DownloadURL   string `json:"download_url"`
		} `json:"latest"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&pkg); err != nil {
		return "", "", fmt.Errorf("failed to parse response: %w", err)
	}

	return pkg.Latest.VersionNumber, pkg.Latest.DownloadURL, nil
}

// Download downloads the BepInEx zip to the cache directory.
func Download(paths config.Paths, downloadURL, version string) (string, error) {
	os.MkdirAll(paths.CacheDir, 0755)
	zipPath := filepath.Join(paths.CacheDir, fmt.Sprintf("BepInExPack_Valheim-%s.zip", version))

	// Skip download if already cached
	if _, err := os.Stat(zipPath); err == nil {
		return zipPath, nil
	}

	resp, err := http.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download BepInEx: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download failed with HTTP %d", resp.StatusCode)
	}

	f, err := os.Create(zipPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		os.Remove(zipPath)
		return "", fmt.Errorf("download interrupted: %w", err)
	}

	return zipPath, nil
}

// legacyDoorstopLib is the root-level Doorstop library that BepInExPack shipped
// for macOS before 5.4.2360. It was x86_64 only, which is why mmcli used to
// force Rosetta.
const legacyDoorstopLib = "libdoorstop.dylib"

// macDoorstopLib is the Doorstop library that the 5.4.2360+ start script loads
// on macOS. Despite the name it is a universal build (x86_64 and arm64).
var macDoorstopLib = filepath.Join("doorstop_libs", "libdoorstop_x64.dylib")

// Install extracts the BepInEx zip into the Valheim directory.
// Strips the "BepInExPack_Valheim/" prefix and skips metadata files.
// Removes any dangling symlinks from a previous mmcli install first.
// An existing BepInEx.cfg is kept so re-installing does not reset the user's
// BepInEx settings. On Windows the BepInEx/ tree is written into each of the
// given profiles; other platforms ignore profiles.
func Install(paths config.Paths, zipPath string, profiles []string) error {
	// Clean up dangling symlinks left from previous profile symlinks
	removeDanglingSymlinks(paths.BepInExDir())

	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	skipFiles := map[string]bool{
		"icon.png":      true,
		"manifest.json": true,
		"README.md":     true,
		"CHANGELOG.md":  true,
	}

	for _, f := range r.File {
		name := f.Name

		// Strip the BepInExPack_Valheim/ prefix
		if idx := strings.Index(name, "/"); idx >= 0 {
			prefix := name[:idx]
			if strings.HasPrefix(prefix, "BepInExPack") {
				name = name[idx+1:]
			}
		}

		if name == "" {
			continue
		}

		// Rename start_game_bepinex.sh to run_bepinex.sh so PatchRunScript
		// and the macOS launch path can find it under the expected name.
		if name == "start_game_bepinex.sh" {
			name = "run_bepinex.sh"
		}

		// Skip metadata files
		baseName := filepath.Base(name)
		if skipFiles[baseName] {
			continue
		}

		for _, destPath := range installDestPaths(paths, name, profiles) {
			if f.FileInfo().IsDir() {
				os.MkdirAll(destPath, 0755)
				continue
			}

			if baseName == "BepInEx.cfg" {
				if _, err := os.Stat(destPath); err == nil {
					continue
				}
			}

			if err := extractFile(f, destPath); err != nil {
				return fmt.Errorf("failed to extract %s: %w", name, err)
			}
		}
	}

	return removeLegacyDoorstop(paths)
}

// removeLegacyDoorstop deletes the old root-level macOS Doorstop library once
// the universal one from doorstop_libs/ is in place.
func removeLegacyDoorstop(paths config.Paths) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(paths.ValheimDir, macDoorstopLib)); err != nil {
		return nil
	}
	err := os.Remove(filepath.Join(paths.ValheimDir, legacyDoorstopLib))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", legacyDoorstopLib, err)
	}
	return nil
}

// PatchRunScript points run_bepinex.sh at Steam's macOS app bundle. The stock
// script defaults executable_name to the Linux binary name.
//
// Nothing else is changed. Since BepInExPack 5.4.2360 the stock script launches
// the game through `arch` using its archpreference setting (native arm64 first,
// Rosetta as fallback) and loads a universal Doorstop library, so Valheim runs
// natively on Apple Silicon.
func PatchRunScript(paths config.Paths) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	scriptPath := paths.RunBepInExScript()
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("run_bepinex.sh not found: %w", err)
	}

	// Match Steam's macOS app bundle name exactly. Case-sensitive volumes fail
	// if the script uses valheim.app while the bundle is Valheim.app.
	content := replaceScriptVar(string(data), "executable_name", "Valheim.app")

	return os.WriteFile(scriptPath, []byte(content), 0755)
}

// RemoveQuarantine strips the com.apple.quarantine extended attribute from the
// Valheim directory. Downloaded files are tagged by macOS Gatekeeper and
// quarantined dylibs (like the Doorstop library) are silently blocked from loading.
func RemoveQuarantine(paths config.Paths) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	// xattr -rd removes the attribute recursively; errors are expected for files
	// that don't have it, so we only fail on exec errors.
	cmd := exec.Command("xattr", "-rd", "com.apple.quarantine", paths.ValheimDir)
	cmd.Run() // ignore exit code — files without the attribute cause non-zero exit
	return nil
}

// MakeExecutable sets executable permissions on required files.
func MakeExecutable(paths config.Paths) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	files := []string{
		paths.RunBepInExScript(),
		filepath.Join(paths.ValheimDir, macDoorstopLib),
	}
	for _, f := range files {
		if _, err := os.Stat(f); err == nil {
			if err := os.Chmod(f, 0755); err != nil {
				return fmt.Errorf("failed to chmod %s: %w", filepath.Base(f), err)
			}
		}
	}
	return nil
}

func extractFile(f *zip.File, destPath string) error {
	os.MkdirAll(filepath.Dir(destPath), 0755)

	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, rc)
	return err
}

func installDestPaths(paths config.Paths, name string, profiles []string) []string {
	if runtime.GOOS != "windows" {
		return []string{filepath.Join(paths.ValheimDir, name)}
	}

	trimmed := strings.TrimPrefix(filepath.ToSlash(name), "./")
	if trimmed == "BepInEx" || strings.HasPrefix(trimmed, "BepInEx/") {
		dests := make([]string, 0, len(profiles))
		for _, p := range profiles {
			dests = append(dests, filepath.Join(paths.ProfileDir(p), filepath.FromSlash(trimmed)))
		}
		return dests
	}
	return []string{filepath.Join(paths.ValheimDir, filepath.FromSlash(trimmed))}
}

// removeDanglingSymlinks removes symlinks in a directory that point to non-existent targets.
func removeDanglingSymlinks(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		info, err := os.Lstat(path)
		if err != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			if _, err := os.Stat(path); err != nil {
				os.Remove(path)
			}
		}
	}
}

// replaceScriptVar sets a top-level variable in a shell script. Indented
// assignments are left alone because they are runtime logic, such as the start
// script taking executable_name from its first argument.
func replaceScriptVar(content, varName, value string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, varName+"=") {
			lines[i] = fmt.Sprintf(`%s="%s"`, varName, value)
		}
	}
	return strings.Join(lines, "\n")
}

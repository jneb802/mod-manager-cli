package cmd

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"mmcli/internal/config"
	"mmcli/internal/profile"
)

type profileConfigEntry struct {
	file *zip.File
	path string
}

// Validate the whole config file list before creating any output files.
func profileConfigEntries(zipData []byte) ([]profileConfigEntry, error) {
	if zipData == nil { // Bare .r2x manifests contain no config archive.
		return nil, nil
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to read profile configs: %w", err)
	}
	var entries []profileConfigEntry
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := strings.ReplaceAll(f.Name, "\\", "/")
		var relative string
		switch {
		case strings.HasPrefix(name, "BepInEx/config/"):
			relative = strings.TrimPrefix(name, "BepInEx/config/")
		case strings.HasPrefix(name, "config/"):
			relative = strings.TrimPrefix(name, "config/")
		default:
			continue
		}
		if !filepath.IsLocal(relative) || strings.ContainsAny(relative, ":\x00") || f.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("invalid exported config path %q", f.Name)
		}
		for _, part := range strings.Split(relative, "/") {
			if part == ".." {
				return nil, fmt.Errorf("invalid exported config path %q", f.Name)
			}
		}
		entries = append(entries, profileConfigEntry{f, filepath.FromSlash(relative)})
	}
	return entries, nil
}

// Root-based operations prevent paths and existing symlinks from escaping the
// destination, including when a directory is changed during extraction.
func extractProfileConfigs(paths config.Paths, profileName string, zipData []byte, source string) error {
	if err := profile.ValidateName(profileName); err != nil {
		return err
	}
	entries, err := profileConfigEntries(zipData)
	if err != nil || len(entries) == 0 {
		return err
	}
	profilesRoot, err := os.OpenRoot(paths.ProfilesDir)
	if err != nil {
		return err
	}
	defer profilesRoot.Close()
	profileRoot, err := profilesRoot.OpenRoot(profileName)
	if err != nil {
		return err
	}
	defer profileRoot.Close()
	configPath, err := filepath.Rel(paths.ProfileDir(profileName), paths.ProfileConfigDir(profileName))
	if err != nil {
		return err
	}
	info, err := profileRoot.Lstat(configPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("profile config directory must not be a symlink")
	}
	configRoot, err := profileRoot.OpenRoot(configPath)
	if err != nil {
		return err
	}
	defer configRoot.Close()
	for _, entry := range entries {
		if err := writeProfileConfig(configRoot, entry); err != nil {
			return fmt.Errorf("failed to extract config %q: %w", entry.file.Name, err)
		}
	}
	fmt.Printf("Extracted %d config file(s) from %s.\n", len(entries), source)
	return nil
}

func writeProfileConfig(root *os.Root, entry profileConfigEntry) error {
	if err := root.MkdirAll(filepath.Dir(entry.path), 0755); err != nil {
		return err
	}
	rc, err := entry.file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := root.Create(entry.path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, rc)
	return errors.Join(copyErr, out.Close())
}

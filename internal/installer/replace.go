package installer

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"mmcli/internal/config"
	"mmcli/internal/thunderstore"
)

// replaceMod prepares the complete archive before touching installed files.
// Directory backups permit rollback if publishing any part of the mod fails.
func replaceMod(paths config.Paths, cfg config.Config, pkg *thunderstore.Package, disabled bool) (files []string, err error) {
	if err := os.MkdirAll(paths.ProfilesDir, 0755); err != nil {
		return nil, err
	}
	stage, err := os.MkdirTemp(paths.ProfilesDir, ".mmcli-replace-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	staged := paths
	staged.ProfilesDir = filepath.Join(stage, "profiles")
	staged.ValheimDir = filepath.Join(stage, "game")
	stagedFiles, err := downloadAndExtract(staged, cfg, pkg)
	if err != nil {
		return nil, err
	}
	profile := cfg.ActiveProfile
	name := fmt.Sprintf("%s-%s", pkg.Owner, pkg.Name)
	sources := modDirs(staged, profile, name)
	destinations := modDirs(paths, profile, name)
	if disabled {
		for _, dir := range sources {
			if err := renameDLLs(dir, ".dll", ".dll.old"); err != nil {
				return nil, err
			}
		}
	}

	type movedDirectory struct {
		destination string
		backup      string
		installed   bool
	}
	var moved []movedDirectory
	var addedConfigs []string
	defer func() {
		if err != nil {
			for _, path := range addedConfigs {
				err = errors.Join(err, os.Remove(path))
			}
			for i := len(moved) - 1; i >= 0; i-- {
				move := moved[i]
				if move.installed {
					err = errors.Join(err, os.RemoveAll(move.destination))
				}
				if move.backup != "" {
					if restoreErr := os.Rename(move.backup, move.destination); restoreErr != nil {
						err = errors.Join(err, fmt.Errorf("restore %s from %s: %w", move.destination, move.backup, restoreErr))
						continue // Retain the backup if restoration fails.
					}
					os.Remove(filepath.Dir(move.backup))
				}
			}
			return
		}
		for _, move := range moved {
			if move.backup != "" {
				if cleanupErr := os.RemoveAll(filepath.Dir(move.backup)); cleanupErr != nil {
					fmt.Printf("Warning: could not remove replacement backup %s: %v\n", move.backup, cleanupErr)
				}
			}
		}
	}()

	for i, destination := range destinations {
		if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return nil, err
		}
		move := movedDirectory{destination: destination}
		if _, statErr := os.Lstat(destination); statErr == nil {
			backupDir, backupErr := os.MkdirTemp(filepath.Dir(destination), ".mmcli-backup-")
			if backupErr != nil {
				return nil, backupErr
			}
			move.backup = filepath.Join(backupDir, strconv.Itoa(i))
			if err = os.Rename(destination, move.backup); err != nil {
				os.Remove(backupDir)
				return nil, err
			}
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
		moved = append(moved, move)
		if _, statErr := os.Stat(sources[i]); statErr == nil {
			if err = os.Rename(sources[i], destination); err != nil {
				return nil, err
			}
			moved[len(moved)-1].installed = true
		} else if !os.IsNotExist(statErr) {
			return nil, statErr
		}
	}

	// Publish only missing config files. Never overwrite user configuration.
	roots := append(append([]string{}, sources...), staged.ProfileConfigDir(profile))
	targets := append(append([]string{}, destinations...), paths.ProfileConfigDir(profile))
	for _, stagedFile := range stagedFiles {
		for i, root := range roots {
			relative, relErr := filepath.Rel(root, stagedFile)
			if relErr != nil || !filepath.IsLocal(relative) {
				continue
			}
			destination := filepath.Join(targets[i], relative)
			if i == len(sources) {
				if err = os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
					return nil, err
				}
				data, readErr := os.ReadFile(stagedFile)
				if readErr != nil {
					return nil, readErr
				}
				out, openErr := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
				if openErr == nil {
					addedConfigs = append(addedConfigs, destination)
					_, writeErr := out.Write(data)
					if err = errors.Join(writeErr, out.Close()); err != nil {
						return nil, err
					}
				} else if !os.IsExist(openErr) {
					return nil, openErr
				}
			}
			files = append(files, destination)
			break
		}
	}
	return files, nil
}

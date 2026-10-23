package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PersistentAdmins contains exact platform IDs accepted by the installed game.
// It is agent configuration, so a launch profile or mod mirror cannot replace it.
func (c AgentConfig) ensurePersistentAdmins(saveDir string) error {
	if len(c.PersistentAdmins) == 0 {
		return nil
	}
	for _, id := range c.PersistentAdmins {
		if id == "" || strings.ContainsAny(id, " \t\r\n") || strings.HasPrefix(id, "//") {
			return fmt.Errorf("persistent_admins must contain nonempty platform IDs without whitespace or comments")
		}
	}
	if saveDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		saveDir = filepath.Join(home, ".config", "unity3d", "IronGate", "Valheim")
	} else if !filepath.IsAbs(saveDir) {
		saveDir = filepath.Join(c.ValheimDir, saveDir)
	}
	path := filepath.Join(saveDir, "adminlist.txt")
	// Preserve a deliberately shared permission file rather than replacing its link.
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	} else {
		if info, statErr := os.Lstat(path); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("resolve admin list symlink: %w", err)
		}
		if !os.IsNotExist(err) {
			return err
		}
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read persistent admin list: %w", err)
	}
	existing := strings.Split(string(data), "\n")
	merged := mergeAdminIDs(existing, c.PersistentAdmins)
	if len(merged) == len(existing) {
		return nil
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += strings.Join(merged[len(existing):], "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".mmcli-admins-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func mergeAdminIDs(existing, required []string) []string {
	result := append([]string(nil), existing...)
	seen := make(map[string]bool, len(existing)+len(required))
	for _, id := range existing {
		seen[id] = true
	}
	for _, id := range required {
		if !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	return result
}

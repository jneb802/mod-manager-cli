//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStartProvisionsPersistentAdmins(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	cfg := AgentConfig{ValheimDir: root, StartScript: "start.sh", PersistentAdmins: []string{"Steam_required"}}
	// The child copies the admin file as its first action, proving ordering.
	binary := filepath.Join(root, "valheim_server.x86_64")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\ncp \"$2/adminlist.txt\" observed.txt\n"), 0755); err != nil {
		t.Fatal(err)
	}
	pm := NewProcessManager(cfg)
	for _, dir := range []string{"one", "two"} {
		if err := os.WriteFile(cfg.ResolvedStartScript(), []byte("exec ./valheim_server.x86_64 -savedir "+dir+"\n"), 0755); err != nil {
			t.Fatal(err)
		}
		os.Remove(filepath.Join(root, "observed.txt"))
		if err := pm.Start(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for pm.IsRunning() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if pm.IsRunning() {
			pm.Stop()
			t.Fatal("test child did not finish")
		}
		data, err := os.ReadFile(filepath.Join(root, "observed.txt"))
		if err != nil || string(data) != "Steam_required\n" {
			t.Fatalf("server started without admin: %q %v", data, err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "two", "adminlist.txt.block"), 0755); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(root, "two", "adminlist.txt"))
	os.Rename(filepath.Join(root, "two", "adminlist.txt.block"), filepath.Join(root, "two", "adminlist.txt"))
	if err := pm.Start(); err == nil || !strings.Contains(err.Error(), "admin") {
		t.Fatalf("must refuse startup when admins cannot be read: %v", err)
	}
}

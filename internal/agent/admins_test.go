package agent

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistentAdminsAcrossSaveDirectories(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	cfg := AgentConfig{ValheimDir: root, PersistentAdmins: []string{"Steam_123", "Steam_456", "Steam_123"}}
	for _, dir := range []string{"first", "second", ""} {
		path := filepath.Join(root, dir, "adminlist.txt")
		if dir == "" {
			path = filepath.Join(root, ".config/unity3d/IronGate/Valheim/adminlist.txt")
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		original := "// keep this comment\nSteam_existing\nSteam_123"
		if err := os.WriteFile(path, []byte(original), 0600); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if err := cfg.ensurePersistentAdmins(dir); err != nil {
				t.Fatal(err)
			}
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != original+"\nSteam_456\n" {
			t.Fatalf("unexpected list %q: %v", data, err)
		}
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0600 {
			t.Fatal("changed existing permission file mode")
		}
		// Replacing a save folder's admin file must be repaired at the next start.
		if err := os.WriteFile(path, []byte("// mirrored list\nSteam_other\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := cfg.ensurePersistentAdmins(dir); err != nil {
			t.Fatal(err)
		}
		data, _ = os.ReadFile(path)
		if string(data) != "// mirrored list\nSteam_other\nSteam_123\nSteam_456\n" {
			t.Fatalf("missing required entries after replacement: %q", data)
		}
	}
}

func TestPersistentAdminsSettingsAPI(t *testing.T) {
	root := t.TempDir()
	saveDir := filepath.Join(root, "saves")
	cfg := AgentConfig{ValheimDir: root, StartScript: "start.sh", PersistentAdmins: []string{"Steam_required"}}
	script := "exec ./valheim_server.x86_64 -savedir " + saveDir + "\n"
	if err := os.WriteFile(cfg.ResolvedStartScript(), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	h := NewHandlers(cfg, "", nil, nil, "test")
	for _, body := range []string{`{"admins":["Steam_other"]}`, `{"admins":[]}`} {
		w := httptest.NewRecorder()
		h.HandleSettingsUpdate(w, httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(body)))
		if w.Code != http.StatusOK {
			t.Fatalf("settings update: %d %s", w.Code, w.Body.String())
		}
		data, _ := os.ReadFile(filepath.Join(saveDir, "adminlist.txt"))
		if !strings.Contains(string(data), "\nSteam_required\n") {
			t.Fatalf("settings removed required admin: %q", data)
		}
	}
	// A save-directory settings change must provision the new permission file.
	newDir := filepath.Join(root, "new-saves")
	w := httptest.NewRecorder()
	h.HandleSettingsUpdate(w, httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(`{"savedir":"`+newDir+`"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("save directory change: %s", w.Body.String())
	}
	data, _ := os.ReadFile(filepath.Join(newDir, "adminlist.txt"))
	if string(data) != "Steam_required\n" {
		t.Fatalf("new directory missing admin: %q", data)
	}
}

func TestPersistentAdminsFailureAndDisabled(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := AgentConfig{PersistentAdmins: []string{"Steam_required"}}
	if err := cfg.ensurePersistentAdmins(blocked); err == nil {
		t.Fatal("expected failure for inaccessible admin file")
	}
	cfg.PersistentAdmins = []string{"Steam_123\nSteam_456"}
	if err := cfg.ensurePersistentAdmins(root); err == nil {
		t.Fatal("accepted multiline admin entry")
	}
	cfg.PersistentAdmins = nil
	if err := cfg.ensurePersistentAdmins(blocked); err != nil {
		t.Fatalf("disabled feature changed behavior: %v", err)
	}
}

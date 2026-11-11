package cmd

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mmcli/internal/config"
	"mmcli/internal/thunderstore"
)

func exportZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(f, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func importTestPaths(t *testing.T) (config.Paths, config.Config) {
	t.Helper()
	dir := t.TempDir()
	paths := config.Paths{ConfigDir: dir, ConfigFile: filepath.Join(dir, "config.json"), RegistryFile: filepath.Join(dir, "registry.json"), ProfilesDir: filepath.Join(dir, "profiles"), CacheDir: filepath.Join(dir, "cache"), ValheimDir: filepath.Join(dir, "game")}
	cfg := config.Config{ActiveProfile: "original", Initialized: true}
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}
	return paths, cfg
}

func TestFileAndCodeImportResults(t *testing.T) {
	for _, source := range []string{"file", "code"} {
		for _, scenario := range []string{"valid", "missing", "mixed"} {
			t.Run(source+"/"+scenario, func(t *testing.T) {
				paths, cfg := importTestPaths(t)
				before, err := os.ReadFile(paths.ConfigFile)
				if err != nil {
					t.Fatal(err)
				}
				manifest := "profileName: Import Test\nmods:\n"
				if scenario != "missing" {
					manifest += "- name: Author-Good\n  versionNumber:\n    major: 1\n    minor: 0\n    patch: 0\n  enabled: false\n"
				}
				if scenario != "valid" {
					manifest += "- name: Author-Missing\n  versionNumber:\n    major: 999\n    minor: 0\n    patch: 0\n  enabled: true\n"
				}
				export := exportZip(t, map[string]string{"export.r2x": manifest, "config/nested/test.cfg": "custom"})
				modZip := exportZip(t, map[string]string{"plugins/mod.dll": "pinned DLL"})
				original := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = original })
				http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
					var body []byte
					status := 200
					switch r.URL.Path {
					case "/api/experimental/legacyprofile/get/test-code/":
						body = []byte("#r2modman" + base64.StdEncoding.EncodeToString(export))
					case "/api/experimental/package/Author/Good/":
						body = []byte(`{"namespace":"Author","name":"Good","full_name":"Author-Good","latest":{"version_number":"1.0.0"}}`)
					case "/api/experimental/package/Author/Good/1.0.0/":
						body = []byte(`{"version_number":"1.0.0","download_url":"https://test.invalid/good.zip","dependencies":[]}`)
					case "/good.zip":
						body = modZip
					default:
						status = 404
					}
					return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: r}, nil
				})
				if source == "file" {
					file := filepath.Join(paths.ConfigDir, "export.r2z")
					if err := os.WriteFile(file, export, 0644); err != nil {
						t.Fatal(err)
					}
					err = importProfileFile(paths, cfg, file, "")
				} else {
					err = importProfileCode(paths, cfg, "test-code")
				}
				if scenario == "valid" && err != nil {
					t.Fatal(err)
				}
				if scenario != "valid" && (err == nil || !strings.Contains(err.Error(), "Author-Missing requested version 999.0.0") || !strings.Contains(err.Error(), "partial profile retained")) {
					t.Fatalf("expected failed import with package, version and partial profile, got %v", err)
				}
				reg, err := config.LoadRegistry(paths)
				if err != nil {
					t.Fatal(err)
				}
				wantCount := 1
				if scenario == "missing" {
					wantCount = 0
				}
				if got := len(reg.Profiles["Import Test"]); got != wantCount {
					t.Fatalf("installed count=%d want %d", got, wantCount)
				}
				if wantCount == 1 {
					mod, _ := reg.GetMod("Import Test", "Author-Good")
					if mod.Version != "1.0.0" || !mod.Disabled {
						t.Fatalf("incorrect mod state: %+v", mod)
					}
					if data, err := os.ReadFile(filepath.Join(paths.ProfilePluginsDir("Import Test"), "Author-Good/mod.dll.old")); err != nil || string(data) != "pinned DLL" {
						t.Fatalf("DLL=%q, error=%v", data, err)
					}
				}
				if data, err := os.ReadFile(filepath.Join(paths.ProfileConfigDir("Import Test"), "nested/test.cfg")); err != nil || string(data) != "custom" {
					t.Fatalf("config=%q, error=%v", data, err)
				}
				after, err := os.ReadFile(paths.ConfigFile)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("active config changed: %v", err)
				}
			})
		}
	}
}

func TestImportRejectsInvalidPathsBeforeCreatingProfile(t *testing.T) {
	for _, name := range []string{"config/../../../escape", "BepInEx/config/../../escape", "config//absolute", "config/C:/absolute", `config\..\..\escape`, `config\C:\absolute`} {
		t.Run(name, func(t *testing.T) {
			paths, cfg := importTestPaths(t)
			err := importProfileExport(paths, cfg, "safe", nil, exportZip(t, map[string]string{name: "marker", "config/valid.cfg": "valid"}), "test")
			if err == nil {
				t.Fatal("accepted invalid config path")
			}
			if _, err := os.Stat(paths.ProfileDir("safe")); !os.IsNotExist(err) {
				t.Fatal("profile created before path validation")
			}
		})
	}
	paths, cfg := importTestPaths(t)
	for _, name := range []string{"../outside", "/absolute", `..\outside`, `C:\outside`} {
		if err := importProfileExport(paths, cfg, name, []thunderstore.ProfileMod{}, nil, "test"); err == nil {
			t.Fatalf("accepted profile name %q", name)
		}
	}
}

func TestExtractProfileConfigsRejectsSymlinkEscapes(t *testing.T) {
	for _, kind := range []string{"file", "directory", "config root", "profile root"} {
		t.Run(kind, func(t *testing.T) {
			paths, _ := importTestPaths(t)
			configDir := paths.ProfileConfigDir("safe")
			if err := os.MkdirAll(configDir, 0755); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			marker := filepath.Join(outside, "marker")
			if err := os.WriteFile(marker, []byte("original"), 0644); err != nil {
				t.Fatal(err)
			}
			entry := "config/link"
			target, link := marker, filepath.Join(configDir, "link")
			switch kind {
			case "directory":
				target = outside
				entry += "/marker"
			case "config root":
				target = outside
				link = configDir
				entry = "config/marker"
				if err := os.RemoveAll(link); err != nil {
					t.Fatal(err)
				}
			case "profile root":
				target = outside
				link = paths.ProfileDir("safe")
				if err := os.RemoveAll(link); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := extractProfileConfigs(paths, "safe", exportZip(t, map[string]string{entry: "changed"}), "test"); err == nil {
				t.Fatal("accepted symlink escape")
			}
			if data, err := os.ReadFile(marker); err != nil || string(data) != "original" {
				t.Fatalf("outside marker changed: %q %v", data, err)
			}
		})
	}
}

func TestExtractProfileConfigsReportsWriteFailure(t *testing.T) {
	paths, _ := importTestPaths(t)
	if err := os.MkdirAll(filepath.Join(paths.ProfileConfigDir("safe"), "blocked.cfg"), 0755); err != nil {
		t.Fatal(err)
	}
	err := extractProfileConfigs(paths, "safe", exportZip(t, map[string]string{"config/blocked.cfg": "data"}), "test")
	if err == nil || !strings.Contains(err.Error(), "blocked.cfg") {
		t.Fatalf("expected config write error, got %v", err)
	}
}

func TestExtractProfileConfigsAcceptsWindowsSeparators(t *testing.T) {
	paths, _ := importTestPaths(t)
	if err := os.MkdirAll(paths.ProfileConfigDir("safe"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := extractProfileConfigs(paths, "safe", exportZip(t, map[string]string{`BepInEx\config\nested\valid.cfg`: "data"}), "test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(paths.ProfileConfigDir("safe"), "nested/valid.cfg"))
	if err != nil || string(data) != "data" {
		t.Fatal(fmt.Sprintf("config=%q error=%v", data, err))
	}
}

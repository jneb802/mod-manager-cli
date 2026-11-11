package installer

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mmcli/internal/config"
)

type versionTransport func(*http.Request) (*http.Response, error)

func (f versionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func versionFixture(t *testing.T) (config.Paths, config.Config, *config.Registry) {
	t.Helper()
	dir := t.TempDir()
	paths := config.Paths{ProfilesDir: filepath.Join(dir, "profiles"), CacheDir: filepath.Join(dir, "cache"), ValheimDir: filepath.Join(dir, "game")}
	cfg := config.Config{ActiveProfile: "test"}
	reg := config.NewRegistry()
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = versionTransport(func(r *http.Request) (*http.Response, error) {
		status := 200
		var body []byte
		switch r.URL.Path {
		case "/api/experimental/package/Author/Mod/":
			body = []byte(`{"namespace":"Author","name":"Mod","full_name":"Author-Mod","latest":{"version_number":"2.0.0"}}`)
		case "/api/experimental/package/Author/Mod/1.0.0/", "/api/experimental/package/Author/Mod/2.0.0/", "/api/experimental/package/Author/Mod/3.0.0/":
			parts := strings.Split(r.URL.Path, "/")
			v := parts[len(parts)-2]
			body = []byte(fmt.Sprintf(`{"version_number":%q,"download_url":"https://test.invalid/download/%s","dependencies":["Author-Shared-1.0.0"]}`, v, v))
		case "/download/1.0.0", "/download/2.0.0":
			v := strings.TrimPrefix(r.URL.Path, "/download/")
			var buffer bytes.Buffer
			writer := zip.NewWriter(&buffer)
			entries := map[string]string{"plugins/mod.dll": v, "patchers/patch.dll": v, "monomod/mod.mm.dll": v, "core/core.dll": v, "config/custom.cfg": "packaged default", "config/added.cfg": "new default"}
			if v == "1.0.0" {
				entries["plugins/obsolete.dll"] = "old"
			}
			for name, content := range entries {
				w, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.WriteString(w, content); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			body = buffer.Bytes()
		case "/download/3.0.0":
			body = []byte("broken zip")
		default:
			status = 404
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: r}, nil
	})
	reg.SetMod("test", config.ModEntry{Owner: "Author", Name: "Shared", Version: "1.0.0", IsDependency: true})
	if err := InstallVersion(paths, cfg, &reg, "Author-Mod", "client", "1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.ProfileConfigDir("test"), "custom.cfg"), []byte("user setting"), 0644); err != nil {
		t.Fatal(err)
	}
	return paths, cfg, &reg
}

func TestInstallVersionReplacesUpgradeAndDowngrade(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint("disabled=", disabled), func(t *testing.T) {
			paths, cfg, reg := versionFixture(t)
			if disabled {
				if err := Disable(paths, cfg, reg, "Author-Mod"); err != nil {
					t.Fatal(err)
				}
			}
			original, _ := reg.GetMod("test", "Author-Mod")
			original.Anticheat = "adminonly"
			original.GUID = "author.mod"
			reg.SetMod("test", original)
			if err := Install(paths, cfg, reg, "Author-Mod", "both"); err != nil {
				t.Fatal(err)
			}
			unchanged, _ := reg.GetMod("test", "Author-Mod")
			if !reflect.DeepEqual(original, unchanged) {
				t.Fatal("install without a version changed an installed mod")
			}
			for _, v := range []string{"2.0.0", "2.0.0", "1.0.0"} {
				if err := InstallVersion(paths, cfg, reg, "Author-Mod", "both", v); err != nil {
					t.Fatal(err)
				}
				mod, _ := reg.GetMod("test", "Author-Mod")
				if mod.Version != v || mod.Target != "client" || mod.Disabled != disabled || mod.GUID != original.GUID || mod.Anticheat != original.Anticheat {
					t.Fatalf("settings/version changed unexpectedly: %+v", mod)
				}
				if !reflect.DeepEqual(mod.Dependencies, []string{"Author-Shared"}) {
					t.Fatalf("dependencies = %v", mod.Dependencies)
				}
				for _, file := range mod.Files {
					if strings.HasSuffix(file, ".dll") && disabled {
						file += ".old"
					}
					if _, err := os.Stat(file); err != nil {
						t.Fatal(err)
					}
				}
				dll := filepath.Join(paths.ProfilePluginsDir("test"), "Author-Mod", "mod.dll")
				if disabled {
					dll += ".old"
				}
				assertFileContent(t, dll, v)
				assertFileContent(t, filepath.Join(paths.ProfileConfigDir("test"), "custom.cfg"), "user setting")
				if v == "2.0.0" {
					for _, suffix := range []string{".dll", ".dll.old"} {
						if _, err := os.Stat(filepath.Join(paths.ProfilePluginsDir("test"), "Author-Mod", "obsolete"+suffix)); !os.IsNotExist(err) {
							t.Fatalf("obsolete file retained: %v", err)
						}
					}
				}
			}
			if err := Update(paths, cfg, reg, "Mod"); err != nil {
				t.Fatal(err)
			}
			mod, _ := reg.GetMod("test", "Author-Mod")
			if mod.Version != "2.0.0" {
				t.Fatalf("update version = %s", mod.Version)
			}
		})
	}
}

func TestInstallVersionFailureKeepsInstalledMod(t *testing.T) {
	for _, mode := range []string{"missing version", "broken archive", "publish failure"} {
		t.Run(mode, func(t *testing.T) {
			paths, cfg, reg := versionFixture(t)
			before, _ := reg.GetMod("test", "Author-Mod")
			v := "9.0.0"
			if mode == "broken archive" {
				v = "3.0.0"
			}
			if mode == "publish failure" {
				v = "2.0.0"
				// Fail after the plugin directory has been replaced.
				if err := os.RemoveAll(paths.ProfilePatchersDir("test")); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(paths.ProfilePatchersDir("test"), []byte("blocked"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			if err := InstallVersion(paths, cfg, reg, "Author-Mod", "both", v); err == nil {
				t.Fatal("expected failure")
			}
			after, _ := reg.GetMod("test", "Author-Mod")
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("registry changed: %+v", after)
			}
			assertFileContent(t, filepath.Join(paths.ProfilePluginsDir("test"), "Author-Mod", "mod.dll"), "1.0.0")
			assertFileContent(t, filepath.Join(paths.ProfileConfigDir("test"), "custom.cfg"), "user setting")
		})
	}
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}

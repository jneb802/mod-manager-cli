package cmd

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"mmcli/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProfileImportInstallsManifestVersion(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)

	paths, err := config.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(paths.ConfigDir, 0755); err != nil {
		t.Fatal(err)
	}
	valheimDir := filepath.Join(homeDir, "Valheim")
	cfg := config.Config{
		ActiveProfile: "default",
		ValheimPath:   valheimDir,
		Initialized:   true,
	}
	if err := config.Save(paths, cfg); err != nil {
		t.Fatal(err)
	}

	var modZip bytes.Buffer
	zipWriter := zip.NewWriter(&modZip)
	fileWriter, err := zipWriter.Create("plugins/dependency.dll")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileWriter.Write([]byte("test DLL")); err != nil {
		t.Fatal(err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		var statusCode int
		var body []byte
		contentType := "application/json"
		switch req.URL.Path {
		case "/api/experimental/package/PackAuthor/TestPack/":
			statusCode = http.StatusOK
			body = []byte(`{"namespace":"PackAuthor","name":"TestPack","full_name":"PackAuthor-TestPack","latest":{"version_number":"1.0.0","dependencies":["ModAuthor-Root-1.0.0"]}}`)
		case "/api/experimental/package/ModAuthor/Root/":
			statusCode = http.StatusOK
			body = []byte(`{"namespace":"ModAuthor","name":"Root","full_name":"ModAuthor-Root","latest":{"version_number":"2.0.0","download_url":"https://thunderstore.io/download/root-latest.zip","dependencies":[]}}`)
		case "/api/experimental/package/ModAuthor/Root/1.0.0/":
			statusCode = http.StatusOK
			body = []byte(`{"version_number":"1.0.0","download_url":"https://thunderstore.io/download/root-1.0.0.zip","dependencies":["ModAuthor-Dependency-1.0.0"]}`)
		case "/api/experimental/package/ModAuthor/Dependency/":
			statusCode = http.StatusOK
			body = []byte(`{"namespace":"ModAuthor","name":"Dependency","full_name":"ModAuthor-Dependency","latest":{"version_number":"2.0.0","download_url":"https://thunderstore.io/download/latest.zip","dependencies":[]}}`)
		case "/api/experimental/package/ModAuthor/Dependency/1.0.0/":
			statusCode = http.StatusOK
			body = []byte(`{"version_number":"1.0.0","download_url":"https://thunderstore.io/download/1.0.0.zip","dependencies":[]}`)
		case "/download/root-latest.zip", "/download/root-1.0.0.zip", "/download/latest.zip", "/download/1.0.0.zip":
			statusCode = http.StatusOK
			body = modZip.Bytes()
			contentType = "application/zip"
		default:
			return nil, fmt.Errorf("unexpected request: %s", req.URL.String())
		}
		return &http.Response{
			StatusCode: statusCode,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     http.Header{"Content-Type": []string{contentType}},
			Request:    req,
		}, nil
	})

	if err := profileImportCmd.RunE(profileImportCmd, []string{"imported", "PackAuthor-TestPack"}); err != nil {
		t.Fatal(err)
	}

	registry, err := config.LoadRegistry(paths)
	if err != nil {
		t.Fatal(err)
	}
	rootMod, ok := registry.GetMod("imported", "ModAuthor-Root")
	if !ok {
		t.Fatal("imported root mod is missing from the registry")
	}
	if rootMod.Version != "1.0.0" {
		t.Fatalf("imported root mod version = %q, want manifest version %q", rootMod.Version, "1.0.0")
	}
	mod, ok := registry.GetMod("imported", "ModAuthor-Dependency")
	if !ok {
		t.Fatal("imported dependency is missing from the registry")
	}
	if mod.Version != "1.0.0" {
		t.Fatalf("imported dependency version = %q, want manifest version %q", mod.Version, "1.0.0")
	}
}

func TestExtractProfileConfigs(t *testing.T) {
	tmp := t.TempDir()
	paths := config.Paths{
		ConfigDir:   tmp,
		ProfilesDir: filepath.Join(tmp, "profiles"),
	}
	profileName := "p"
	if err := os.MkdirAll(paths.ProfileConfigDir(profileName), 0755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, body string) {
		t.Helper()
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	// r2modman layout
	write("BepInEx/config/author.mod.cfg", "A")
	write("BepInEx/config/nested/dir/file.json", "B")
	// older / alternate layout
	write("config/legacy.cfg", "C")
	// must be ignored
	write("export.r2x", "profileName: p\nmods: []\n")
	write("BepInEx/plugins/author-mod/mod.dll", "binary")
	write("BepInEx/core/BepInEx.Core.dll", "binary")
	write("changelog.txt", "x")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	extractProfileConfigs(paths, profileName, buf.Bytes(), "export file")

	configDir := paths.ProfileConfigDir(profileName)
	cases := map[string]string{
		filepath.Join(configDir, "author.mod.cfg"):       "A",
		filepath.Join(configDir, "nested/dir/file.json"): "B",
		filepath.Join(configDir, "legacy.cfg"):           "C",
	}
	for path, want := range cases {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("missing extracted file %s: %v", path, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
	for _, unwanted := range []string{
		filepath.Join(configDir, "export.r2x"),
		filepath.Join(configDir, "plugins"),
		filepath.Join(configDir, "core"),
		filepath.Join(configDir, "changelog.txt"),
	} {
		if _, err := os.Stat(unwanted); !os.IsNotExist(err) {
			t.Errorf("unexpected file extracted: %s", unwanted)
		}
	}
}

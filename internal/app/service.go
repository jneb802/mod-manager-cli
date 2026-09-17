package app

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"mmcli/internal/bepinex"
	"mmcli/internal/config"
	"mmcli/internal/installer"
	"mmcli/internal/platform"
	"mmcli/internal/profile"
	"mmcli/internal/thunderstore"
)

// Service exposes UI-friendly operations over the existing mmcli packages.
type Service struct{}

type State struct {
	Initialized   bool             `json:"initialized"`
	ConfigDir     string           `json:"configDir"`
	ConfigFile    string           `json:"configFile"`
	ValheimPath   string           `json:"valheimPath"`
	DetectedPath  string           `json:"detectedPath"`
	ActiveProfile string           `json:"activeProfile"`
	Profiles      []ProfileSummary `json:"profiles"`
}

type ProfileSummary struct {
	Name   string `json:"name"`
	Mods   int    `json:"mods"`
	Active bool   `json:"active"`
}

type InitRequest struct {
	ValheimPath string `json:"valheimPath"`
	Force       bool   `json:"force"`
}

func NewService() *Service {
	return &Service{}
}

func (s *Service) State() (State, error) {
	return loadState()
}

func (s *Service) DetectValheimPath() (string, error) {
	return config.DetectValheimPath()
}

func (s *Service) Initialize(req InitRequest) (State, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return State{}, err
	}

	if cfg, err := config.Load(paths); err == nil && cfg.Initialized && !req.Force {
		return loadState()
	}

	valheimPath := strings.TrimSpace(req.ValheimPath)
	if valheimPath == "" {
		valheimPath, err = config.DetectValheimPath()
		if err != nil {
			return State{}, fmt.Errorf("Valheim path is required because automatic detection failed: %w", err)
		}
	}
	if info, err := os.Stat(valheimPath); err != nil {
		return State{}, fmt.Errorf("Valheim path does not exist: %s", valheimPath)
	} else if !info.IsDir() {
		return State{}, fmt.Errorf("Valheim path is not a directory: %s", valheimPath)
	}

	paths.ValheimDir = valheimPath
	for _, dir := range []string{paths.ConfigDir, paths.CacheDir, paths.ProfilesDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return State{}, fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}

	version, downloadURL, err := bepinex.LatestVersion()
	if err != nil {
		return State{}, err
	}
	zipPath, err := bepinex.Download(paths, downloadURL, version)
	if err != nil {
		return State{}, err
	}

	if err := profile.Create(paths, "default"); err != nil && !strings.Contains(err.Error(), "already exists") {
		return State{}, err
	}
	if err := bepinex.Install(paths, zipPath); err != nil {
		return State{}, err
	}
	if runtime.GOOS == "darwin" {
		if err := bepinex.PatchRunScript(paths); err != nil {
			return State{}, err
		}
		if err := bepinex.MakeExecutable(paths); err != nil {
			return State{}, err
		}
		bepinex.RemoveQuarantine(paths)
	}
	if err := profile.Activate(paths, "default"); err != nil {
		return State{}, err
	}

	cfg := config.Config{
		ActiveProfile: "default",
		ValheimPath:   valheimPath,
		Initialized:   true,
	}
	if err := config.Save(paths, cfg); err != nil {
		return State{}, err
	}
	reg := config.NewRegistry()
	reg.EnsureProfile("default")
	if err := config.SaveRegistry(paths, reg); err != nil {
		return State{}, err
	}

	return loadState()
}

func (s *Service) CreateProfile(name string) (State, error) {
	paths, cfg, reg, err := loadConfigWithRegistry()
	if err != nil {
		return State{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return State{}, fmt.Errorf("profile name is required")
	}
	if err := profile.Create(paths, name); err != nil {
		return State{}, err
	}
	reg.EnsureProfile(name)
	if err := config.SaveRegistry(paths, *reg); err != nil {
		return State{}, err
	}
	return stateFromLoaded(paths, cfg, reg)
}

func (s *Service) SwitchProfile(name string) (State, error) {
	paths, cfg, reg, err := loadConfigWithRegistry()
	if err != nil {
		return State{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return State{}, fmt.Errorf("profile name is required")
	}
	if cfg.ActiveProfile != name {
		if err := profile.Switch(paths, &cfg, name); err != nil {
			return State{}, err
		}
		if err := config.Save(paths, cfg); err != nil {
			return State{}, err
		}
	}
	return stateFromLoaded(paths, cfg, reg)
}

func (s *Service) DeleteProfile(name string) (State, error) {
	paths, cfg, reg, err := loadConfigWithRegistry()
	if err != nil {
		return State{}, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return State{}, fmt.Errorf("profile name is required")
	}
	if err := profile.Delete(paths, cfg, name); err != nil {
		return State{}, err
	}
	delete(reg.Profiles, name)
	delete(reg.Settings, name)
	if err := config.SaveRegistry(paths, *reg); err != nil {
		return State{}, err
	}
	return stateFromLoaded(paths, cfg, reg)
}

func (s *Service) OpenActiveProfile() error {
	paths, cfg, err := loadConfig()
	if err != nil {
		return err
	}
	return platform.OpenPath(paths.ProfileDir(cfg.ActiveProfile))
}

func (s *Service) ImportProfileCode(code string) (State, error) {
	paths, cfg, reg, err := loadConfigWithRegistry()
	if err != nil {
		return State{}, err
	}
	code = strings.TrimSpace(code)
	if !thunderstore.IsProfileCode(code) {
		return State{}, fmt.Errorf("enter a valid Thunderstore or r2modman profile code")
	}

	profileName, mods, zipData, err := thunderstore.FetchProfileCode(code)
	if err != nil {
		return State{}, err
	}
	filtered := filterProfileMods(mods)
	if err := profile.Create(paths, profileName); err != nil {
		return State{}, err
	}
	cleanupCfg := cfg
	importComplete := false
	defer func() {
		if !importComplete {
			_ = profile.Delete(paths, cleanupCfg, profileName)
			delete(reg.Profiles, profileName)
			delete(reg.Settings, profileName)
		}
	}()
	reg.EnsureProfile(profileName)

	origProfile := cfg.ActiveProfile
	cfg.ActiveProfile = profileName
	applyEnabled := func(m thunderstore.ProfileMod) {
		if m.Enabled {
			return
		}
		_ = installer.Toggle(paths, cfg, reg, m.Name)
	}

	for _, m := range filtered {
		if err := installer.InstallVersion(paths, cfg, reg, m.Name, "both", m.Version); err != nil {
			return State{}, fmt.Errorf("failed to install %s: %w", m.Name, err)
		}
		applyEnabled(m)
	}
	for attempt := 0; attempt < 3; attempt++ {
		mismatches := 0
		for _, m := range filtered {
			if m.Version == "" {
				continue
			}
			mod, ok := reg.GetMod(profileName, m.Name)
			if !ok || mod.Version == m.Version {
				continue
			}
			mismatches++
			if err := installer.Remove(paths, cfg, reg, m.Name); err != nil {
				return State{}, fmt.Errorf("failed to remove %s for version pinning: %w", m.Name, err)
			}
			if err := installer.InstallVersion(paths, cfg, reg, m.Name, "both", m.Version); err != nil {
				return State{}, fmt.Errorf("failed to pin %s to %s: %w", m.Name, m.Version, err)
			}
			applyEnabled(m)
		}
		if mismatches == 0 {
			break
		}
	}

	extractProfileConfigs(paths, profileName, zipData)
	cfg.ActiveProfile = origProfile
	if err := config.Save(paths, cfg); err != nil {
		return State{}, err
	}
	if err := config.SaveRegistry(paths, *reg); err != nil {
		return State{}, err
	}
	importComplete = true
	return stateFromLoaded(paths, cfg, reg)
}

func filterProfileMods(mods []thunderstore.ProfileMod) []thunderstore.ProfileMod {
	filtered := make([]thunderstore.ProfileMod, 0, len(mods))
	for _, m := range mods {
		parts := strings.SplitN(m.Name, "-", 2)
		if len(parts) == 2 && (parts[1] == "BepInExPack_Valheim" || parts[1] == "BepInEx_pack") {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func loadState() (State, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return State{}, err
	}
	state := State{
		ConfigDir:  paths.ConfigDir,
		ConfigFile: paths.ConfigFile,
	}
	if detected, err := config.DetectValheimPath(); err == nil {
		state.DetectedPath = detected
	}

	cfg, err := config.Load(paths)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return State{}, err
	}
	if !cfg.Initialized {
		return state, nil
	}
	paths.ValheimDir = cfg.ValheimPath
	reg, err := config.LoadRegistry(paths)
	if err != nil {
		return State{}, err
	}
	return stateFromLoaded(paths, cfg, &reg)
}

func loadConfig() (config.Paths, config.Config, error) {
	paths, err := config.DefaultPaths()
	if err != nil {
		return config.Paths{}, config.Config{}, err
	}
	cfg, err := config.EnsureInitialized(paths)
	if err != nil {
		return config.Paths{}, config.Config{}, err
	}
	paths.ValheimDir = cfg.ValheimPath
	return paths, cfg, nil
}

func loadConfigWithRegistry() (config.Paths, config.Config, *config.Registry, error) {
	paths, cfg, err := loadConfig()
	if err != nil {
		return config.Paths{}, config.Config{}, nil, err
	}
	reg, err := config.LoadRegistry(paths)
	if err != nil {
		return config.Paths{}, config.Config{}, nil, err
	}
	cfgDirty, regDirty := config.MigrateProfileSettings(&cfg, &reg, cfg.ActiveProfile)
	if cfgDirty {
		if err := config.Save(paths, cfg); err != nil {
			return config.Paths{}, config.Config{}, nil, err
		}
	}
	if regDirty {
		if err := config.SaveRegistry(paths, reg); err != nil {
			return config.Paths{}, config.Config{}, nil, err
		}
	}
	return paths, cfg, &reg, nil
}

func stateFromLoaded(paths config.Paths, cfg config.Config, reg *config.Registry) (State, error) {
	names, err := profile.List(paths)
	if err != nil {
		return State{}, err
	}
	sort.Strings(names)
	profiles := make([]ProfileSummary, 0, len(names))
	for _, name := range names {
		profiles = append(profiles, ProfileSummary{
			Name:   name,
			Mods:   len(reg.ListMods(name)),
			Active: name == cfg.ActiveProfile,
		})
	}
	state := State{
		Initialized:   true,
		ConfigDir:     paths.ConfigDir,
		ConfigFile:    paths.ConfigFile,
		ValheimPath:   cfg.ValheimPath,
		ActiveProfile: cfg.ActiveProfile,
		Profiles:      profiles,
	}
	if detected, err := config.DetectValheimPath(); err == nil {
		state.DetectedPath = detected
	}
	return state, nil
}

func extractProfileConfigs(paths config.Paths, profileName string, zipData []byte) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return
	}
	configDir := paths.ProfileConfigDir(profileName)
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		var rel string
		switch {
		case strings.HasPrefix(name, "BepInEx/config/"):
			rel = name[len("BepInEx/config/"):]
		case strings.HasPrefix(name, "config/"):
			rel = name[len("config/"):]
		default:
			continue
		}
		if rel == "" {
			continue
		}
		dest := filepath.Join(configDir, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			continue
		}
		_, _ = io.Copy(out, rc)
		out.Close()
		rc.Close()
	}
}

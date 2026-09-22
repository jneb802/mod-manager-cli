package thunderstore

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

const baseURL = "https://thunderstore.io"

// GetPackageVersion fetches a specific version of a package. Registries are
// tried in order and the first one carrying the version wins, so a build
// published only on Hexium still resolves.
func GetPackageVersion(owner, name, version string) (*Package, error) {
	var failures []string
	for _, r := range registries {
		pkg, err := getPackageVersionFrom(r, owner, name, version)
		if err == nil {
			return pkg, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", r.Name, err))
	}
	return nil, fmt.Errorf("version %s of %s-%s not found (%s)", version, owner, name, strings.Join(failures, "; "))
}

// getPackageVersionFrom fetches a pinned version from a single registry.
func getPackageVersionFrom(r Registry, owner, name, version string) (*Package, error) {
	url := fmt.Sprintf("%s%s/%s/%s/", r.experimentalAPI(), owner, name, version)
	resp, err := getWithRetry(url)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var ev ExperimentalVersion
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		return nil, fmt.Errorf("failed to decode version response: %w", err)
	}

	pkg := &Package{
		Owner:    owner,
		Name:     name,
		FullName: fmt.Sprintf("%s-%s", owner, name),
		Source:   r.Name,
		Versions: []Version{
			{
				Name:          name,
				FullName:      fmt.Sprintf("%s-%s-%s", owner, name, ev.VersionNumber),
				VersionNumber: ev.VersionNumber,
				DownloadURL:   ev.DownloadURL,
				Dependencies:  ev.Dependencies,
				Description:   ev.Description,
				FileSize:      ev.FileSize,
			},
		},
	}
	return pkg, nil
}

// GetPackage fetches a package's latest version. Every registry is queried and
// the highest version wins, so a mod whose newest build ships on Hexium is not
// reported as up to date because Thunderstore still lists an older one. Ties go
// to the earlier registry, keeping Thunderstore the source for packages both
// registries serve at the same version.
func GetPackage(owner, name string) (*Package, error) {
	type lookup struct {
		pkg *Package
		err error
	}

	results := make([]lookup, len(registries))
	var wg sync.WaitGroup
	for i, r := range registries {
		wg.Add(1)
		go func(i int, r Registry) {
			defer wg.Done()
			pkg, err := getPackageFrom(r, owner, name)
			results[i] = lookup{pkg: pkg, err: err}
		}(i, r)
	}
	wg.Wait()

	var best *Package
	var failures []string
	for i, res := range results {
		if res.err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", registries[i].Name, res.err))
			continue
		}
		if best == nil || compareVersions(latestVersionNumber(res.pkg), latestVersionNumber(best)) > 0 {
			best = res.pkg
		}
	}

	if best == nil {
		return nil, fmt.Errorf("package %s-%s not found (%s)", owner, name, strings.Join(failures, "; "))
	}
	return best, nil
}

// getPackageFrom fetches a package's latest version from a single registry.
func getPackageFrom(r Registry, owner, name string) (*Package, error) {
	url := fmt.Sprintf("%s%s/%s/", r.experimentalAPI(), owner, name)
	resp, err := getWithRetry(url)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var expPkg ExperimentalPackage
	if err := json.NewDecoder(resp.Body).Decode(&expPkg); err != nil {
		return nil, fmt.Errorf("failed to decode package response: %w", err)
	}

	// Convert to Package type
	pkg := &Package{
		Owner:    expPkg.Namespace,
		Name:     expPkg.Name,
		FullName: expPkg.FullName,
		Source:   r.Name,
		Versions: []Version{
			{
				Name:          expPkg.Name,
				FullName:      fmt.Sprintf("%s-%s-%s", expPkg.Namespace, expPkg.Name, expPkg.LatestVersion.VersionNumber),
				VersionNumber: expPkg.LatestVersion.VersionNumber,
				DownloadURL:   expPkg.LatestVersion.DownloadURL,
				Dependencies:  expPkg.LatestVersion.Dependencies,
				Description:   expPkg.LatestVersion.Description,
				FileSize:      expPkg.LatestVersion.FileSize,
			},
		},
	}
	return pkg, nil
}

// ResolveDependencies resolves all dependencies for a package recursively.
// Returns packages in topological order (dependencies first).
// Skips BepInExPack_Valheim and already-installed mods.
func ResolveDependencies(pkg *Package, installed map[string]bool) ([]DepRef, error) {
	if len(pkg.Versions) == 0 {
		return nil, fmt.Errorf("package %s has no versions", pkg.FullName)
	}

	var result []DepRef
	visited := make(map[string]bool)
	inStack := make(map[string]bool)

	var dfs func(deps []string) error
	dfs = func(deps []string) error {
		for _, dep := range deps {
			ref := ParseDep(dep)
			fullName := fmt.Sprintf("%s-%s", ref.Owner, ref.Name)

			// Skip BepInExPack
			if ref.Name == "BepInExPack_Valheim" || ref.Name == "BepInEx_pack" {
				continue
			}

			// Skip already installed
			if installed[fullName] {
				continue
			}

			// Skip already visited
			if visited[fullName] {
				continue
			}

			// Cycle detection
			if inStack[fullName] {
				continue // Skip cycles silently
			}

			inStack[fullName] = true

			// Fetch the pinned dependency version so its dependency graph also
			// matches the version required by the package manifest.
			var depPkg *Package
			var err error
			if ref.Version == "" {
				depPkg, err = GetPackage(ref.Owner, ref.Name)
			} else {
				depPkg, err = GetPackageVersion(ref.Owner, ref.Name, ref.Version)
			}
			if err != nil {
				// Non-fatal: some deps may not resolve
				fmt.Printf("  Warning: could not resolve dependency %s: %v\n", fullName, err)
				inStack[fullName] = false
				continue
			}

			if len(depPkg.Versions) > 0 {
				if err := dfs(depPkg.Versions[0].Dependencies); err != nil {
					return err
				}
			}

			inStack[fullName] = false
			visited[fullName] = true
			result = append(result, DepRef{
				Owner:   ref.Owner,
				Name:    ref.Name,
				Version: ref.Version,
			})
		}
		return nil
	}

	if err := dfs(pkg.Versions[0].Dependencies); err != nil {
		return nil, err
	}

	return result, nil
}

// FindPackageByQuery searches for a package by query string.
// Accepts "Owner-Name", "Owner-Name-Version", or a Thunderstore or Hexium
// package URL.
func FindPackageByQuery(query string) (*Package, error) {
	// Try parsing as a registry package URL
	// (https://thunderstore.io/c/valheim/p/Owner/Name/ or
	// https://valheim.hexium.gg/mods/Owner/Name).
	if r, isRegistryURL := registryForURL(query); isRegistryURL {
		owner, name, ok := ownerNameFromURL(r, query)
		if !ok {
			return nil, fmt.Errorf("could not parse %s URL: %s", r.Name, query)
		}
		pkg, err := GetPackage(owner, name)
		if err != nil {
			return nil, fmt.Errorf("could not fetch package from URL: %w", err)
		}
		return pkg, nil
	}

	// Try parsing as Owner-Name-Version first (e.g., "warpalicious-Praetoris-1.1.16")
	ref := ParseDep(query)
	if ref.Owner != "" && ref.Name != "" {
		pkg, err := GetPackage(ref.Owner, ref.Name)
		if err == nil {
			return pkg, nil
		}
	}

	// Try as Owner-Name (e.g., "warpalicious-Praetoris")
	parts := strings.SplitN(query, "-", 2)
	if len(parts) == 2 {
		pkg, err := GetPackage(parts[0], parts[1])
		if err == nil {
			return pkg, nil
		}
	}

	return nil, fmt.Errorf("no package found matching '%s' — use Owner-Name format or a Thunderstore or Hexium URL", query)
}

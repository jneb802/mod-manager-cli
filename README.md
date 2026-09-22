<p align="center">
  <img src="mmcliBanner.png" alt="mmcli banner" />
</p>

# mmcli

A command-line Valheim mod manager for macOS and Linux. Installs mods from [Thunderstore](https://thunderstore.io/c/valheim/), manages profiles, and launches the game with BepInEx.

## Install

### Shell script (recommended)

```
curl -fsSL https://raw.githubusercontent.com/jneb802/mod-manager-cli/main/install.sh | bash
```

The installer uses a user-writable location and does not require a password. It updates an existing writable `mmcli` install, installs to a writable directory already on your `PATH`, or installs to `~/.local/bin`.

If it installs to `~/.local/bin` and your shell cannot find `mmcli`, the installer adds this to your shell profile:

```
export PATH="$HOME/.local/bin:$PATH"
```

Restart your terminal, or run the `source` command printed by the installer.

### Homebrew

```
brew install jneb802/tap/mmcli
```

### Manual download

Download the latest binary for your platform from [Releases](https://github.com/jneb802/mod-manager-cli/releases), then:

```
mkdir -p ~/.local/bin
binary=mmcli-linux-amd64 # or mmcli-darwin-arm64 / mmcli-darwin-amd64
chmod +x "$binary"
mv "$binary" ~/.local/bin/mmcli
```

## Getting Started

```
mmcli init
```

This detects your Valheim install, installs BepInEx, and creates a default profile.

## Interactive TUI

```
mmcli tui
```

A terminal UI for browsing, toggling, installing, updating, and removing mods with keyboard shortcuts.

## Launching the Game

```
mmcli start
```

Launches Valheim with BepInEx loaded and streams logs to the terminal.

## Installing Mods

```
mmcli install RandyKnapp-EpicLoot
```

Dependencies are resolved and installed automatically.

## Managing Mods

```
mmcli list                        # show installed mods
mmcli remove <mod>                # remove a mod and orphaned dependencies
```

## Profiles

Profiles let you maintain separate sets of mods (e.g. one for solo, one for a modded server).

```
mmcli profile create <name>
mmcli profile switch <name>
mmcli profile list
mmcli profile delete <name>
mmcli profile import <url|code>   # import from r2modman/Thunderstore profile code
mmcli profile import <file.r2z>   # import from an r2modman profile export file
mmcli profile open                # open profile folder in Finder
```

### Importing an r2modman profile

A profile can be imported either from a profile code or from an r2modman export
file. The file form is useful when the profile was exported to disk, when it is
larger than the 20MB the profile code service accepts, or when a shared code has
expired.

```
mmcli profile import ./full-valheim.r2z        # keeps the name from the export
mmcli profile import myprofile ./export.r2z    # overrides the profile name
```

Both `.r2z` archives and bare `.r2x` manifests are accepted. Mods are installed
at the versions pinned by the export, mods the export marks as disabled stay
disabled, and config files travel with a `.r2z`.

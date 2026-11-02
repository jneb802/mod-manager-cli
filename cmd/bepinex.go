package cmd

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"

	"mmcli/internal/bepinex"
	"mmcli/internal/config"
	"mmcli/internal/platform"
	"mmcli/internal/profile"
)

var bepinexCmd = &cobra.Command{
	Use:   "bepinex",
	Short: "Manage the BepInEx install",
}

var bepinexUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Install the latest BepInExPack_Valheim over the current install",
	Long: `Download the latest BepInExPack_Valheim from Thunderstore and install it into
the Valheim directory. Mods, profiles, and an existing BepInEx.cfg are kept.
On macOS the game then runs natively on Apple Silicon instead of under Rosetta.
Valheim must be closed.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		paths, cfg, err := loadConfig()
		if err != nil {
			return err
		}

		if platform.IsGameRunning() {
			return fmt.Errorf("Valheim is running. Close it before updating BepInEx")
		}

		profiles, err := profile.List(paths)
		if err != nil {
			return err
		}

		version, err := installBepInEx(paths, profiles)
		if err != nil {
			return err
		}

		// The pack ships its own doorstop_config.ini, which on Windows replaces
		// the one pointing at the active profile. Re-activate to restore it and
		// the profile symlinks on macOS and Linux.
		fmt.Printf("Re-activating profile %s... ", cfg.ActiveProfile)
		if err := profile.Activate(paths, cfg.ActiveProfile); err != nil {
			fmt.Println("\033[31mfailed\033[0m")
			return err
		}
		fmt.Println("\033[32mdone\033[0m")

		cfg.BepInExVersion = version
		if err := config.Save(paths, cfg); err != nil {
			return fmt.Errorf("failed to save config: %w", err)
		}

		fmt.Printf("\033[32mBepInEx updated to v%s\033[0m\n", version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(bepinexCmd)
	bepinexCmd.AddCommand(bepinexUpdateCmd)
}

// installBepInEx downloads the latest BepInExPack_Valheim, installs it into the
// Valheim directory, and applies the macOS launch fixes. On Windows the BepInEx
// tree is written into each of the given profiles. Returns the installed version.
func installBepInEx(paths config.Paths, profiles []string) (string, error) {
	fmt.Print("Fetching latest BepInEx version... ")
	version, downloadURL, err := bepinex.LatestVersion()
	if err != nil {
		fmt.Println("\033[31mfailed\033[0m")
		return "", err
	}
	fmt.Printf("\033[32mv%s\033[0m\n", version)

	fmt.Print("Downloading BepInEx... ")
	zipPath, err := bepinex.Download(paths, downloadURL, version)
	if err != nil {
		fmt.Println("\033[31mfailed\033[0m")
		return "", err
	}
	fmt.Println("\033[32mdone\033[0m")

	fmt.Print("Installing BepInEx... ")
	if err := bepinex.Install(paths, zipPath, profiles); err != nil {
		fmt.Println("\033[31mfailed\033[0m")
		return "", err
	}
	fmt.Println("\033[32mdone\033[0m")

	if runtime.GOOS == "darwin" {
		fmt.Print("Patching run_bepinex.sh for macOS... ")
		if err := bepinex.PatchRunScript(paths); err != nil {
			fmt.Println("\033[31mfailed\033[0m")
			return "", err
		}
		if err := bepinex.MakeExecutable(paths); err != nil {
			fmt.Println("\033[31mfailed\033[0m")
			return "", err
		}
		fmt.Println("\033[32mdone\033[0m")

		fmt.Print("Removing macOS quarantine attributes... ")
		bepinex.RemoveQuarantine(paths)
		fmt.Println("\033[32mdone\033[0m")
	}

	return version, nil
}

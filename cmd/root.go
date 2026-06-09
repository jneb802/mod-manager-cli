package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var verbose bool
var jsonOutput bool

var rootCmd = &cobra.Command{
	Use:   "mmcli",
	Short: "Valheim mod manager for macOS and Linux",
	Long:  "A CLI tool for managing BepInEx mods, profiles, and game launching for Valheim on macOS and Linux.",
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format for scripting and automation")
}

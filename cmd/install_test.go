package cmd

import (
	"strings"
	"testing"
)

func TestInstallRejectsUnsupportedVersionTargets(t *testing.T) {
	for _, server := range []bool{false, true} {
		t.Run(map[bool]string{false: "local path", true: "server"}[server], func(t *testing.T) {
			flags := installCmd.Flags()
			oldVersion, _ := flags.GetString("version")
			oldServer, _ := flags.GetBool("server")
			t.Cleanup(func() {
				flags.Set("version", oldVersion)
				flags.Set("server", map[bool]string{false: "false", true: "true"}[oldServer])
			})
			flags.Set("version", "1.0.0")
			flags.Set("server", map[bool]string{false: "false", true: "true"}[server])
			query := t.TempDir()
			if server {
				query = "Author-Mod"
			}
			err := installCmd.RunE(installCmd, []string{query})
			if err == nil || !strings.Contains(err.Error(), "--version") {
				t.Fatalf("expected an explicit version error before loading config or contacting a server, got %v", err)
			}
		})
	}
}

package bepinex

import (
	"strings"
	"testing"
)

func TestPatchRunScriptUsesSteamBundleName(t *testing.T) {
	content := `#!/bin/sh
executable_name="valheim.x86_64"
exec "$executable_path" $rest_args
`

	content = replaceScriptVar(content, "executable_name", "Valheim.app")
	content = replaceExecWithArchWrapper(content)

	if !strings.Contains(content, `executable_name="Valheim.app"`) {
		t.Fatalf("patched script did not use Valheim.app:\n%s", content)
	}
	if !strings.Contains(content, "arch -x86_64 zsh -c") {
		t.Fatalf("patched script did not include Rosetta wrapper:\n%s", content)
	}
}

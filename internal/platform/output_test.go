package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestAttachProcessOutputWritesToLogFile(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "launch.log")
	cmd := exec.Command("test")

	lf, err := attachProcessOutput(cmd, logPath)
	if err != nil {
		t.Fatalf("attachProcessOutput failed: %v", err)
	}

	if lf == nil {
		t.Fatal("expected log file handle")
	}
	defer lf.Close()

	if cmd.Stdout != lf || cmd.Stderr != lf {
		t.Fatal("expected stdout and stderr to use log file")
	}
}

func TestAttachProcessOutputInheritsTerminal(t *testing.T) {
	cmd := exec.Command("test")

	lf, err := attachProcessOutput(cmd, "")
	if err != nil {
		t.Fatalf("attachProcessOutput failed: %v", err)
	}

	if lf != nil {
		t.Fatal("expected no log file handle")
	}
	if cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
		t.Fatal("expected stdout and stderr to inherit terminal output")
	}
}

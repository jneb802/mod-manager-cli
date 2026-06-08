//go:build darwin

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".config", "mmcli"), nil
}

func DetectValheimPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, "Library", "Application Support", "Steam", "steamapps", "common", "Valheim")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("Valheim not found at %s", path)
	}
	return path, nil
}

func OpenPath(path string) error {
	return exec.Command("open", path).Run()
}

func GameLaunchTarget(workDir string) string {
	return filepath.Join(workDir, "run_bepinex.sh")
}

func IsGameRunning() bool {
	return exec.Command("pgrep", "-x", "Valheim").Run() == nil
}

// StartGameProcess launches Valheim via /bin/bash run_bepinex.sh with a
// dedicated process group so the entire tree can be killed on shutdown.
// When logPath is non-empty, stdout and stderr are redirected to that file
// so game output doesn't corrupt a TUI. Otherwise output is inherited by the
// CLI so launch script failures are visible. The caller must close the returned
// *os.File when the process exits; it may be nil when logPath is empty.
func StartGameProcess(workDir, target, logPath string) (*exec.Cmd, int, *os.File, error) {
	cmd := exec.Command("/bin/bash", target)
	cmd.Dir = workDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	lf, err := attachProcessOutput(cmd, logPath)
	if err != nil {
		return nil, 0, nil, err
	}

	if err := cmd.Start(); err != nil {
		if lf != nil {
			lf.Close()
		}
		return nil, 0, nil, err
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		pgid = cmd.Process.Pid
	}
	return cmd, pgid, lf, nil
}

// GracefulKill sends SIGTERM to the process group.
func GracefulKill(_ *exec.Cmd, pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGTERM)
}

// ForceKill sends SIGKILL to the process group.
func ForceKill(_ *exec.Cmd, pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// NotifySignals registers for SIGINT and SIGTERM.
func NotifySignals(c chan<- os.Signal) {
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
}

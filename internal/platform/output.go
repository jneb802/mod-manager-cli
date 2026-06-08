package platform

import (
	"fmt"
	"os"
	"os/exec"
)

func attachProcessOutput(cmd *exec.Cmd, logPath string) (*os.File, error) {
	if logPath != "" {
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		cmd.Stdout = f
		cmd.Stderr = f
		return f, nil
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return nil, nil
}

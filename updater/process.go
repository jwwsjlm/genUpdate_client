package updater

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func waitForProcessExit(ctx context.Context, processName string, timeout time.Duration, writer io.Writer) error {
	processName = strings.TrimSpace(processName)
	if processName == "" {
		return nil
	}

	startedAt := time.Now()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		running, err := IsProcessRunning(processName)
		if err != nil {
			return err
		}
		if !running {
			writef(writer, "\r目标进程 %s 已退出，继续更新。                 \n", processName)
			return nil
		}
		if timeout > 0 && time.Since(startedAt) >= timeout {
			return fmt.Errorf("process %s is still running after %s", processName, timeout)
		}

		writef(writer, "\r检测到目标进程 %s 正在运行，请关闭后继续，已等待 %s", processName, formatElapsed(time.Since(startedAt)))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func IsProcessRunning(processName string) (bool, error) {
	switch runtime.GOOS {
	case "windows":
		return isProcessRunningWindows(processName)
	case "linux", "darwin":
		return isProcessRunningUnix(processName)
	default:
		return false, fmt.Errorf("process detection is not supported on %s", runtime.GOOS)
	}
}

func isProcessRunningWindows(processName string) (bool, error) {
	out, err := exec.Command("tasklist", "/FI", "IMAGENAME eq "+processName, "/NH").Output()
	if err != nil {
		return false, fmt.Errorf("failed to run tasklist: %w", err)
	}
	return bytes.Contains(bytes.ToLower(out), []byte(strings.ToLower(processName))), nil
}

func isProcessRunningUnix(processName string) (bool, error) {
	err := exec.Command("pgrep", "-x", processName).Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("failed to run pgrep: %w", err)
}

func formatElapsed(d time.Duration) string {
	d = d.Truncate(time.Second)
	if d < time.Minute {
		return d.String()
	}
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%02ds", minutes, seconds)
}

func writef(writer io.Writer, format string, args ...any) {
	if writer != nil {
		_, _ = fmt.Fprintf(writer, format, args...)
	}
}

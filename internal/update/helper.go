package update

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// HelperRequest defines the work executed by the detached upgrade helper.
type HelperRequest struct {
	TargetPath  string
	WaitPID     int
	Version     string
	RestartArgs []string
	Repo        string
	APIBase     string
}

// StartHelper launches a detached copy of the current executable to perform an upgrade.
func StartHelper(req HelperRequest) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating executable: %w", err)
	}
	helperPath, err := copySelfForHelper(exePath)
	if err != nil {
		return err
	}
	encodedArgs, err := encodeHelperArgs(req.RestartArgs)
	if err != nil {
		return fmt.Errorf("encoding restart args: %w", err)
	}
	cmd := exec.Command(
		helperPath,
		"__upgrade-helper",
		"--target-path", req.TargetPath,
		"--wait-pid", strconv.Itoa(req.WaitPID),
		"--version", req.Version,
		"--restart-args", encodedArgs,
		"--repo", req.Repo,
		"--api-base", req.APIBase,
	) //nolint:gosec
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting upgrade helper: %w", err)
	}
	return nil
}

// RunHelper waits for the target process to exit, installs the requested version, and optionally restarts Aviary.
func RunHelper(ctx context.Context, req HelperRequest, stdout, stderr *os.File) error {
	if req.WaitPID > 0 {
		if err := waitForPIDExit(req.WaitPID, 2*time.Minute); err != nil {
			return err
		}
	}
	result, err := Install(ctx, InstallOptions{
		Version:    req.Version,
		TargetPath: req.TargetPath,
		Repo:       req.Repo,
		APIBase:    req.APIBase,
		Client:     &http.Client{Timeout: 45 * time.Second},
	})
	if err != nil {
		return err
	}
	if stdout != nil {
		if result.Noop {
			_, _ = fmt.Fprintf(stdout, "Emulated upgrade to %s completed. No files were changed.\n", result.Version)
		} else {
			_, _ = fmt.Fprintf(stdout, "Aviary upgraded to %s.\n", result.Version)
		}
	}
	if len(req.RestartArgs) > 0 && !result.Noop {
		cmd := exec.Command(req.TargetPath, req.RestartArgs...) //nolint:gosec
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("restarting aviary: %w", err)
		}
	}
	return nil
}

// EncodeRestartArgs serializes restart args for transport through the helper command line.
func EncodeRestartArgs(args []string) (string, error) {
	return encodeHelperArgs(args)
}

// DecodeRestartArgs decodes restart args previously encoded with EncodeRestartArgs.
func DecodeRestartArgs(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decoding restart args: %w", err)
	}
	var out []string
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parsing restart args: %w", err)
	}
	return out, nil
}

func copySelfForHelper(exePath string) (string, error) {
	data, err := os.ReadFile(exePath)
	if err != nil {
		return "", fmt.Errorf("reading current executable: %w", err)
	}
	ext := filepath.Ext(exePath)
	helperPath := filepath.Join(os.TempDir(), fmt.Sprintf("aviary-upgrade-helper-%d%s", time.Now().UnixNano(), ext))
	if err := os.WriteFile(helperPath, data, 0o755); err != nil {
		return "", fmt.Errorf("writing upgrade helper: %w", err)
	}
	return helperPath, nil
}

func encodeHelperArgs(args []string) (string, error) {
	data, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(data), nil
}

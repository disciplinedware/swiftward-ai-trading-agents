package sandbox

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/blkiodev"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

type DockerManager struct {
	cli   *client.Client
	image string
}

type ResourceOptions struct {
	MemoryMB int
	CpuCount int
	DiskMB   int
}

func NewDockerManager(image string) (*DockerManager, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	if image == "" {
		image = os.Getenv("MCP_SANDBOX_IMAGE")
	}
	if image == "" {
		image = "analytic-sandbox:latest"
	}

	return &DockerManager{
		cli:   cli,
		image: image,
	}, nil
}

func (m *DockerManager) getDevicePath() (string, bool) {
	// Simple heuristic: check common high-perf system devices
	// nvme0n1/nvme1n1 (NVMe), vda (Virtualized), sda (SATA/SCSI)
	devices := []string{"/dev/nvme0n1", "/dev/nvme1n1", "/dev/vda", "/dev/sda", "/dev/sdb"}
	for _, dev := range devices {
		if info, err := os.Stat(dev); err == nil && (info.Mode()&os.ModeDevice) != 0 {
			return dev, true
		}
	}
	return "", false
}

func (m *DockerManager) StartContainer(ctx context.Context, sessionID, hostWorkDir string, allowNetwork bool, res ResourceOptions) (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("failed to get current user: %w", err)
	}

	networkMode := container.NetworkMode("none")
	if allowNetwork {
		networkMode = container.NetworkMode("") // Use default bridge network
	}

	hostConfig := &container.HostConfig{
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: hostWorkDir,
				Target: "/app",
			},
		},
		Resources: container.Resources{
			Memory:     int64(res.MemoryMB) * 1024 * 1024,
			MemorySwap: int64(res.MemoryMB) * 1024 * 1024,
			NanoCPUs:   int64(res.CpuCount) * 1_000_000_000,
			PidsLimit:  &[]int64{256}[0],
			Ulimits: []*container.Ulimit{
				{
					Name: "nproc",
					Soft: 1024,
					Hard: 1024,
				},
				{
					Name: "nofile",
					Soft: 4096,
					Hard: 4096,
				},
			},
		},
		NetworkMode:    networkMode,
		Init:           &[]bool{true}[0],
		ReadonlyRootfs: true,
		Tmpfs: map[string]string{
			"/tmp": "rw,exec,size=64m",
		},
		StorageOpt: map[string]string{
			"size": fmt.Sprintf("%dM", res.DiskMB),
		},
	}

	// Apply I/O limits if a valid device is found
	if devPath, ok := m.getDevicePath(); ok {
		hostConfig.Resources.BlkioDeviceWriteBps = []*blkiodev.ThrottleDevice{
			{
				Path: devPath,
				Rate: 500 * 1024 * 1024, // 500 MB/s
			},
		}
	}

	containerName := fmt.Sprintf("mcp-%s", sessionID)
	config := &container.Config{
		Image:      m.image,
		Cmd:        []string{"tail", "-f", "/dev/null"},
		WorkingDir: "/app",
		User:       fmt.Sprintf("%s:%s", currentUser.Uid, currentUser.Gid),
		Labels: map[string]string{
			"managed-by": "mcp-server",
			"session-id": sessionID,
		},
	}

	// Try to create the container
	resp, err := m.cli.ContainerCreate(ctx, config, hostConfig, nil, nil, containerName)
	if err != nil {
		// Check if it's a conflict (container already exists)
		// We use string check to avoid extra dependency on errdefs package for now
		if strings.Contains(err.Error(), "Conflict") || strings.Contains(err.Error(), "already in use") {
			// Container exists, find its ID
			containers, listErr := m.cli.ContainerList(ctx, container.ListOptions{
				All:     true,
				Filters: filters.NewArgs(filters.Arg("name", containerName)),
			})
			if listErr != nil {
				return "", fmt.Errorf("container exists but failed to list it: %w", listErr)
			}
			if len(containers) == 0 {
				return "", fmt.Errorf("container creation failed with conflict but list returned none: %w", err)
			}
			// Use the existing container ID
			resp.ID = containers[0].ID
		} else {
			return "", err
		}
	}

	// Ensure it is started
	if err := m.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Ignore "already started" error
		if !strings.Contains(err.Error(), "already started") && !strings.Contains(err.Error(), "already in use") {
			return "", err
		}
	}

	return resp.ID, nil
}

func (m *DockerManager) StopContainer(ctx context.Context, containerID string) error {
	return m.cli.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	})
}

func (m *DockerManager) CleanupOrphans(ctx context.Context) error {
	containers, err := m.cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "managed-by=mcp-server")),
	})
	if err != nil {
		return err
	}

	for _, c := range containers {
		_ = m.StopContainer(ctx, c.ID)
	}
	return nil
}

type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

type limitWriter struct {
	w         io.Writer
	limit     int64
	n         int64
	truncated bool
	file      *os.File
}

func (lw *limitWriter) Write(p []byte) (n int, err error) {
	inputLen := len(p)

	if lw.file != nil {
		// optionally write to file, ignoring errors
		_, _ = lw.file.Write(p)
	}

	if lw.n >= lw.limit {
		lw.truncated = true
		return inputLen, nil
	}
	left := lw.limit - lw.n
	var memP []byte
	if int64(len(p)) > left {
		memP = p[:left]
		lw.truncated = true
	} else {
		memP = p
	}
	n, err = lw.w.Write(memP)
	lw.n += int64(n)
	if err == nil {
		// Return full input length to satisfy io.Writer contract and avoid io.ErrShortWrite
		return inputLen, nil
	}
	return n, err
}

func (m *DockerManager) Exec(ctx context.Context, containerID string, cmd []string, timeout time.Duration, outputDir string) (*ExecResult, error) {
	// Construct command with timeout
	finalCmd := buildTimeoutCommand(cmd, timeout)

	execConfig := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          finalCmd,
	}

	execResp, err := m.cli.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return nil, err
	}

	attachResp, err := m.cli.ContainerExecAttach(ctx, execResp.ID, container.ExecStartOptions{})
	if err != nil {
		return nil, err
	}
	defer attachResp.Close()

	var stdout, stderr strings.Builder
	stdoutWriter := &limitWriter{w: &stdout, limit: 8 * 1024}
	stderrWriter := &limitWriter{w: &stderr, limit: 8 * 1024}

	var stdoutFile, stderrFile string
	if outputDir != "" {
		_ = os.MkdirAll(outputDir, 0755)
		prefix := fmt.Sprintf("cmd_%d", time.Now().UnixMilli())

		stdoutFile = filepath.Join(outputDir, prefix+"_stdout.log")
		if fOut, err := os.Create(stdoutFile); err == nil {
			stdoutWriter.file = fOut
		}

		stderrFile = filepath.Join(outputDir, prefix+"_stderr.log")
		if fErr, err := os.Create(stderrFile); err == nil {
			stderrWriter.file = fErr
		}
	}

	done := make(chan error, 1)

	go func() {
		_, err := stdcopy.StdCopy(stdoutWriter, stderrWriter, attachResp.Reader)
		done <- err
	}()

	// We wait for slightly longer than the internal timeout to allow 'timeout' command to do its job
	// and exit. If it hangs, we cancel via context.
	waitTimeout := timeout + 2*time.Second

	var execErr error
	select {
	case <-time.After(waitTimeout):
		// This should theoretically not happen if 'timeout' works, but as a safety net:
		return &ExecResult{
			ExitCode: 124,
			Stderr:   "SYSTEM: terminated because timeout passed (safety net).",
		}, nil
	case err := <-done:
		execErr = err
	case <-ctx.Done():
		execErr = ctx.Err()
	}

	// Close files if they were opened
	if stdoutWriter.file != nil {
		stdoutWriter.file.Close()
	}
	if stderrWriter.file != nil {
		stderrWriter.file.Close()
	}

	if execErr != nil {
		return nil, execErr
	}

	inspectResp, err := m.cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return nil, err
	}

	stdoutStr := stdout.String()
	if stdoutWriter.truncated {
		msg := "Output truncated."
		if stdoutFile != "" {
			msg = fmt.Sprintf("Output too long, truncated. Full log saved to: %s", filepath.Base(outputDir)+"/"+filepath.Base(stdoutFile))
		}
		stdoutStr += fmt.Sprintf("\n... (truncated) [SYSTEM]: %s", msg)
	} else if stdoutFile != "" {
		// Clean up file if not truncated
		_ = os.Remove(stdoutFile)
	}

	stderrStr := stderr.String()
	if stderrWriter.truncated {
		msg := "Stderr truncated."
		if stderrFile != "" {
			msg = fmt.Sprintf("Stderr too long, truncated. Full log saved to: %s", filepath.Base(outputDir)+"/"+filepath.Base(stderrFile))
		}
		stderrStr += fmt.Sprintf("\n... (truncated) [SYSTEM]: %s", msg)
	} else if stderrFile != "" {
		// Clean up file if not truncated
		_ = os.Remove(stderrFile)
	}

	// 'timeout' returns 124 if it timed out, or 137 if it utilized SIGKILL (depending on impl, but usually 124 for timeout command itself if it catches signal, or 128+9 for KILL)
	// Just passing the exit code is sufficient.

	return &ExecResult{
		ExitCode: inspectResp.ExitCode,
		Stdout:   stdoutStr,
		Stderr:   stderrStr,
	}, nil
}

// buildTimeoutCommand helper for testing logic
func buildTimeoutCommand(cmd []string, timeout time.Duration) []string {
	seconds := int(timeout.Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return append([]string{"timeout", "-s", "KILL", fmt.Sprintf("%ds", seconds)}, cmd...)
}

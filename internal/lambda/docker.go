package lambda

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"
)

// Invoker runs a Docker container for one Lambda-style invocation.
type Invoker struct {
	// DockerPath is the docker CLI binary (default "docker").
	DockerPath string
}

// InvokeResult is the outcome of a single invocation.
type InvokeResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

// Run executes `docker run --rm -i` with the given image, passes eventBytes to stdin,
// sets env, applies memory and timeout, and returns stdout/stderr/exit.
func (inv *Invoker) Run(ctx context.Context, imageURI string, eventBytes []byte, env map[string]string, memoryMB, timeoutSec int) InvokeResult {
	if inv == nil {
		return InvokeResult{Err: fmt.Errorf("lambda: nil invoker")}
	}
	dock := inv.DockerPath
	if dock == "" {
		dock = "docker"
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second+2*time.Second)
	defer cancel()
	args := []string{
		"run", "--rm", "-i",
		"--read-only",
		"--stop-timeout", strconv.Itoa(timeoutSec + 1),
	}
	if memoryMB > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dm", memoryMB))
	}
	for k, v := range env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(args, imageURI)
	cmd := exec.CommandContext(ctx, dock, args...)
	cmd.Stdin = bytes.NewReader(eventBytes)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	runErr := cmd.Run()
	ec := 0
	if runErr != nil {
		if ee, ok := runErr.(*exec.ExitError); ok {
			ec = ee.ExitCode()
		} else {
			ec = -1
		}
	}
	return InvokeResult{Stdout: out.Bytes(), Stderr: errBuf.Bytes(), ExitCode: ec, Err: runErr}
}

// DockerCLIAvailable returns true if the docker binary exists in PATH.
func DockerCLIAvailable() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

// DockerDaemonReachable runs `docker version` to verify the socket works.
func DockerDaemonReachable() bool {
	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	cmd.Env = os.Environ()
	_ = cmd.Run()
	return cmd.ProcessState != nil && cmd.ProcessState.Success()
}

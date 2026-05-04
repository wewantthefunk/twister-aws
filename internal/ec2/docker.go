package ec2

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// dockerExec runs the docker CLI. Overridden in tests.
var dockerExec = func(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	return cmd.CombinedOutput()
}

func dockerRunDetached(ctx context.Context, containerName, image string, cmd []string, publish []string) error {
	args := []string{"run", "-d", "--name", containerName}
	for _, p := range publish {
		args = append(args, "-p", p)
	}
	args = append(args, image)
	args = append(args, cmd...)
	out, err := dockerExec(ctx, args...)
	if err != nil {
		return fmt.Errorf("docker %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func dockerStopRemove(ctx context.Context, containerName string) error {
	_, _ = dockerExec(ctx, "stop", containerName)
	out, err := dockerExec(ctx, "rm", containerName)
	if err != nil {
		// rm may fail if already removed; still report if unexpected
		msg := strings.TrimSpace(string(out))
		if msg != "" && !strings.Contains(strings.ToLower(msg), "no such container") {
			return fmt.Errorf("docker rm %s: %w (%s)", containerName, err, msg)
		}
	}
	return nil
}

func dockerPull(ctx context.Context, image string) error {
	out, err := dockerExec(ctx, "pull", image)
	if err != nil {
		return fmt.Errorf("docker pull %s: %w (%s)", image, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func dockerStopOnly(ctx context.Context, containerName string) error {
	out, err := dockerExec(ctx, "stop", containerName)
	if err != nil {
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "no such container") {
			return nil
		}
		return fmt.Errorf("docker stop %s: %w (%s)", containerName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func dockerStart(ctx context.Context, containerName string) error {
	out, err := dockerExec(ctx, "start", containerName)
	if err != nil {
		return fmt.Errorf("docker start %s: %w (%s)", containerName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

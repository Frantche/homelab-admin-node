package runner

import (
	"context"
	"os"
	"os/exec"
)

type Result struct {
	Stdout string
	Stderr string
	Code   int
}

type Runner interface {
	Run(ctx context.Context, name string, args ...string) Result
}

type ExecRunner struct {
	Env []string
	Dir string
}

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) Result {
	cmd := exec.CommandContext(ctx, name, args...)
	if r.Dir != "" {
		cmd.Dir = r.Dir
	}
	if len(r.Env) > 0 {
		cmd.Env = append(os.Environ(), r.Env...)
	}

	stdout, err := cmd.Output()
	if err == nil {
		return Result{Stdout: string(stdout)}
	}

	result := Result{Stdout: string(stdout), Code: 1}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.Stderr = string(exitErr.Stderr)
		result.Code = exitErr.ExitCode()
		return result
	}
	result.Stderr = err.Error()
	return result
}

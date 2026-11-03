package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// commandSpec describes one external process invocation.
type commandSpec struct {
	Dir   string
	Name  string
	Args  []string
	Env   []string // extra "KEY=VALUE" entries layered on the current environment
	Stdin string   // piped to the child when non-empty
}

// runCommand executes spec, forwarding child stdout/stderr to the tool's
// streams, and returns the child's exit code. A non-zero exit is reported as an
// exitCodeError so run() propagates it without an extra message.
func runCommand(spec commandSpec, stdout, stderr io.Writer) error {
	cmd := exec.Command(spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exitCodeError{code: exit.ExitCode()}
		}
		return fmt.Errorf("run %s: %w", spec.Name, err)
	}
	return nil
}

// captureCommand runs spec and returns its combined stdout.
func captureCommand(spec commandSpec) (string, error) {
	cmd := exec.Command(spec.Name, spec.Args...)
	cmd.Dir = spec.Dir
	if len(spec.Env) > 0 {
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	if spec.Stdin != "" {
		cmd.Stdin = strings.NewReader(spec.Stdin)
	}
	output, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return string(output), fmt.Errorf("%s %s: %w: %s", spec.Name, strings.Join(spec.Args, " "), err, strings.TrimSpace(string(exit.Stderr)))
		}
		return string(output), fmt.Errorf("%s %s: %w", spec.Name, strings.Join(spec.Args, " "), err)
	}
	return string(output), nil
}

// lookPath reports whether name resolves on PATH or is an explicit path.
func lookPath(name string) bool {
	if strings.ContainsRune(name, filepath.Separator) {
		info, err := os.Stat(name)
		return err == nil && !info.IsDir()
	}
	_, err := exec.LookPath(name)
	return err == nil
}

// copyFile mirrors fs.copyFileSync: contents and mode, no symlink following
// beyond what os.Open does.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// writeFile writes data with the given mode, creating parent directories.
func writeFile(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, mode)
}

// sha256Hex returns the hex digest of a file, the format SHA256SUMS uses.
func sha256Hex(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

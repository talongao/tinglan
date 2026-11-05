package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
)

// runBinary starts the built app, inheriting this process's stdio, and forwards
// SIGINT/SIGTERM to it — the same contract as start.mjs.
func runBinary(binary string, args []string) error {
	cmd := exec.Command(binary, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		for received := range signals {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(received)
			}
		}
	}()
	if err := cmd.Wait(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exitCodeError{code: exit.ExitCode()}
		}
		return err
	}
	return nil
}

// appBinaryPath is the executable start.mjs runs: the .app bundle on macOS,
// the payload directory elsewhere.
func appBinaryPath(root, platform string) string {
	if platform == "darwin" {
		return filepath.Join(appBundlePath(root), "Contents", "MacOS", "tinglan")
	}
	return filepath.Join(payloadDir(root), executableName(platform))
}

// startApp ports native/scripts/start.mjs: probe Go, build, then run the app.
func startApp(root string, args []string, stdout, stderr io.Writer) error {
	goBin := goBinary()
	if _, err := exec.LookPath(goBin); err != nil {
		// Go is missing: fall back to an existing build.
		binary := appBinaryPath(root, nodePlatform())
		if _, statErr := os.Stat(binary); statErr == nil {
			fmt.Fprintln(stdout, "未找到 Go，启动已构建的本地 Viva 程序；源码修改需重新构建。")
			return runBinary(binary, args)
		}
		fmt.Fprintln(stderr, "需要 Go 1.27.3+ 与本平台 C 编译器，请确认 go 在 PATH 中。")
		return exitCodeError{code: 1}
	}
	if runtime.GOOS == "darwin" {
		if err := macApp(root, stdout, stderr); err != nil {
			return err
		}
	} else if err := buildPayload(root, stdout, stderr); err != nil {
		return err
	}
	return runBinary(appBinaryPath(root, nodePlatform()), args)
}

func startCommand(stdout, stderr io.Writer, args []string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	return startApp(root, args, stdout, stderr)
}

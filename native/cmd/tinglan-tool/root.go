package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"tinglan/native/internal/appmeta"
)

// info is the one-line identity summary shown by `version`.
func info() string { return appmeta.Info() }

// repoRoot locates the repository root (the directory holding native/go.mod and
// the native sources). The removed Node scripts derived it from import.meta.url;
// the Go tool has no module-relative anchor at runtime, so it walks up from the
// working directory and then from the executable.
func repoRoot() (string, error) {
	if wd, err := os.Getwd(); err == nil {
		if root, ok := findRoot(wd); ok {
			return root, nil
		}
	}
	if exe, err := os.Executable(); err == nil {
		if root, ok := findRoot(filepath.Dir(exe)); ok {
			return root, nil
		}
	}
	return "", errors.New("cannot locate the repository root (native/go.mod + build/icon.png)")
}

// findRoot walks from start up to the filesystem root looking for the marker
// files that identify this repository.
func findRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if isRepoRoot(dir) {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isRepoRoot(dir string) bool {
	for _, marker := range []string{filepath.Join("native", "go.mod"), filepath.Join("build", "icon.png")} {
		info, err := os.Stat(filepath.Join(dir, marker))
		if err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

// nodePlatform returns the platform string Node reports as process.platform.
// Output directories and report.json keep the Node spelling so artifacts built
// by either toolchain land in the same place.
func nodePlatform() string {
	if runtime.GOOS == "windows" {
		return "win32"
	}
	return runtime.GOOS
}

// nodeArch returns the architecture string Node reports as process.arch.
func nodeArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	case "386":
		return "ia32"
	default:
		return runtime.GOARCH
	}
}

func executableName(platform string) string {
	if platform == "win32" {
		return "tinglan.exe"
	}
	return "tinglan"
}

// payloadDir is out/native/tinglan/<platform>-<arch>, the directory build.mjs
// writes and check-package.mjs validates.
func payloadDir(root string) string {
	return filepath.Join(root, "out", "native", "tinglan", nodePlatform()+"-"+nodeArch())
}

// Thin accessors over internal/appmeta so the tooling never re-declares the
// product identity in a second place.
func appmetaVersion() string     { return appmeta.Version }
func appmetaDisplayName() string { return appmeta.DisplayName }

// goBinary resolves the Go toolchain from PATH.
func goBinary() string { return "go" }

// usageError keeps error text in the "<command>: <detail>" shape used by every
// subcommand so the CLI prefix is never doubled.
func usageError(command, text string) error {
	return fmt.Errorf("%s: %s", command, text)
}

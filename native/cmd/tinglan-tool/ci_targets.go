package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ciTarget is one row of the GitHub Actions build matrix. Field order matches
// ci-targets.mjs so the emitted JSON is byte-comparable with the Node output.
type ciTarget struct {
	ID        string `json:"id"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	Platform  string `json:"platform"`
	Directory string `json:"directory"`
}

type ciMatrix struct {
	Include []ciTarget `json:"include"`
}

// ciTargetSpec pairs a matrix row with the workflow-dispatch input that selects
// it. `input` is stripped from the emitted matrix.
type ciTargetSpec struct {
	Target ciTarget
	Input  string
}

// ciTargetSpecs mirrors the target table in native/scripts/ci-targets.mjs.
var ciTargetSpecs = []ciTargetSpec{
	{
		Target: ciTarget{ID: "macos-arm64", OS: "macos-15", Arch: "arm64", Platform: "mac", Directory: "darwin-arm64"},
		Input:  "MAC_ARM64",
	},
	{
		Target: ciTarget{ID: "macos-x64", OS: "macos-15-intel", Arch: "x64", Platform: "mac", Directory: "darwin-x64"},
		Input:  "MAC_X64",
	},
	{
		Target: ciTarget{ID: "windows-x64", OS: "windows-2022", Arch: "x64", Platform: "win", Directory: "win32-x64"},
		Input:  "WINDOWS_X64",
	},
}

// Errors are the exact strings ci-targets.mjs throws.
var (
	errAutomaticMainOnly = errors.New("Automatic packages are main-only")
	errManualDevelopOnly = errors.New("Manual builds must select develop")
	errNoPlatforms       = errors.New("Select at least one platform")
	errAutomaticPlugins  = errors.New("Automatic plugin packages are main-only")
	errUnknownPlugin     = errors.New("unknown plugin")
)

// computeMatrix is the pure port of targets(env) from ci-targets.mjs.
//
// Automatic (push) builds are main-only; manual workflow_dispatch builds must
// target develop and must select at least one platform checkbox.
func computeMatrix(event, ref string, selected func(input string) bool) (ciMatrix, error) {
	manual := event == "workflow_dispatch"
	if !manual && (event != "push" || ref != "refs/heads/main") {
		return ciMatrix{}, errAutomaticMainOnly
	}
	if manual && ref != "refs/heads/develop" {
		return ciMatrix{}, errManualDevelopOnly
	}
	include := make([]ciTarget, 0, len(ciTargetSpecs))
	for _, spec := range ciTargetSpecs {
		if manual && !selected(spec.Input) {
			continue
		}
		include = append(include, spec.Target)
	}
	if len(include) == 0 {
		return ciMatrix{}, errNoPlatforms
	}
	return ciMatrix{Include: include}, nil
}

// envSelected reports whether a workflow-dispatch checkbox is ticked. The
// GitHub UI passes the literal string "true".
func envSelected(getenv func(string) string) func(string) bool {
	return func(input string) bool { return getenv(input) == "true" }
}

// computePlugins decides which plugins a run packages. Automatic main builds
// package everything; manual develop builds package only the plugins the
// dispatcher selected (an empty selection packages none).
func computePlugins(event, ref, value string, available []string) (string, error) {
	if event != "workflow_dispatch" {
		if event != "push" || ref != "refs/heads/main" {
			return "", errAutomaticPlugins
		}
		return "all", nil
	}
	want := strings.TrimSpace(value)
	if want == "" {
		return "", nil
	}
	if want == "all" {
		return "all", nil
	}
	known := make(map[string]bool, len(available))
	for _, id := range available {
		known[id] = true
	}
	seen := make(map[string]bool)
	var picked []string
	for _, part := range strings.Split(want, ",") {
		id := strings.TrimSpace(part)
		if id == "" || seen[id] {
			continue
		}
		if !known[id] {
			return "", fmt.Errorf("%w: %s", errUnknownPlugin, id)
		}
		seen[id] = true
		picked = append(picked, id)
	}
	if len(picked) == 0 {
		return "", nil
	}
	return strings.Join(picked, ","), nil
}

// availablePlugins lists plugin ids under <repo>/plugins.
func availablePlugins() ([]string, error) {
	root, err := repoRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "plugins")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list plugins: %w", err)
	}
	var ids []string
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	return ids, nil
}

func ciTargetsCommand(stdout, _ io.Writer, _ []string) error {
	matrix, err := computeMatrix(os.Getenv("EVENT"), os.Getenv("REF"), envSelected(os.Getenv))
	if err != nil {
		return err
	}
	ids, err := availablePlugins()
	if err != nil {
		return err
	}
	plugins, err := computePlugins(os.Getenv("EVENT"), os.Getenv("REF"), os.Getenv("PLUGINS"), ids)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(matrix)
	if err != nil {
		return err
	}
	line := "matrix=" + string(encoded) + "\nplugins=" + plugins + "\n"
	if output := os.Getenv("GITHUB_OUTPUT"); output != "" {
		file, err := os.OpenFile(output, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return fmt.Errorf("append to GITHUB_OUTPUT: %w", err)
		}
		if _, err := file.WriteString(line); err != nil {
			file.Close()
			return fmt.Errorf("append to GITHUB_OUTPUT: %w", err)
		}
		if err := file.Close(); err != nil {
			return fmt.Errorf("append to GITHUB_OUTPUT: %w", err)
		}
		return nil
	}
	_, err = io.WriteString(stdout, line)
	return err
}

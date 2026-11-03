package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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

func ciTargetsCommand(stdout, _ io.Writer, _ []string) error {
	matrix, err := computeMatrix(os.Getenv("EVENT"), os.Getenv("REF"), envSelected(os.Getenv))
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(matrix)
	if err != nil {
		return err
	}
	line := "matrix=" + string(encoded) + "\n"
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

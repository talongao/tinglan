package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// noticesHeader opens THIRD_PARTY_NOTICES.txt, matching licenses.mjs.
const noticesHeader = "Third-party notices for the native build. Source module versions are pinned in native/go.mod and go.sum."

// noticesSeparator joins each license block in licenses.mjs.
const noticesSeparator = "\n\n----------------------------------------\n\n"

// licenseFilePattern is the case-insensitive basename filter from licenses.mjs.
var licenseFilePattern = regexp.MustCompile(`(?i)^(LICENSE|UNLICENSE|COPYING|NOTICE)(\..*)?$`)

// goModRequire is the subset of `go mod edit -json` that licenses.mjs reads.
type goModRequire struct {
	Path    string
	Version string
	Main    bool
}

// moduleDirName escapes a module path/version into its GOMODCACHE directory
// name: uppercase letters become '!' plus the lowercase letter.
func moduleDirName(path, version string) string {
	escape := func(value string) string {
		var builder strings.Builder
		for _, r := range value {
			if r >= 'A' && r <= 'Z' {
				builder.WriteByte('!')
				builder.WriteRune(r + ('a' - 'A'))
				continue
			}
			builder.WriteRune(r)
		}
		return builder.String()
	}
	return escape(path) + "@" + escape(version)
}

// licenseBlock renders one "<path> <version>\n<file name>\n<contents>" section.
func licenseBlock(path, version, name, contents string) string {
	return path + " " + version + "\n" + name + "\n" + contents
}

// findLicenseFile returns the first license-like file in dir, matching the
// readdir+regex scan in licenses.mjs (which errors when nothing matches).
func findLicenseFile(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if licenseFilePattern.MatchString(entry.Name()) {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("No license file found for %s", dir)
}

// buildNotices renders THIRD_PARTY_NOTICES.txt. It is the pure core of
// licenses.mjs so tests can drive it with a fake module cache.
//
// require is the parsed `go mod edit -json` output, moduleDir maps a module
// path+version to its unpacked cache directory, and iconLicense is the contents
// of build/icon.LICENSE.txt appended last.
func buildNotices(require []goModRequire, moduleDir func(path, version string) string, iconLicense string) (string, error) {
	sections := []string{noticesHeader}
	for _, module := range require {
		if module.Main || module.Path == "" {
			continue
		}
		dir := moduleDir(module.Path, module.Version)
		if dir == "" {
			continue
		}
		licensePath, err := findLicenseFile(dir)
		if err != nil {
			return "", err
		}
		contents, err := os.ReadFile(licensePath)
		if err != nil {
			return "", err
		}
		sections = append(sections, licenseBlock(module.Path, module.Version, filepath.Base(licensePath), string(contents)))
		// go-webview2 ships its loader license in a subdirectory.
		if module.Path == "github.com/wailsapp/go-webview2" {
			loaderPath := filepath.Join(dir, "webviewloader", "LICENSE")
			loaderContents, err := os.ReadFile(loaderPath)
			if err != nil {
				return "", err
			}
			sections = append(sections, licenseBlock(module.Path+"/webviewloader", module.Version, "LICENSE", string(loaderContents)))
		}
	}
	sections = append(sections, iconLicense)
	return strings.Join(sections, noticesSeparator), nil
}

// readGoModRequire runs `go mod edit -json` in native/ and parses .Require.
func readGoModRequire(nativeDir, goBin string) ([]goModRequire, error) {
	output, err := captureCommand(commandSpec{Dir: nativeDir, Name: goBin, Args: []string{"mod", "edit", "-json"}})
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Require []goModRequire
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return nil, fmt.Errorf("parse go mod edit -json: %w", err)
	}
	return parsed.Require, nil
}

// goModCache returns GOMODCACHE as Go reports it.
func goModCache(nativeDir, goBin string) (string, error) {
	output, err := captureCommand(commandSpec{Dir: nativeDir, Name: goBin, Args: []string{"env", "GOMODCACHE"}})
	if err != nil {
		return "", err
	}
	cache := strings.TrimSpace(output)
	if cache == "" {
		return "", errors.New("go env GOMODCACHE returned an empty path")
	}
	return cache, nil
}

// writeNotices regenerates THIRD_PARTY_NOTICES.txt at destination. This is the
// direct port of writeNotices() in native/scripts/licenses.mjs.
func writeNotices(root, destination, goBin string) error {
	nativeDir := filepath.Join(root, "native")
	require, err := readGoModRequire(nativeDir, goBin)
	if err != nil {
		return err
	}
	cache, err := goModCache(nativeDir, goBin)
	if err != nil {
		return err
	}
	iconLicense, err := os.ReadFile(filepath.Join(root, "build", "icon.LICENSE.txt"))
	if err != nil {
		return err
	}
	notices, err := buildNotices(require, func(path, version string) string {
		return filepath.Join(cache, moduleDirName(path, version))
	}, string(iconLicense))
	if err != nil {
		return err
	}
	return writeFile(destination, []byte(notices), 0o644)
}

// noticesDefaultDestination is where build.mjs writes the notices inside the
// payload directory.
func noticesDefaultDestination(payload string) string {
	return filepath.Join(payload, "THIRD_PARTY_NOTICES.txt")
}

// sortedKeys is a small helper used by tests and debugging output.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func licensesCommand(stdout, _ io.Writer, args []string) error {
	destination := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--out":
			if i+1 >= len(args) {
				return usageError("licenses", "--out requires a path")
			}
			destination = args[i+1]
			i++
		default:
			return usageError("licenses", fmt.Sprintf("unknown argument %q", args[i]))
		}
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	if destination == "" {
		destination = noticesDefaultDestination(payloadDir(root))
	}
	if err := writeNotices(root, destination, goBinary()); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Wrote %s\n", destination)
	return err
}

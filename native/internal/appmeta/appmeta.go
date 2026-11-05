// Package appmeta is the single source of truth for the product's identity,
// bundle identifier, user data directory and license.
//
// These are the only source of truth after the Node toolchain was removed on
// 2026-11-01: cmd/tinglan-tool reads them for build, package and release, and
// `tinglan-tool icons` regenerates native/cmd/music/app_name.go from them.
package appmeta

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"
)

const (
	// Version is the product version. It is the only version source: the build
	// tool, release workflow and Info.plist all read this constant.
	Version = "0.0.3"

	// DisplayName is the user-visible product name (window title, bundles).
	DisplayName = "听澜"

	// BrandLabel is the compact brand label used inside the UI.
	BrandLabel = "听澜"

	// BundleID is the reverse-DNS macOS bundle identifier.
	BundleID = "org.tinglan.unofficial.internal.viva"

	// DataDir is the profile directory, relative to os.UserConfigDir(). It is
	// slash-separated. Older builds used "huawei-music-desktop/native-v1";
	// MigrateDataDir moves that directory here on first launch.
	DataDir = "tinglan/native-v1"

	// legacyDataDir is the pre-rename profile directory.
	legacyDataDir = "huawei-music-desktop/native-v1"

	// License is the project license identifier used by package metadata.
	License = "AGPL-3.0-only"
)

// Info returns a one-line summary such as
// "听澜 0.0.3 (org.tinglan.unofficial.internal.viva)".
func Info() string {
	return fmt.Sprintf("%s %s (%s)", DisplayName, Version, BundleID)
}

// Build returns a short, user-reportable build identity: the VCS revision
// embedded by the Go toolchain, or "dev" outside a repository. The bundle
// identifier is not a build number and cannot tell two local builds apart when
// a user reports a defect.
func Build() string {
	revision, _, modified := vcs()
	if revision == "" {
		return "dev"
	}
	if modified {
		return revision + "+dirty"
	}
	return revision
}

// CommitAt returns the VCS commit time embedded in the binary, formatted for
// display. It is the time the code was committed, not the time this binary was
// packaged, so the About page labels it separately.
func CommitAt() string {
	_, at, _ := vcs()
	if at == "" {
		return ""
	}
	return at
}

// PackagedAt returns the executable's modification time, which is the closest
// thing to a packaging timestamp a Go binary can report about itself.
func PackagedAt() string {
	path, err := os.Executable()
	if err != nil {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return info.ModTime().Local().Format("2006-01-02 15:04")
}

func vcs() (revision, at string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", "", false
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, setting.Value); err == nil {
				at = t.Local().Format("2006-01-02 15:04")
			}
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	return revision, at, modified
}

// MigrateDataDir moves the pre-rename profile directory to DataDir so an
// existing library, queue and session survive the rename. It is a no-op when
// the new directory already exists, when there is nothing to move, or when the
// rename fails (the app then starts empty rather than deleting old data).
func MigrateDataDir() error {
	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	target := filepath.Join(base, filepath.FromSlash(DataDir))
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	legacy := filepath.Join(base, filepath.FromSlash(legacyDataDir))
	if _, err := os.Stat(legacy); err != nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.Rename(legacy, target)
}

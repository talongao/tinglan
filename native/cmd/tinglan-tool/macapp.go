package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"tinglan/native/internal/appmeta"
)

// appmetaBundleID is a seam so tests can pin the Info.plist identifier.
func appmetaBundleID() string { return appmeta.BundleID }

// infoPlist renders the Info.plist build-macos.mjs writes. Key order is
// preserved because the Node script's output is byte-for-byte reproducible and
// release diffs compare against it.
func infoPlist(version, displayName, bundleID string) string {
	xml := func(value string) string {
		value = strings.ReplaceAll(value, "&", "&amp;")
		value = strings.ReplaceAll(value, "<", "&lt;")
		return strings.ReplaceAll(value, ">", "&gt;")
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>` + xml(bundleID) + `</string>
<key>CFBundleName</key><string>` + xml(displayName) + `</string>
<key>CFBundleDisplayName</key><string>` + xml(displayName) + `</string>
<key>CFBundleExecutable</key><string>tinglan</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>CFBundleIconFile</key><string>AppIcon.icns</string>
<key>LSApplicationCategoryType</key><string>public.app-category.music</string>
<key>CFBundleVersion</key><string>` + xml(version) + `</string>
<key>CFBundleShortVersionString</key><string>` + xml(version) + `</string>
<key>NSHighResolutionCapable</key><true/>
</dict></plist>
`
}

// iconSetEntries lists the sips outputs for the .iconset, in the order
// build-macos.mjs generates them.
type iconSetEntry struct {
	Name string
	Size int
}

func iconSetEntries() []iconSetEntry {
	sizes := []int{16, 32, 128, 256, 512}
	entries := make([]iconSetEntry, 0, len(sizes)*2)
	for _, size := range sizes {
		entries = append(entries, iconSetEntry{
			Name: fmt.Sprintf("icon_%dx%d.png", size, size),
			Size: size,
		})
		entries = append(entries, iconSetEntry{
			Name: fmt.Sprintf("icon_%dx%d@2x.png", size, size),
			Size: size * 2,
		})
	}
	return entries
}

// appBundlePath is out/native/Tinglan.app, the bundle build-macos.mjs emits.
func appBundlePath(root string) string {
	return filepath.Join(root, "out", "native", "Tinglan.app")
}

// macApp builds out/native/Tinglan.app. Port of native/scripts/build-macos.mjs.
func macApp(root string, stdout, stderr io.Writer) error {
	if runtime.GOOS != "darwin" {
		return errors.New("This build requires macOS")
	}
	bundle := appBundlePath(root)
	contents := filepath.Join(bundle, "Contents")
	if err := os.MkdirAll(filepath.Join(contents, "MacOS"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(contents, "Resources"), 0o755); err != nil {
		return err
	}
	// A prior certificate-signed build must not leave a stale bundle seal when
	// rebuilding without an explicit signing identity.
	if err := os.RemoveAll(filepath.Join(contents, "_CodeSignature")); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(root, "build", "icon.png"), filepath.Join(contents, "Resources", "icon.png")); err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "hwmusic-icon-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	iconset := filepath.Join(scratch, "AppIcon.iconset")
	if err := os.Mkdir(iconset, 0o755); err != nil {
		return err
	}
	master := filepath.Join(root, "build", "icon-1024.png")
	for _, entry := range iconSetEntries() {
		dimension := fmt.Sprint(entry.Size)
		if err := runCommand(commandSpec{
			Name: "sips",
			Args: []string{"-z", dimension, dimension, master, "--out", filepath.Join(iconset, entry.Name)},
		}, io.Discard, io.Discard); err != nil {
			return fmt.Errorf("sips %s: %w", entry.Name, err)
		}
	}
	if err := runCommand(commandSpec{
		Name: "iconutil",
		Args: []string{"-c", "icns", iconset, "-o", filepath.Join(contents, "Resources", "AppIcon.icns")},
	}, stdout, stderr); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(root, "build", "icon.LICENSE.txt"), filepath.Join(contents, "Resources", "icon.LICENSE.txt")); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(root, "LICENSE"), filepath.Join(contents, "Resources", "LICENSE")); err != nil {
		return err
	}
	if err := writeNotices(root, filepath.Join(contents, "Resources", "THIRD_PARTY_NOTICES.txt"), goBinary()); err != nil {
		return err
	}
	if err := runCommand(commandSpec{
		Dir:  filepath.Join(root, "native"),
		Name: goBinary(),
		Args: []string{"build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(contents, "MacOS", "tinglan"), "./cmd/music"},
	}, stdout, stderr); err != nil {
		return err
	}
	// The Huawei source ships as a preinstalled external plugin. Keeping it in
	// the bundle means the host can load every source through one protocol.
	pluginDir := filepath.Join(contents, "Resources", "sources", "huawei")
	if err := os.MkdirAll(filepath.Join(pluginDir, "bin"), 0o755); err != nil {
		return err
	}
	if err := runCommand(commandSpec{
		Dir:  filepath.Join(root, "native"),
		Name: goBinary(),
		Args: []string{"build", "-trimpath", "-ldflags=-s -w", "-o", filepath.Join(pluginDir, "bin", "huawei-source"), "./cmd/huawei-source"},
	}, stdout, stderr); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(root, "native", "plugins", "huawei", "source.json"), filepath.Join(pluginDir, "source.json")); err != nil {
		return err
	}
	// Replacing the executable updates the file mtime but not the bundle
	// directory's, so Finder keeps showing the first build's date. Touch the
	// bundle after every successful build so "Date Modified" is truthful.
	now := time.Now()
	if err := os.Chtimes(bundle, now, now); err != nil {
		return err
	}
	if err := os.Chtimes(contents, now, now); err != nil {
		return err
	}
	plist := infoPlist(appmeta.Version, appmeta.DisplayName, appmetaBundleID())
	if err := writeFile(filepath.Join(contents, "Info.plist"), []byte(plist), 0o644); err != nil {
		return err
	}
	if err := runCommand(commandSpec{
		Name: "plutil",
		Args: []string{"-lint", filepath.Join(contents, "Info.plist")},
	}, stdout, stderr); err != nil {
		return err
	}
	if _, err := signMacOS(bundle, stderr); err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, "Built local Viva app (not notarized; no global hms handler).")
	return err
}

func macAppCommand(stdout, stderr io.Writer, _ []string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	return macApp(root, stdout, stderr)
}

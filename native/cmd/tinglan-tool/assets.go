package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// assetsCommand regenerates native/assets from the committed icon masters, the
// direct port of generate-assets.mjs. The icon pipeline stays PNG-based: the
// committed bitmaps are the source of truth, so this command only copies and
// verifies the files the packaging steps consume.
func assetsCommand(_ io.Writer, stderr io.Writer, _ []string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	opts := assetsOptions{root: root}
	var builder strings.Builder
	if err := runAssets(opts, &builder); err != nil {
		return err
	}
	_, err = io.WriteString(stderr, builder.String())
	return err
}

type assetsOptions struct {
	root string
}

func runAssets(opts assetsOptions, stderr *strings.Builder) error {
	src := filepath.Join(opts.root, "build", "icon.png")
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("icon master missing: %w", err)
	}
	targets := map[string]string{
		filepath.Join(opts.root, "native", "assets", "icon.png"): filepath.Join(opts.root, "build", "icon.png"),
		filepath.Join(opts.root, "native", "assets", "tray.png"): filepath.Join(opts.root, "build", "tray.png"),
	}
	for target, source := range targets {
		if err := copyFile(source, target); err != nil {
			return err
		}
		stderr.WriteString("assets: wrote " + target + "\n")
	}
	return nil
}

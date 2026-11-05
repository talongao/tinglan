// Command tinglan-tool is the single native build and tooling entry point. It
// replaced the Node scripts under native/scripts (removed 2026-11-01), so the
// repository no longer needs Node.js, npm or playwright to build or verify the
// app.
//
// Subcommands:
//
//	version    — product identity from internal/appmeta
//	ci-targets — GitHub Actions build matrix
//	build      — build ./cmd/music into out/native/tinglan/<os>-<arch>/
//	start      — build, then run the native app
//	mac-app    — build out/native/Tinglan.app (macOS only)
//	package    — build a local installer (macOS: DMG)
//	assets     — copy the PNG masters into internal/icons
//	icons      — compile native/assets/lucide/*.svg into internal/icons/icons.go
//	             and regenerate cmd/music/app_name.go
//	licenses   — write THIRD_PARTY_NOTICES.txt
//	plugins    — package plugins/ into release/plugins/*.zip
//	ui-check   — headless UI regression run
package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"tinglan/native/internal/appmeta"
)

// usageText lists every subcommand. It is printed for `help` (exit 0) and for
// an unknown subcommand (exit 2), so the two paths never drift apart.
const usageText = `tinglan-tool — 听澜 native build and tooling

usage: tinglan-tool <command> [arguments]

commands:
  version                    print the product identity summary
  ci-targets                 compute the GitHub Actions build matrix
  build                      build ./cmd/music into out/native/tinglan/<os>-<arch>/
  start                      build, then run the native app
  mac-app                    build out/native/Tinglan.app (macOS only)
  package [platform] [arch]  build a local installer (macOS: DMG)
  assets                     copy the committed PNG masters into internal/icons
  icons                      compile native/assets/lucide/*.svg and refresh app_name.go
  licenses [--out PATH]      write THIRD_PARTY_NOTICES.txt
  plugins                    package plugins/ into release/plugins/*.zip
  ui-check                   headless UI regression run (no Node)

environment:
  HWMUSIC_MAC_SIGN_IDENTITY  macOS codesign identity (default: ad-hoc "-")
  HWMUSIC_WINDRES            windres binary for the Windows resource step
`

type commandFunc func(stdout, stderr io.Writer, args []string) error

// commands is the full dispatch table; it is also the source of truth for the
// "every subcommand is listed" requirement, checked by TestUsageListsCommands.
var commands = map[string]commandFunc{
	"version":    versionCommand,
	"ci-targets": ciTargetsCommand,
	"build":      buildCommand,
	"start":      startCommand,
	"mac-app":    macAppCommand,
	"package":    packageCommand,
	"assets":     assetsCommand,
	"icons":      iconsCommand,
	"licenses":   licensesCommand,
	"plugins":    pluginsCommand,
	"ui-check":   uiCheckCommand,
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run executes one subcommand and returns the process exit code.
//
//	0  success
//	1  command failure (a clear error on stderr)
//	2  missing or unknown subcommand (usage on stderr)
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	name, rest := args[0], args[1:]
	if name == "help" || name == "-h" || name == "--help" {
		fmt.Fprint(stdout, usageText)
		return 0
	}
	command, ok := commands[name]
	if !ok {
		fmt.Fprintf(stderr, "tinglan-tool: unknown command %q\n\n", name)
		fmt.Fprint(stderr, usageText)
		return 2
	}
	if err := command(stdout, stderr, rest); err != nil {
		var exit exitCodeError
		if errors.As(err, &exit) {
			// The child process already reported its own failure; do not add noise.
			return exit.code
		}
		fmt.Fprintf(stderr, "tinglan-tool %s: %v\n", name, err)
		return 1
	}
	return 0
}

// exitCodeError propagates a child process exit status without printing an
// extra message, matching Node's `process.exit(code)` behaviour.
type exitCodeError struct{ code int }

func (e exitCodeError) Error() string { return fmt.Sprintf("exit status %d", e.code) }

func versionCommand(stdout, _ io.Writer, args []string) error {
	value := info()
	for _, arg := range args {
		switch arg {
		case "--short":
			value = appmeta.Version
		default:
			return usageError("version", fmt.Sprintf("unknown argument %q", arg))
		}
	}
	_, err := fmt.Fprintln(stdout, value)
	return err
}

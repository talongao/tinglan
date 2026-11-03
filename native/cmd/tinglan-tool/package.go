package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// packageRequest is the parsed `package [platform] [arch]` invocation.
type packageRequest struct {
	// Platform is the Node spelling of the requested platform (win32/darwin/linux),
	// or "" when the argument was omitted (meaning "this host").
	Platform string
	// Arch is the Go spelling of the requested architecture (arm64/amd64), or "".
	Arch string
}

// platformAliases maps the CLI aliases package.mjs accepts onto Node platform
// names. Anything else falls back to the host platform, exactly like
// `{win:'win32',mac:'darwin',linux:'linux'}[requested] || process.platform`.
var platformAliases = map[string]string{
	"win":   "win32",
	"mac":   "darwin",
	"linux": "linux",
}

// parsePackageArgs mirrors package.mjs argument handling: argv[2] is the
// platform alias, argv[3] is the architecture with x64 normalised to amd64.
func parsePackageArgs(args []string) (packageRequest, error) {
	var request packageRequest
	if len(args) > 2 {
		return request, usageError("package", "too many arguments; usage: package [mac|win|linux] [arm64|x64]")
	}
	if len(args) >= 1 && args[0] != "" {
		platform, ok := platformAliases[args[0]]
		if !ok {
			return request, usageError("package", fmt.Sprintf("unknown platform %q (want mac, win or linux)", args[0]))
		}
		request.Platform = platform
	}
	if len(args) == 2 {
		request.Arch = strings.ReplaceAll(args[1], "x64", "amd64")
	}
	return request, nil
}

// validatePackageRequest enforces the two host checks package.mjs performs.
func validatePackageRequest(request packageRequest, hostPlatform, hostArch string) error {
	if request.Platform != "" && request.Platform != hostPlatform {
		return errors.New("Build the native installer on its target OS")
	}
	if request.Arch != "" && request.Arch != strings.ReplaceAll(hostArch, "x64", "amd64") {
		return errors.New("Build this installer on its target architecture")
	}
	return nil
}

// dmgName is the artifact file name package.mjs builds on macOS.
func dmgName(version, arch string, signed bool) string {
	state := "unsigned"
	if signed {
		state = "local-signed"
	}
	return fmt.Sprintf("tinglan-%s-%s-%s.dmg", version, arch, state)
}

// expectedPayload lists the exact files check-package.mjs allows in the payload
// directory, sorted, with the binary name substituted per platform.
func expectedPayload(platform string) []string {
	names := []string{"LICENSE", "NOTICE.txt", "SHA256SUMS", "THIRD_PARTY_NOTICES.txt", "icon.LICENSE.txt", executableName(platform)}
	sort.Strings(names)
	return names
}

// requiredNotices are the module names check-package.mjs asserts appear in
// THIRD_PARTY_NOTICES.txt.
var requiredNotices = []string{
	"vivaui/viva",
	"godbus/dbus",
	"gorilla/websocket",
	"media-winrt-go",
	"go-ole",
	"wailsapp/go-webview2/webviewloader",
	"zzl/go-webview2",
	"zzl/go-com",
	"zzl/go-win32api",
}

// checkPayload is the port of native/scripts/check-package.mjs: the payload
// allowlist, SHA256SUMS agreement, minimum binary size, Windows PE/GUI/.rsrc
// checks, required license names and the OAuth NOTICE marker.
func checkPayload(dir string, platform string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	actual := make([]string, 0, len(entries))
	for _, entry := range entries {
		actual = append(actual, entry.Name())
	}
	sort.Strings(actual)
	expected := expectedPayload(platform)
	if strings.Join(actual, "\x00") != strings.Join(expected, "\x00") {
		return fmt.Errorf("Unexpected payload file: got %v, want %v", actual, expected)
	}
	for _, name := range expected {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("No links or directories in payload")
		}
	}
	binaryName := executableName(platform)
	binaryPath := filepath.Join(dir, binaryName)
	digest, err := sha256Hex(binaryPath)
	if err != nil {
		return err
	}
	sums, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		return err
	}
	if string(sums) != digest+"  "+binaryName+"\n" {
		return errors.New("SHA256SUMS does not match the native binary")
	}
	info, err := os.Stat(binaryPath)
	if err != nil {
		return err
	}
	if info.Size() <= 1000000 {
		return errors.New("Missing native binary")
	}
	if platform == "win32" {
		if err := checkWindowsBinary(binaryPath); err != nil {
			return err
		}
	}
	notices, err := os.ReadFile(filepath.Join(dir, "THIRD_PARTY_NOTICES.txt"))
	if err != nil {
		return err
	}
	for _, name := range requiredNotices {
		if !strings.Contains(string(notices), name) {
			return fmt.Errorf("Missing license %s", name)
		}
	}
	notice, err := os.ReadFile(filepath.Join(dir, "NOTICE.txt"))
	if err != nil {
		return err
	}
	if !strings.Contains(string(notice), "OAuth") {
		return errors.New("NOTICE.txt must mention the OAuth identity boundary")
	}
	return nil
}

// checkWindowsBinary performs the PE header, GUI subsystem and .rsrc assertions
// from check-package.mjs.
func checkWindowsBinary(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) < 0x40 {
		return errors.New("Missing PE header")
	}
	pe := int(binary.LittleEndian.Uint32(data[0x3c:]))
	if pe+24 > len(data) || string(data[pe:pe+4]) != "PE\x00\x00" {
		return errors.New("Missing PE header")
	}
	optional := pe + 24
	if optional+70 > len(data) {
		return errors.New("Truncated PE optional header")
	}
	if binary.LittleEndian.Uint16(data[optional+68:]) != 2 {
		return errors.New("App must use GUI subsystem, not console")
	}
	sections := int(binary.LittleEndian.Uint16(data[pe+6:]))
	start := optional + int(binary.LittleEndian.Uint16(data[pe+20:]))
	resources := false
	for i := 0; i < sections; i++ {
		section := start + i*40
		if section+40 > len(data) {
			break
		}
		name := strings.ReplaceAll(string(data[section:section+8]), "\x00", "")
		if name == ".rsrc" && binary.LittleEndian.Uint32(data[section+16:]) > 0 {
			resources = true
		}
	}
	if !resources {
		return errors.New("Missing embedded Windows icon resources")
	}
	return nil
}

// packageLocal is the port of native/scripts/package.mjs for macOS. Windows
// NSIS and Linux tarballs are intentionally not ported here (see the error text).
func packageLocal(root string, request packageRequest, stdout, stderr io.Writer) error {
	platform := nodePlatform()
	arch := nodeArch()
	if err := validatePackageRequest(request, platform, arch); err != nil {
		return err
	}
	output := filepath.Join(root, "release", "native", platform+"-"+arch)
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	if err := buildPayload(root, stdout, stderr); err != nil {
		return err
	}
	payload := payloadDir(root)
	if err := checkPayload(payload, platform); err != nil {
		return err
	}
	switch platform {
	case "darwin":
	case "win32":
		return errors.New("Windows NSIS packaging is out of scope for the Go port; run native/scripts/package.mjs on Windows")
	case "linux":
		return errors.New("Linux tarball packaging is out of scope for the Go port; run native/scripts/package.mjs on Linux")
	default:
		return errors.New("Unsupported installer platform")
	}
	if err := macApp(root, stdout, stderr); err != nil {
		return err
	}
	staging, err := os.MkdirTemp("", "hwmusic-native-dmg-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	appName := appmetaDisplayName() + ".app"
	if err := runCommand(commandSpec{
		Name: "ditto",
		Args: []string{appBundlePath(root), filepath.Join(staging, appName)},
	}, stdout, stderr); err != nil {
		return err
	}
	if err := runCommand(commandSpec{
		Name: "codesign",
		Args: []string{"--verify", "--deep", "--strict", filepath.Join(staging, appName)},
	}, stdout, stderr); err != nil {
		return err
	}
	applications := filepath.Join(staging, "Applications")
	if err := os.Symlink("/Applications", applications); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(payload, "NOTICE.txt"), filepath.Join(staging, "NOTICE.txt")); err != nil {
		return err
	}
	signed := os.Getenv("HWMUSIC_MAC_SIGN_IDENTITY") != ""
	artifact := filepath.Join(output, dmgName(appmetaVersion(), arch, signed))
	if err := runCommand(commandSpec{
		Name: "hdiutil",
		Args: []string{"create", "-ov", "-format", "UDZO", "-volname", appmetaDisplayName(), "-srcfolder", staging, artifact},
	}, stdout, stderr); err != nil {
		return err
	}
	if err := runCommand(commandSpec{Name: "hdiutil", Args: []string{"verify", artifact}}, stdout, stderr); err != nil {
		return err
	}
	digest, err := sha256Hex(artifact)
	if err != nil {
		return err
	}
	sums := digest + "  " + filepath.Base(artifact) + "\n"
	if err := writeFile(filepath.Join(output, "SHA256SUMS"), []byte(sums), 0o644); err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Local test package: %s\n", artifact)
	return err
}

func packageCommand(stdout, stderr io.Writer, args []string) error {
	request, err := parsePackageArgs(args)
	if err != nil {
		return err
	}
	root, err := repoRoot()
	if err != nil {
		return err
	}
	return packageLocal(root, request, stdout, stderr)
}

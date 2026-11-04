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
		return packageDMG(root, output, payload, stdout, stderr)
	case "win32":
		return packageNSIS(root, output, payload, stdout, stderr)
	case "linux":
		return errors.New("Linux tarball packaging is out of scope for the Go port; run native/scripts/package.mjs on Linux")
	default:
		return errors.New("Unsupported installer platform")
	}
}

// packageDMG is the macOS half of package.mjs: stage Tinglan.app, add the
// /Applications symlink and NOTICE.txt, then build and verify the UDZO image.
func packageDMG(root, output, payload string, stdout, stderr io.Writer) error {
	arch := nodeArch()
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

// packageNSIS is the Windows half of package.mjs: write a UTF-8 NSIS script for
// the staged payload and run makensis to produce the unsigned installer.
func packageNSIS(root, output, payload string, stdout, stderr io.Writer) error {
	icon := filepath.Join(root, "out", "native", "windows-resources", "app.ico")
	if _, err := os.Stat(icon); err != nil {
		return fmt.Errorf("Windows icon: %w", err)
	}
	staging, err := os.MkdirTemp("", "hwmusic-native-nsis-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	version := appmetaVersion()
	name := fmt.Sprintf("tinglan-%s-%s-setup-unsigned.exe", version, nodeArch())
	artifact := filepath.Join(output, name)
	notice, err := os.ReadFile(filepath.Join(payload, "NOTICE.txt"))
	if err != nil {
		return err
	}
	license := filepath.Join(staging, "license-utf16.txt")
	body := "\ufeff" + appmetaDisplayName() + " · 安装说明\r\n\r\n" + string(notice)
	if err := os.WriteFile(license, utf16le(body), 0o644); err != nil {
		return err
	}
	sources, err := stageBundledPlugins(root, filepath.Join(payload, "sources"), stdout, stderr)
	if err != nil {
		return fmt.Errorf("bundle plugins: %w", err)
	}
	script := nsisScript(nsisConfig{
		Icon:       icon,
		Name:       appmetaDisplayName(),
		Output:     artifact,
		InstallDir: "$LOCALAPPDATA\\Programs\\Tinglan",
		Version:    version,
		Notice:     license,
		Payload:    payload,
		Files:      []string{executableName("win32"), "LICENSE", "THIRD_PARTY_NOTICES.txt", "icon.LICENSE.txt", "NOTICE.txt", "SHA256SUMS"},
		Sources:    sources,
	})
	file := filepath.Join(staging, "installer.nsi")
	if err := writeFile(file, []byte("\ufeff"+script), 0o644); err != nil {
		return err
	}
	makensis := os.Getenv("HWMUSIC_MAKENSIS")
	if makensis == "" {
		makensis = "makensis"
	}
	if err := runCommand(commandSpec{
		Name: makensis,
		Args: []string{"/INPUTCHARSET", "UTF8", "/V2", file},
	}, stdout, stderr); err != nil {
		return fmt.Errorf("makensis (set HWMUSIC_MAKENSIS if it is not on PATH): %w", err)
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

// nsisConfig carries the values package.mjs interpolates into installer.nsi.
type nsisConfig struct {
	Icon       string
	Name       string
	Output     string
	InstallDir string
	Version    string
	Notice     string
	Payload    string
	Files      []string
	Sources    bool
}

// sourcesInstall ships the bundled official plugins beside the executable; the
// host discovers them at <exe>/sources/<id>/source.json.
func sourcesInstall(cfg nsisConfig) string {
	if !cfg.Sources {
		return ""
	}
	return " SetOutPath \"$INSTDIR\\sources\"\n" +
		" File /r \"" + nsisEscape(filepath.Join(cfg.Payload, "sources")) + "\\*.*\"\n" +
		" SetOutPath \"$INSTDIR\"\n"
}

func sourcesUninstall(cfg nsisConfig) string {
	if !cfg.Sources {
		return ""
	}
	return " RMDir /r \"$INSTDIR\\sources\"\n"
}

// nsisEscape mirrors package.mjs' esc(): dollars double and quotes escape.
func nsisEscape(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "$", "$$"), `"`, `$\"`)
}

// nsisScript renders the installer script byte-for-byte like package.mjs.
func nsisScript(cfg nsisConfig) string {
	var files strings.Builder
	for _, file := range cfg.Files {
		path := filepath.Join(cfg.Payload, file)
		fmt.Fprintf(&files, " File \"%s\"\n", nsisEscape(path))
	}
	var deletes strings.Builder
	for _, file := range cfg.Files {
		fmt.Fprintf(&deletes, " Delete \"$INSTDIR\\%s\"\n", file)
	}
	name := nsisEscape(cfg.Name)
	shortcut := "$SMPROGRAMS\\Tinglan\\" + name + ".lnk"
	return "Unicode true\n" +
		"!include \"MUI2.nsh\"\n" +
		"!define MUI_ICON \"" + nsisEscape(cfg.Icon) + "\"\n" +
		"!define MUI_UNICON \"" + nsisEscape(cfg.Icon) + "\"\n" +
		"Name \"" + name + "\"\n" +
		"OutFile \"" + nsisEscape(cfg.Output) + "\"\n" +
		"RequestExecutionLevel user\n" +
		"InstallDir \"" + cfg.InstallDir + "\"\n" +
		"InstallDirRegKey HKCU \"Software\\HuaweiMusicViva\" \"InstallDir\"\n" +
		"!insertmacro MUI_PAGE_WELCOME\n" +
		"!insertmacro MUI_PAGE_LICENSE \"" + nsisEscape(cfg.Notice) + "\"\n" +
		"!insertmacro MUI_PAGE_DIRECTORY\n" +
		"!insertmacro MUI_PAGE_INSTFILES\n" +
		"!define MUI_FINISHPAGE_RUN \"$INSTDIR\\" + cfg.Files[0] + "\"\n" +
		"!define MUI_FINISHPAGE_RUN_TEXT \"打开" + nsisEscape(cfg.Name) + "\"\n" +
		"!insertmacro MUI_PAGE_FINISH\n" +
		"!insertmacro MUI_UNPAGE_CONFIRM\n" +
		"!insertmacro MUI_UNPAGE_INSTFILES\n" +
		"!insertmacro MUI_LANGUAGE \"SimpChinese\"\n" +
		"Section\n" +
		" SetShellVarContext current\n" +
		" SetOutPath \"$INSTDIR\"\n" +
		files.String() +
		sourcesInstall(cfg) +
		" WriteUninstaller \"$INSTDIR\\Uninstall.exe\"\n" +
		" CreateDirectory \"$SMPROGRAMS\\Tinglan\"\n" +
		" CreateDirectory \"$INSTDIR\\upgrade-backup\"\n" +
		legacyShortcutScript(cfg.Name) +
		" CreateShortcut \"" + shortcut + "\" \"$INSTDIR\\" + cfg.Files[0] + "\"\n" +
		" CreateShortcut \"$SMPROGRAMS\\Tinglan\\Uninstall.lnk\" \"$INSTDIR\\Uninstall.exe\"\n" +
		" WriteRegStr HKCU \"Software\\HuaweiMusicViva\" \"InstallDir\" \"$INSTDIR\"\n" +
		" WriteRegStr HKCU \"Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\HuaweiMusicViva\" \"DisplayName\" \"" + name + "\"\n" +
		" WriteRegStr HKCU \"Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\HuaweiMusicViva\" \"DisplayVersion\" \"" + cfg.Version + "\"\n" +
		" WriteRegStr HKCU \"Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\HuaweiMusicViva\" \"UninstallString\" '\"$INSTDIR\\Uninstall.exe\"'\n" +
		"SectionEnd\n" +
		"Section \"Uninstall\"\n" +
		" SetShellVarContext current\n" +
		deletes.String() +
		sourcesUninstall(cfg) +
		" Delete \"$INSTDIR\\Uninstall.exe\"\n" +
		" Delete \"" + shortcut + "\"\n" +
		" Delete \"$SMPROGRAMS\\Tinglan\\Uninstall.lnk\"\n" +
		" RMDir \"$SMPROGRAMS\\Tinglan\"\n" +
		" RMDir \"$INSTDIR\"\n" +
		" DeleteRegKey HKCU \"Software\\HuaweiMusicViva\"\n" +
		" DeleteRegKey HKCU \"Software\\Microsoft\\Windows\\CurrentVersion\\Uninstall\\HuaweiMusicViva\"\n" +
		"SectionEnd\n"
}

// legacyShortcutScript emits the upgrade step that moves Start-Menu shortcuts
// left behind by older installers (their names were UTF-8 bytes decoded as
// latin1/windows-1252) into upgrade-backup, exactly like package.mjs did.
func legacyShortcutScript(display string) string {
	var out strings.Builder
	for i, name := range legacyShortcutNames(display) {
		link := "$SMPROGRAMS\\Tinglan\\" + nsisEscape(name) + ".lnk"
		fmt.Fprintf(&out, " IfFileExists \"%s\" 0 +2\n", link)
		fmt.Fprintf(&out, " Rename \"%s\" \"$INSTDIR\\upgrade-backup\\legacy-shortcut-%d.lnk.bak\"\n", link, i)
	}
	return out.String()
}

// legacyShortcutNames returns the mojibake spellings older installers created
// for the display name: the same bytes read as latin1 and as windows-1252.
func legacyShortcutNames(display string) []string {
	raw := []byte(display)
	latin1 := make([]rune, 0, len(raw))
	cp1252 := make([]rune, 0, len(raw))
	for _, b := range raw {
		latin1 = append(latin1, rune(b))
		cp1252 = append(cp1252, cp1252Rune(b))
	}
	seen := map[string]bool{display: true}
	var names []string
	for _, name := range []string{string(latin1), string(cp1252)} {
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}

// cp1252Rune maps the 0x80-0x9F range that windows-1252 differs on; all other
// bytes are identical to latin1.
func cp1252Rune(b byte) rune {
	table := map[byte]rune{
		0x80: 0x20ac, 0x82: 0x201a, 0x83: 0x0192, 0x84: 0x201e, 0x85: 0x2026,
		0x86: 0x2020, 0x87: 0x2021, 0x88: 0x02c6, 0x89: 0x2030, 0x8a: 0x0160,
		0x8b: 0x2039, 0x8c: 0x0152, 0x8e: 0x017d, 0x91: 0x2018, 0x92: 0x2019,
		0x93: 0x201c, 0x94: 0x201d, 0x95: 0x2022, 0x96: 0x2013, 0x97: 0x2014,
		0x98: 0x02dc, 0x99: 0x2122, 0x9a: 0x0161, 0x9b: 0x203a, 0x9c: 0x0153,
		0x9e: 0x017e, 0x9f: 0x0178,
	}
	if r, ok := table[b]; ok {
		return r
	}
	return rune(b)
}

// utf16le encodes text as UTF-16LE, the format NSIS LicenseData reads as-is.
func utf16le(text string) []byte {
	out := make([]byte, 0, len(text)*2)
	for _, r := range text {
		if r > 0xffff {
			r = 0xfffd
		}
		out = append(out, byte(r), byte(r>>8))
	}
	return out
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

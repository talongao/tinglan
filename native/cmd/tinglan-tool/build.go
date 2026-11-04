package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// signingIdentity returns the codesign identity the Node scripts use: the
// HWMUSIC_MAC_SIGN_IDENTITY environment variable, else the fingerprint recorded
// in local-signing.json, else "" (which signMacOS turns into an ad-hoc "-").
func signingIdentity() string {
	if identity := os.Getenv("HWMUSIC_MAC_SIGN_IDENTITY"); identity != "" {
		return identity
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(config, "tinglan", "local-signing.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	fingerprint := jsonStringField(data, "fingerprint")
	if !isHexFingerprint(fingerprint) {
		return ""
	}
	return fingerprint
}

// isHexFingerprint accepts exactly 40 hex characters, the shape
// sign-macos.mjs validates before using the stored fingerprint.
func isHexFingerprint(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		case r >= 'A' && r <= 'F':
		default:
			return false
		}
	}
	return true
}

// signMacOS mirrors signMacOS() from native/scripts/sign-macos.mjs. It returns
// whether the target was signed with a real identity (used by the NOTICE text),
// and is a no-op on non-darwin hosts.
func signMacOS(target string, stderr io.Writer) (bool, error) {
	if runtime.GOOS != "darwin" {
		return false, nil
	}
	identity := signingIdentity()
	used := identity
	if used == "" {
		used = "-"
	}
	if err := runCommand(commandSpec{
		Name: "codesign",
		Args: []string{"--force", "--sign", used, "--identifier", appmetaBundleID(), target},
	}, io.Discard, stderr); err != nil {
		return false, err
	}
	if err := runCommand(commandSpec{
		Name: "codesign",
		Args: []string{"--verify", "--deep", "--strict", target},
	}, io.Discard, stderr); err != nil {
		return false, err
	}
	return identity != "", nil
}

// noticeText is the NOTICE.txt body written by build.mjs.
func noticeText(signed bool) string {
	prefix := "未签名测试构建"
	if signed {
		prefix = "本地证书签名、未公证测试构建"
	}
	return prefix + "；非官方项目；OAuth 身份不属于本项目。\n" +
		"Windows/Linux 的真实登录、音频输出和 UI 尚未实机验收。\n" +
		"不得将合成夹具当作私人账号证据。仅通过 GitHub Release 分发二进制。\n"
}

// buildBinaryFlags is the -ldflags value build.mjs uses per platform.
func buildBinaryFlags(platform string) string {
	if platform == "win32" {
		return "-s -w -H=windowsgui"
	}
	return "-s -w"
}

// buildPayload is the port of native/scripts/build.mjs: compile ./cmd/music
// into out/native/tinglan/<platform>-<arch>/ and write the release sidecars.
func buildPayload(root string, stdout, stderr io.Writer) error {
	output := payloadDir(root)
	if err := os.MkdirAll(output, 0o755); err != nil {
		return err
	}
	platform := nodePlatform()
	binary := filepath.Join(output, executableName(platform))
	goBin := goBinary()

	if platform == "win32" {
		if err := applyWindowsResources(root, goBin, stdout, stderr); err != nil {
			return err
		}
	}

	if err := runCommand(commandSpec{
		Dir:  filepath.Join(root, "native"),
		Name: goBin,
		Args: []string{"build", "-trimpath", "-ldflags=" + buildBinaryFlags(platform), "-o", binary, "./cmd/music"},
	}, stdout, stderr); err != nil {
		return err
	}
	signed, err := signMacOS(binary, stderr)
	if err != nil {
		return err
	}
	if err := copyFile(filepath.Join(root, "LICENSE"), filepath.Join(output, "LICENSE")); err != nil {
		return err
	}
	if err := writeNotices(root, filepath.Join(output, "THIRD_PARTY_NOTICES.txt"), goBin); err != nil {
		return err
	}
	if err := copyFile(filepath.Join(root, "build", "icon.LICENSE.txt"), filepath.Join(output, "icon.LICENSE.txt")); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(output, "NOTICE.txt"), []byte(noticeText(signed)), 0o644); err != nil {
		return err
	}
	digest, err := sha256Hex(binary)
	if err != nil {
		return err
	}
	sums := digest + "  " + filepath.Base(binary) + "\n"
	if err := writeFile(filepath.Join(output, "SHA256SUMS"), []byte(sums), 0o644); err != nil {
		return err
	}
	state := "unsigned"
	if signed {
		state = "signed, not notarized"
	}
	_, err = fmt.Fprintf(stdout, "Built local %s native binary: %s\n", state, output)
	return err
}

// applyWindowsResources ports the windres step of the old build.mjs: render the
// PNG icon into an .ico, wrap it in an .rc file and compile that into the
// arch-specific .syso the Go linker picks up.
func applyWindowsResources(root, goBin string, stdout, stderr io.Writer) error {
	assets := filepath.Join(root, "out", "native", "windows-resources")
	if err := os.MkdirAll(assets, 0o755); err != nil {
		return err
	}
	icon := filepath.Join(root, "build", "icon.png")
	ico := filepath.Join(assets, "app.ico")
	if err := runCommand(commandSpec{
		Dir:  filepath.Join(root, "native"),
		Name: goBin,
		Args: []string{"run", "./tools/windows-icon", icon, ico},
	}, stdout, stderr); err != nil {
		return fmt.Errorf("Windows icon: %w", err)
	}
	rc := filepath.Join(assets, "app.rc")
	line := "1 ICON \"" + strings.ReplaceAll(ico, "\\", "/") + "\"\n"
	if err := writeFile(rc, []byte(line), 0o644); err != nil {
		return err
	}
	windres := os.Getenv("HWMUSIC_WINDRES")
	if windres == "" {
		if cc := os.Getenv("CC"); cc != "" {
			windres = filepath.Join(filepath.Dir(cc), "windres.exe")
		} else {
			windres = "windres"
		}
	}
	target := filepath.Join(root, "native", "cmd", "music", "icon_windows_"+runtime.GOARCH+".syso")
	if err := runCommand(commandSpec{
		Name: windres,
		Args: []string{"-O", "coff", "-i", rc, "-o", target},
	}, stdout, stderr); err != nil {
		return fmt.Errorf("Windows resources (set HWMUSIC_WINDRES if windres is not on PATH): %w", err)
	}
	return nil
}

// jsonStringField extracts a top-level string field from a small JSON object
// without pulling in encoding/json's map ordering (used for the signing config).
func jsonStringField(data []byte, field string) string {
	text := string(data)
	needle := `"` + field + `"`
	index := strings.Index(text, needle)
	if index < 0 {
		return ""
	}
	rest := text[index+len(needle):]
	colon := strings.Index(rest, ":")
	if colon < 0 {
		return ""
	}
	rest = strings.TrimSpace(rest[colon+1:])
	if !strings.HasPrefix(rest, `"`) {
		return ""
	}
	rest = rest[1:]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}

func buildCommand(stdout, stderr io.Writer, _ []string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}
	return buildPayload(root, stdout, stderr)
}

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNSISScriptCoversInstallAndUninstall(t *testing.T) {
	cfg := nsisConfig{
		Icon:       `C:\repo\out\native\windows-resources\app.ico`,
		Name:       "听澜",
		Output:     `C:\repo\release\native\win32-x64\tinglan-1.0.0-x64-setup-unsigned.exe`,
		InstallDir: `$LOCALAPPDATA\Programs\Tinglan`,
		Version:    "1.0.0",
		Notice:     `C:\repo\out\native\tinglan\win32-x64\NOTICE.txt`,
		Payload:    `C:\repo\out\native\tinglan\win32-x64`,
		Files:      []string{"tinglan.exe", "LICENSE", "THIRD_PARTY_NOTICES.txt", "icon.LICENSE.txt", "NOTICE.txt", "SHA256SUMS"},
	}
	script := nsisScript(cfg)
	want := []string{
		"Unicode true",
		`!include "MUI2.nsh"`,
		`Name "听澜"`,
		`OutFile "C:\repo\release\native\win32-x64\tinglan-1.0.0-x64-setup-unsigned.exe"`,
		`InstallDir "$LOCALAPPDATA\Programs\Tinglan"`,
		`File "` + filepath.Join(cfg.Payload, "tinglan.exe") + `"`,
		`File "` + filepath.Join(cfg.Payload, "SHA256SUMS") + `"`,
		`WriteUninstaller "$INSTDIR\Uninstall.exe"`,
		`CreateShortcut "$SMPROGRAMS\Tinglan\听澜.lnk" "$INSTDIR\tinglan.exe"`,
		`WriteRegStr HKCU "Software\HuaweiMusicViva" "InstallDir" "$INSTDIR"`,
		`Delete "$INSTDIR\LICENSE"`,
		`RMDir "$INSTDIR"`,
		`!insertmacro MUI_LANGUAGE "SimpChinese"`,
	}
	for _, want := range want {
		if !strings.Contains(script, want) {
			t.Fatalf("NSIS script missing %q\n%s", want, script)
		}
	}
	if strings.Contains(script, "\ufffd") {
		t.Fatal("NSIS script must stay valid UTF-8 for /INPUTCHARSET UTF8")
	}
}

func TestNSISScriptEscapesDollarAndQuote(t *testing.T) {
	cfg := nsisConfig{Icon: `C:\a$b\icon.ico`, Name: `We"ird`, Output: "out.exe", InstallDir: "dir", Notice: "n", Payload: filepath.FromSlash("/tmp/p"), Files: []string{"a.exe"}}
	script := nsisScript(cfg)
	if !strings.Contains(script, `C:\a$$b\icon.ico`) {
		t.Fatalf("dollar not doubled: %s", script)
	}
	if !strings.Contains(script, `Name "We$\"ird"`) {
		t.Fatalf("quote not escaped: %s", script)
	}
}

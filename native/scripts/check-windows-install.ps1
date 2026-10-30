$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot/../..").Path
$package = Get-ChildItem "$root/release/native/win32-x64/*-setup-unsigned.exe" | Select-Object -First 1
if (!$package) { throw 'Installer missing' }
$target = Join-Path $env:RUNNER_TEMP ('tinglan-install-' + [guid]::NewGuid().ToString('N'))
$process = Start-Process $package.FullName -ArgumentList @('/S', "/D=$target") -Wait -PassThru
if ($process.ExitCode -ne 0) { throw "Install failed: $($process.ExitCode)" }
$name = ([char]0x542c).ToString() + [char]0x6f9c
$programs = Join-Path ([Environment]::GetFolderPath('Programs')) 'Tinglan'
$link = [System.IO.Path]::GetFullPath((Join-Path $programs "$name.lnk"))
if (!(Test-Path -LiteralPath $link)) { throw 'Unicode Start Menu shortcut missing' }
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($link)
if ([string]::IsNullOrWhiteSpace($shortcut.TargetPath)) {
    throw "Cannot read shortcut target from canonical path: $link (size $((Get-Item -LiteralPath $link).Length))"
}
$expected = Join-Path $target 'tinglan.exe'
Write-Output "Expected executable: $expected"
Write-Output "Shortcut executable: $($shortcut.TargetPath)"
# Resolve actual files rather than comparing DOS short paths with long paths.
Add-Type @'
using System;
using System.Text;
using System.Runtime.InteropServices;
using Microsoft.Win32.SafeHandles;
public static class InstalledPath {
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)]
    public static extern uint GetFinalPathNameByHandle(SafeFileHandle file, StringBuilder path, uint size, uint flags);
}
'@
function Get-CanonicalFilePath([string]$Path) {
    $stream = [System.IO.File]::OpenRead($Path)
    try {
        $buffer = [System.Text.StringBuilder]::new(32768)
        $length = [InstalledPath]::GetFinalPathNameByHandle($stream.SafeFileHandle, $buffer, $buffer.Capacity, 0)
        if ($length -eq 0 -or $length -ge $buffer.Capacity) { throw "Cannot resolve installed file: $Path" }
        return $buffer.ToString()
    } finally { $stream.Dispose() }
}
if ((Get-CanonicalFilePath $shortcut.TargetPath) -ne (Get-CanonicalFilePath $expected)) { throw 'Wrong shortcut executable' }
$built = Join-Path $root 'out/native/tinglan/win32-x64/tinglan.exe'
if ((Get-FileHash -LiteralPath $expected).Hash -ne (Get-FileHash -LiteralPath $built).Hash) { throw 'Installed executable differs from payload' }
if (!(Test-Path -LiteralPath $shortcut.TargetPath)) { throw 'Installed executable missing' }
# Do not launch the app: installation checks must never open a personal profile.
Write-Output 'Silent install and Unicode shortcut verified; application was not launched.'

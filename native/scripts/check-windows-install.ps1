$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot/../..").Path
$package = Get-ChildItem "$root/release/native/win32-x64/*-setup-unsigned.exe" | Select-Object -First 1
if (!$package) { throw 'Installer missing' }
$target = Join-Path $env:RUNNER_TEMP ('tinglan-install-' + [guid]::NewGuid().ToString('N'))
$name = ([char]0x542c).ToString() + [char]0x6f9c
$programs = Join-Path ([Environment]::GetFolderPath('Programs')) 'Tinglan'
New-Item -ItemType Directory -Force -Path $programs | Out-Null
$oldName = [System.Text.Encoding]::GetEncoding(1252).GetString([System.Text.Encoding]::UTF8.GetBytes($name))
$oldLink = Join-Path $programs ($oldName + '.lnk')
[System.IO.File]::WriteAllText($oldLink, 'synthetic legacy shortcut')
$process = Start-Process $package.FullName -ArgumentList @('/S', "/D=$target") -Wait -PassThru
if ($process.ExitCode -ne 0) { throw "Install failed: $($process.ExitCode)" }
if (Test-Path -LiteralPath $oldLink) { throw 'Legacy mojibake shortcut still visible' }
$backup = Get-ChildItem (Join-Path $target 'upgrade-backup') -Filter '*.bak'
if (!$backup) { throw 'Legacy shortcut was not preserved in upgrade backup' }
$link = [System.IO.Path]::GetFullPath((Join-Path $programs "$name.lnk"))
if (!(Test-Path -LiteralPath $link)) { throw 'Unicode Start Menu shortcut missing' }
# Read through IShellLinkW/IPersistFile: WScript's shortcut automation can
# return an empty target for a Unicode shortcut name on non-Chinese Windows.
Add-Type @'
using System;
using System.Text;
using System.Runtime.InteropServices;
using System.Runtime.InteropServices.ComTypes;
[ComImport, Guid("00021401-0000-0000-C000-000000000046")]
public class ShellLinkObject { }
[ComImport, Guid("000214F9-0000-0000-C000-000000000046"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
public interface IShellLinkUnicode {
    void GetPath([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int max, IntPtr data, uint flags);
}
public static class ShortcutReader {
    public static string Read(string file) {
        var link = new ShellLinkObject();
        try {
            ((IPersistFile)link).Load(file, 0);
            var path = new StringBuilder(32768);
            ((IShellLinkUnicode)link).GetPath(path, path.Capacity, IntPtr.Zero, 4);
            return path.ToString();
        } finally { Marshal.FinalReleaseComObject(link); }
    }
}
'@
$diagnostics = Join-Path $root 'out/native/qa'
New-Item -ItemType Directory -Force -Path $diagnostics | Out-Null
Copy-Item -LiteralPath $link -Destination (Join-Path $diagnostics 'installed-shortcut.lnk')
$shortcutTarget = [ShortcutReader]::Read($link)
if ([string]::IsNullOrWhiteSpace($shortcutTarget)) { throw 'IShellLinkW returned an empty target' }
$expected = Join-Path $target 'tinglan.exe'
Write-Output "Expected executable: $expected"
Write-Output "Shortcut executable: $shortcutTarget"
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
if ((Get-CanonicalFilePath $shortcutTarget) -ne (Get-CanonicalFilePath $expected)) { throw 'Wrong shortcut executable' }
$built = Join-Path $root 'out/native/tinglan/win32-x64/tinglan.exe'
if ((Get-FileHash -LiteralPath $expected).Hash -ne (Get-FileHash -LiteralPath $built).Hash) { throw 'Installed executable differs from payload' }
if (!(Test-Path -LiteralPath $shortcutTarget)) { throw 'Installed executable missing' }
# Do not launch the app: installation checks must never open a personal profile.
Write-Output 'Silent install and Unicode shortcut verified; application was not launched.'

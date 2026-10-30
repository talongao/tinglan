$ErrorActionPreference = 'Stop'
$root = (Resolve-Path "$PSScriptRoot/../..").Path
$package = Get-ChildItem "$root/release/native/win32-x64/*-setup-unsigned.exe" | Select-Object -First 1
if (!$package) { throw 'Installer missing' }
$target = Join-Path $env:RUNNER_TEMP ('tinglan-install-' + [guid]::NewGuid().ToString('N'))
$process = Start-Process $package.FullName -ArgumentList @('/S', "/D=$target") -Wait -PassThru
if ($process.ExitCode -ne 0) { throw "Install failed: $($process.ExitCode)" }
$name = ([char]0x542c).ToString() + [char]0x6f9c
$link = Join-Path ([Environment]::GetFolderPath('Programs')) "Tinglan/$name.lnk"
if (!(Test-Path -LiteralPath $link)) { throw 'Unicode Start Menu shortcut missing' }
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($link)
if ($shortcut.TargetPath -ne (Join-Path $target 'tinglan.exe')) { throw 'Wrong shortcut executable' }
if (!(Test-Path -LiteralPath $shortcut.TargetPath)) { throw 'Installed executable missing' }
# Do not launch the app: installation checks must never open a personal profile.
Write-Output 'Silent install and Unicode shortcut verified; application was not launched.'

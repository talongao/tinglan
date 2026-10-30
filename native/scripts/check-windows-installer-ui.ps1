$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName UIAutomationClient
Add-Type -AssemblyName UIAutomationTypes
$root = (Resolve-Path "$PSScriptRoot/../..").Path
$package = Get-ChildItem "$root/release/native/win32-x64/*-setup-unsigned.exe" | Select-Object -First 1
$target = Join-Path $env:RUNNER_TEMP ('tinglan-ui-install-' + [guid]::NewGuid().ToString('N'))
$process = Start-Process $package.FullName -ArgumentList @("/D=$target") -PassThru
function Get-Window {
    $condition = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::ProcessIdProperty, $process.Id)
    return [System.Windows.Automation.AutomationElement]::RootElement.FindFirst([System.Windows.Automation.TreeScope]::Children, $condition)
}
function Get-Control([string]$id) {
    $window = Get-Window
    if (!$window) { return $null }
    $condition = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::AutomationIdProperty, $id)
    return $window.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $condition)
}
function Click-Next {
    $button = Get-Control '1'
    if (!$button -or !$button.Current.IsEnabled) { throw 'Installer next button unavailable' }
    $button.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
    Start-Sleep -Milliseconds 500
}
try {
    $deadline = (Get-Date).AddSeconds(20)
    while (!(Get-Control '1') -and (Get-Date) -lt $deadline) { Start-Sleep -Milliseconds 100 }
    Click-Next
    # Read the actual displayed RichEdit content, not the source NOTICE file.
    $all = (Get-Window).FindAll([System.Windows.Automation.TreeScope]::Descendants, [System.Windows.Automation.Condition]::TrueCondition)
    $license = ''
    foreach ($element in $all) {
        $pattern = $null
        if ($element.TryGetCurrentPattern([System.Windows.Automation.TextPattern]::Pattern, [ref]$pattern)) {
            $text = $pattern.DocumentRange.GetText(-1)
            if ($text.Contains('OAuth')) { $license = $text }
        }
    }
    $name = ([char]0x542c).ToString() + [char]0x6f9c
    if (!$license.Contains($name) -or !$license.Contains('OAuth')) { throw 'Displayed license is missing Unicode text' }
    Click-Next
    Click-Next
    $deadline = (Get-Date).AddSeconds(60)
    $run = $null
    while ((Get-Date) -lt $deadline) {
        $window = Get-Window
        if ($window) {
            $condition = [System.Windows.Automation.PropertyCondition]::new([System.Windows.Automation.AutomationElement]::ControlTypeProperty, [System.Windows.Automation.ControlType]::CheckBox)
            $run = $window.FindFirst([System.Windows.Automation.TreeScope]::Descendants, $condition)
            if ($run -and $run.Current.Name.Contains($name)) { break }
        }
        Start-Sleep -Milliseconds 200
    }
    if (!$run -or !$run.Current.Name.Contains($name)) { throw 'Finish page has no launch checkbox' }
    $toggle = $run.GetCurrentPattern([System.Windows.Automation.TogglePattern]::Pattern)
    if ($toggle.Current.ToggleState -ne [System.Windows.Automation.ToggleState]::Off) { $toggle.Toggle() }
    Click-Next
    if (!$process.WaitForExit(10000)) { throw 'Installer did not finish' }
    Write-Output 'Actual installer UI: Unicode license and launch checkbox verified; app launch disabled.'
} finally {
    if (!$process.HasExited) { $process.Kill() }
}

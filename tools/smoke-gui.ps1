param(
    [Parameter(Mandatory=$true)][string]$Exe,
    [Parameter(Mandatory=$true)][string]$Source,
    [int]$ExpectedCount = 3
)
$ErrorActionPreference = 'Stop'
Add-Type @'
using System;
using System.Text;
using System.Runtime.InteropServices;
public static class ReportsUI {
 [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string cls,string title);
 [DllImport("user32.dll")] public static extern IntPtr GetDlgItem(IntPtr hwnd,int id);
 [DllImport("user32.dll")] public static extern bool IsWindowEnabled(IntPtr hwnd);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern IntPtr SendMessage(IntPtr hwnd,int msg,IntPtr wp,IntPtr lp);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern bool SetWindowText(IntPtr hwnd,string text);
 [DllImport("user32.dll",EntryPoint="SendMessageW",CharSet=CharSet.Unicode)] public static extern IntPtr SendText(IntPtr hwnd,int msg,IntPtr wp,string text);
 [DllImport("user32.dll",CharSet=CharSet.Unicode)] public static extern int GetWindowText(IntPtr hwnd,StringBuilder text,int count);
}
'@
$dir = Join-Path (Get-Location) ('output\ui-' + [guid]::NewGuid().ToString('N'))
$arguments = '--gui --source "' + $Source + '" --out "' + $dir + '"'
$process = Start-Process -FilePath $Exe -ArgumentList $arguments -WindowStyle Hidden -PassThru
function Wait-Condition([scriptblock]$Condition,[string]$Message) {
    $deadline = [DateTime]::UtcNow.AddSeconds(45)
    do {
        if (& $Condition) {return}
        if ($process.HasExited) {
            if (& $Condition) {return}
            throw "Process exited: $($process.ExitCode)"
        }
        Start-Sleep -Milliseconds 100
    } while ([DateTime]::UtcNow -lt $deadline)
    throw $Message
}
try {
    $script:hwnd = [IntPtr]::Zero
    Wait-Condition { $script:hwnd = [ReportsUI]::FindWindow('ESReportsLegacy','Паспорта присоединений — Excel'); $script:hwnd -ne [IntPtr]::Zero } 'Window missing'
    $list = [ReportsUI]::GetDlgItem($hwnd,103)
    Wait-Condition { [ReportsUI]::SendMessage($list,0x1004,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32() -eq $ExpectedCount } 'Wrong import count'
    $combo = [ReportsUI]::GetDlgItem($hwnd,102)
    $objectCount = [ReportsUI]::SendMessage($combo,0x146,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32()
    if ($ExpectedCount -eq 373 -and $objectCount -ne 19) {throw 'Wrong object count'}
    [void][ReportsUI]::SendMessage($combo,0x14e,[IntPtr]1,[IntPtr]::Zero)
    [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr](102 -bor (1 -shl 16)),[IntPtr]::Zero)
    if ($ExpectedCount -gt 3) {
        $filteredCount = [ReportsUI]::SendMessage($list,0x1004,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32()
        if ($filteredCount -le 0 -or $filteredCount -ge $ExpectedCount) {throw 'Object filter failed'}
    }
    [void][ReportsUI]::SendMessage($combo,0x14e,[IntPtr]::Zero,[IntPtr]::Zero)
    [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr](102 -bor (1 -shl 16)),[IntPtr]::Zero)
    [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr]105,[IntPtr]::Zero) # Deselect all.
    $search = [ReportsUI]::GetDlgItem($hwnd,101)
    [void][ReportsUI]::SendText($search,0x0c,[IntPtr]::Zero,'01111479')
    Wait-Condition { [ReportsUI]::SendMessage($list,0x1004,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32() -eq 1 } 'Search failed'
    [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr]104,[IntPtr]::Zero) # Select visible.
    [void][ReportsUI]::SendText($search,0x0c,[IntPtr]::Zero,'') # Selection survives filter change.
    Wait-Condition { [ReportsUI]::SendMessage($list,0x1004,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32() -eq $ExpectedCount } 'Filter reset failed'
    [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr]109,[IntPtr]::Zero)
    Wait-Condition { (Get-ChildItem -LiteralPath $dir -Filter '*.xlsx' -ErrorAction SilentlyContinue).Count -eq 1 } 'Selected export failed'
    Wait-Condition { [ReportsUI]::IsWindowEnabled([ReportsUI]::GetDlgItem($hwnd,109)) } 'Queue did not finish'
    $files = @(Get-ChildItem -LiteralPath $dir -Filter '*.xlsx')
    if ($files[0].Name -notmatch 'РП-17') {throw 'Wrong selected report'}
    [void][ReportsUI]::SendMessage($hwnd,0x10,[IntPtr]::Zero,[IntPtr]::Zero)
    Wait-Condition { $process.HasExited } 'Window did not close'
    Write-Output "PASS native GUI: $ExpectedCount imported, search, select visible, filter reset, one report; $dir"
} finally {
    if (-not $process.HasExited) {Stop-Process -Id $process.Id}
}

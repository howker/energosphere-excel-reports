param(
    [Parameter(Mandatory=$true)][string]$Exe,
    [Parameter(Mandatory=$true)][string]$Source,
    [int]$ExpectedCount = 3,
    [switch]$ExportObject,
    [switch]$CheckBatchOptions
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
 [DllImport("user32.dll",EntryPoint="SendMessageW",CharSet=CharSet.Unicode)] public static extern IntPtr ReadText(IntPtr hwnd,int msg,IntPtr wp,StringBuilder text);
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
    if ($ExportObject) {
        $index = -1
        for ($i=1; $i -lt $objectCount; $i++) {
            $name = [Text.StringBuilder]::new(512)
            [void][ReportsUI]::ReadText($combo,0x148,[IntPtr]$i,$name)
            if ($name.ToString() -match 'Подземное хранилище') {$index=$i;break}
        }
        if ($index -lt 0) {throw 'Underground storage missing'}
        [void][ReportsUI]::SendMessage($combo,0x14e,[IntPtr]$index,[IntPtr]::Zero)
        [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr](102 -bor (1 -shl 16)),[IntPtr]::Zero)
        $expectedFiles = [ReportsUI]::SendMessage($list,0x1004,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32()
        # Original all-source checkboxes remain set: only the active object may export.
        if ($expectedFiles -le 0 -or $expectedFiles -ge $ExpectedCount) {throw 'Object scope invalid'}
    } else {
    [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr]105,[IntPtr]::Zero) # Deselect all.
    $search = [ReportsUI]::GetDlgItem($hwnd,101)
    [void][ReportsUI]::SendText($search,0x0c,[IntPtr]::Zero,'01111479')
    Wait-Condition { [ReportsUI]::SendMessage($list,0x1004,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32() -eq 1 } 'Search failed'
    [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr]104,[IntPtr]::Zero) # Select visible.
    [void][ReportsUI]::SendText($search,0x0c,[IntPtr]::Zero,'') # Selection survives filter change.
    Wait-Condition { [ReportsUI]::SendMessage($list,0x1004,[IntPtr]::Zero,[IntPtr]::Zero).ToInt32() -eq $ExpectedCount } 'Filter reset failed'
    $expectedFiles=1
    }
    if ($CheckBatchOptions) {
        [void][ReportsUI]::SendMessage([ReportsUI]::GetDlgItem($hwnd,112),0xf1,[IntPtr]1,[IntPtr]::Zero)
        [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr]112,[IntPtr]::Zero)
        [void][ReportsUI]::SendText([ReportsUI]::GetDlgItem($hwnd,113),0x0c,[IntPtr]::Zero,'08.10.2026')
        [void][ReportsUI]::SendText([ReportsUI]::GetDlgItem($hwnd,115),0x0c,[IntPtr]::Zero,'10.10.2026')
        [void][ReportsUI]::SendText([ReportsUI]::GetDlgItem($hwnd,114),0x0c,[IntPtr]::Zero,"Инженер АОСС; С.Е. Кудряшов`r`nНачальник; И.И. Иванов")
    }
    [void][ReportsUI]::SendMessage($hwnd,0x111,[IntPtr]109,[IntPtr]::Zero)
    Wait-Condition { (Get-ChildItem -LiteralPath $dir -Filter '*.xlsx' -ErrorAction SilentlyContinue).Count -eq $expectedFiles } 'Selected export failed'
    Wait-Condition { [ReportsUI]::IsWindowEnabled([ReportsUI]::GetDlgItem($hwnd,109)) } 'Queue did not finish'
    $files = @(Get-ChildItem -LiteralPath $dir -Filter '*.xlsx')
    if (-not $ExportObject -and $files[0].Name -notmatch 'РП-17') {throw 'Wrong selected report'}
    if ($ExportObject -and @($files | Where-Object Name -NotMatch 'Подземное хранилище').Count -ne 0) {throw 'Hidden object exported'}
    if ($CheckBatchOptions -and @($files | Where-Object Name -Match '^ООО ').Count -ne 0) {throw 'Company not omitted'}
    if ($CheckBatchOptions) {
        Add-Type -AssemblyName System.IO.Compression.FileSystem
        $zip = [IO.Compression.ZipFile]::OpenRead($files[0].FullName)
        $reader = $null
        try {
            $reader = [IO.StreamReader]::new($zip.GetEntry('xl/worksheets/sheet2.xml').Open())
            [xml]$sheetXml = $reader.ReadToEnd()
            $dateCell = $sheetXml.SelectSingleNode("//*[local-name()='c' and @r='AC5']//*[local-name()='t']")
            if ($null -eq $dateCell -or $dateCell.InnerText -ne '10.10.2026') {throw 'VAF verification date not exported from GUI'}
        } finally {
            if ($null -ne $reader) {$reader.Dispose()}
            $zip.Dispose()
        }
    }
    [void][ReportsUI]::SendMessage($hwnd,0x10,[IntPtr]::Zero,[IntPtr]::Zero)
    Wait-Condition { $process.HasExited } 'Window did not close'
    Write-Output "PASS native GUI: $ExpectedCount imported, $expectedFiles exported; object=$ExportObject options=$CheckBatchOptions; $dir"
} finally {
    if (-not $process.HasExited) {Stop-Process -Id $process.Id}
}

[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'core-launcher.ps1')
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
Add-Type -AssemblyName System.Net.Http
[Windows.Forms.Application]::EnableVisualStyles()

# Prevent two launcher windows from racing to start the same core.
$mutex = New-Object Threading.Mutex($false, 'Local\YuiCoreLauncher')
if (-not $mutex.WaitOne(0)) {
    [Windows.Forms.MessageBox]::Show('Окно запуска Yui уже открыто.', 'Yui') | Out-Null
    $mutex.Dispose()
    return
}
$form = New-Object Windows.Forms.Form
$form.Text = 'Yui — запуск ядра'
$form.ClientSize = New-Object Drawing.Size(510, 245)
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.Font = New-Object Drawing.Font('Segoe UI', 10)
$title = New-Object Windows.Forms.Label
$title.Text = 'Yui Core'
$title.Font = New-Object Drawing.Font('Segoe UI', 19, [Drawing.FontStyle]::Bold)
$title.SetBounds(24, 18, 460, 42)
$status = New-Object Windows.Forms.Label
$status.SetBounds(24, 70, 460, 52)
$status.Text = 'Проверка состояния ядра…'
$start = New-Object Windows.Forms.Button
$start.Text = 'Включить ядро'
$start.SetBounds(24, 132, 215, 42)
$start.Enabled = $false
$open = New-Object Windows.Forms.Button
$open.Text = 'Открыть интерфейс'
$open.SetBounds(253, 132, 230, 42)
$open.Enabled = $false
$note = New-Object Windows.Forms.Label
$note.Text = 'Закрытие этого окна не выключает ядро.'
$note.SetBounds(24, 193, 460, 30)
$form.Controls.AddRange(@($title, $status, $start, $open, $note))
$http = New-Object Net.Http.HttpClient
$http.Timeout = [TimeSpan]::FromSeconds(2)
$script:probe = $null
$script:probePid = 0
$script:startedAt = $null
$script:launchError = $null
$script:wasRunning = $false
$timer = New-Object Windows.Forms.Timer
$timer.Interval = 1000
$timer.Add_Tick({
    try {
        $process = Get-YuiCoreProcess -Root $root
        if (-not $process) {
            if ($script:probe) {
                if (-not $script:probe.IsCompleted) { $http.CancelPendingRequests() }
                if ($script:probe.Status -eq 'RanToCompletion') { $script:probe.Result.Dispose() }
                $script:probe = $null
            }
            $start.Enabled = $true
            $start.Text = 'Включить ядро'
            $open.Enabled = $false
            if ($script:launchError) { $status.Text = $script:launchError }
            elseif ($script:wasRunning) { $status.Text = 'Ядро остановлено. Журнал: .cache\core-launcher-error.log' }
            else { $status.Text = 'Ядро выключено' }
            return
        }
        $script:wasRunning = $true
        $start.Enabled = $false
        $start.Text = 'Ядро запущено'
        if ($script:probe -and $script:probe.IsCompleted) {
            $ready = $false
            if ($script:probe.Status -eq 'RanToCompletion') {
                $ready = $script:probe.Result.IsSuccessStatusCode -and $script:probePid -eq $process.Id
                $script:probe.Result.Dispose()
            }
            $script:probe = $null
            $open.Enabled = $ready
            if ($ready) { $status.Text = 'Ядро включено и готово к работе' }
            elseif ($script:startedAt -and ((Get-Date) - $script:startedAt).TotalSeconds -gt 60) {
                $status.Text = 'Ядро не отвечает. Журнал: .cache\core-launcher-error.log'
            } else { $status.Text = 'Ядро запускается…' }
        }
        if (-not $script:probe) {
            $page = [Uri](Get-YuiCorePage -Root $root -Process $process)
            $script:probePid = $process.Id
            $script:probe = $http.GetAsync($page.GetLeftPart([UriPartial]::Authority) + '/healthz')
        }
    } catch {
        $status.Text = "Ошибка: $($_.Exception.Message)"
        $open.Enabled = $false
    }
})
$start.Add_Click({
    $start.Enabled = $false
    $script:launchError = $null
    try {
        $null = Start-YuiCore -Root $root
        $script:startedAt = Get-Date
        $script:wasRunning = $true
        $status.Text = 'Ядро запускается…'
    } catch {
        $script:launchError = $_.Exception.Message
        $status.Text = $script:launchError
        $start.Enabled = $true
    }
})
$open.Add_Click({
    try {
        $process = Get-YuiCoreProcess -Root $root
        if (-not $process) { throw 'Ядро уже остановлено.' }
        Start-Process (Get-YuiCorePage -Root $root -Process $process)
    } catch { $status.Text = $_.Exception.Message }
})
try {
    $timer.Start()
    [Windows.Forms.Application]::Run($form)
} finally {
    $timer.Stop()
    $timer.Dispose()
    $http.Dispose()
    $form.Dispose()
    $mutex.ReleaseMutex()
    $mutex.Dispose()
}

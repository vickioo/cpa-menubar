Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$dashboardUrl = "http://127.0.0.1:8765/"
$script:serverProcess = $null
$script:tunnelProcess = $null

function Test-LocalPort([int]$Port) {
    $connection = Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue
    return $null -ne $connection
}

function Stop-LocalPort([int]$Port) {
    Get-NetTCPConnection -LocalPort $Port -State Listen -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty OwningProcess -Unique |
        ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
}

function Start-DashboardServices {
    if (-not (Test-LocalPort 18330)) {
        $script:tunnelProcess = Start-Process ssh -ArgumentList "-N", "-L", "18330:127.0.0.1:18330", "CC" -WindowStyle Hidden -PassThru
        Start-Sleep -Seconds 2
    }

    if (-not (Test-LocalPort 8765)) {
        $token = (& ssh CC 'sudo sed -n "s/^CPA_DESKTOP_TOKEN=//p" /opt/cpa-sub2-bridge/bridge.env').Trim()
        if ([string]::IsNullOrWhiteSpace($token)) {
            throw "无法读取 Bridge 连接令牌"
        }
        $node = (Get-Command node -ErrorAction Stop).Source
        $startInfo = [System.Diagnostics.ProcessStartInfo]::new()
        $startInfo.FileName = $node
        $startInfo.WorkingDirectory = $root
        $startInfo.Arguments = "preview\server.mjs"
        $startInfo.UseShellExecute = $false
        $startInfo.CreateNoWindow = $true
        $startInfo.EnvironmentVariables["BRIDGE_URL"] = "http://127.0.0.1:18330"
        $startInfo.EnvironmentVariables["BRIDGE_TOKEN"] = $token
        $script:serverProcess = [System.Diagnostics.Process]::Start($startInfo)
        Start-Sleep -Seconds 2
    }

    $summary = Invoke-RestMethod "http://127.0.0.1:8765/api/summary"
    if ($summary.source -ne "sub2") {
        throw "Bridge 返回了非 Sub2 数据源"
    }
}

function Open-Dashboard {
    Start-Process $dashboardUrl
}

function Reconnect-Dashboard {
    Stop-LocalPort 8765
    Stop-LocalPort 18330
    Start-Sleep -Milliseconds 500
    Start-DashboardServices
    $notifyIcon.ShowBalloonTip(1500, "Sub2 账号池", "连接已刷新", [System.Windows.Forms.ToolTipIcon]::Info)
}

try {
    Start-DashboardServices
} catch {
    $_ | Out-String | Set-Content (Join-Path $PSScriptRoot "tray-error.log") -Encoding UTF8
    [System.Windows.Forms.MessageBox]::Show($_.Exception.Message, "Sub2 账号池启动失败", "OK", "Error")
    exit 1
}

$menu = [System.Windows.Forms.ContextMenuStrip]::new()
$openItem = $menu.Items.Add("打开看板")
$refreshItem = $menu.Items.Add("重新连接")
$menu.Items.Add("-") | Out-Null
$exitItem = $menu.Items.Add("退出")

$notifyIcon = [System.Windows.Forms.NotifyIcon]::new()
$notifyIcon.Icon = [System.Drawing.SystemIcons]::Information
$notifyIcon.Text = "Sub2 账号池"
$notifyIcon.ContextMenuStrip = $menu
$notifyIcon.Visible = $true

$openItem.Add_Click({ Open-Dashboard })
$refreshItem.Add_Click({
    try { Reconnect-Dashboard } catch {
        $notifyIcon.ShowBalloonTip(2500, "Sub2 账号池", $_.Exception.Message, [System.Windows.Forms.ToolTipIcon]::Error)
    }
})
$notifyIcon.Add_DoubleClick({ Open-Dashboard })
$exitItem.Add_Click({
    $notifyIcon.Visible = $false
    $notifyIcon.Dispose()
    if ($script:serverProcess -and -not $script:serverProcess.HasExited) { $script:serverProcess.Kill() }
    if ($script:tunnelProcess -and -not $script:tunnelProcess.HasExited) { $script:tunnelProcess.Kill() }
    [System.Windows.Forms.Application]::ExitThread()
})

$notifyIcon.ShowBalloonTip(1500, "Sub2 账号池", "托盘程序已启动，双击图标打开看板", [System.Windows.Forms.ToolTipIcon]::Info)
[System.Windows.Forms.Application]::Run()

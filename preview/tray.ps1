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
    Update-TraySummary
    $notifyIcon.ShowBalloonTip(1500, "Sub2 账号池", "连接已刷新", [System.Windows.Forms.ToolTipIcon]::Info)
}

function Update-TraySummary {
    $summary = Invoke-RestMethod "http://127.0.0.1:8765/api/summary"
    $accountResponse = Invoke-RestMethod "http://127.0.0.1:8765/api/accounts"
    $poolResponse = Invoke-RestMethod "http://127.0.0.1:8765/api/pools"
    $requests = ($accountResponse.accounts | Measure-Object -Property recent_requests -Sum).Sum
    $errors = ($accountResponse.accounts | Measure-Object -Property recent_errors -Sum).Sum
    $success = if (($requests + $errors) -gt 0) { [math]::Round($requests / ($requests + $errors) * 100) } else { 100 }
    $validItem.Text = "有效账号：$($summary.valid_accounts)"
    $usageItem.Text = "24h 请求：$requests · 成功率：$success%"
    foreach ($pool in $poolResponse.pools) {
        $remaining = [math]::Max(0, $pool.weekly_limit_usd - $pool.weekly_usage_usd)
        $percent = if ($pool.weekly_limit_usd -gt 0) { [math]::Round($remaining / $pool.weekly_limit_usd * 100) } else { 0 }
        if ($pool.id -eq "13") { $pool0703Item.Text = "0703 20X：剩余 $percent%（" + '$' + "$([math]::Round($remaining, 2))）" }
        if ($pool.id -eq "12") { $poolFuItem.Text = "福CCC：剩余 $percent%（" + '$' + "$([math]::Round($remaining, 2))）" }
    }
    $notifyIcon.Text = "Sub2 · $($summary.valid_accounts) 有效 · $success%"
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
$validItem = $menu.Items.Add("有效账号：-")
$usageItem = $menu.Items.Add("24h 请求：-")
$pool0703Item = $menu.Items.Add("0703 20X：加载中")
$poolFuItem = $menu.Items.Add("福CCC：加载中")
$validItem.Enabled = $false
$usageItem.Enabled = $false
$pool0703Item.Enabled = $false
$poolFuItem.Enabled = $false
$menu.Items.Add("-") | Out-Null
$exitItem = $menu.Items.Add("退出")

$notifyIcon = [System.Windows.Forms.NotifyIcon]::new()
$notifyIcon.Icon = [System.Drawing.SystemIcons]::Information
$notifyIcon.Text = "Sub2 账号池"
$notifyIcon.ContextMenuStrip = $menu
$notifyIcon.Visible = $true

$refreshTimer = [System.Windows.Forms.Timer]::new()
$refreshTimer.Interval = 60000
$refreshTimer.Add_Tick({ try { Update-TraySummary } catch {} })
$refreshTimer.Start()
try { Update-TraySummary } catch {}

$openItem.Add_Click({ Open-Dashboard })
$refreshItem.Add_Click({
    try { Reconnect-Dashboard } catch {
        $notifyIcon.ShowBalloonTip(2500, "Sub2 账号池", $_.Exception.Message, [System.Windows.Forms.ToolTipIcon]::Error)
    }
})
$notifyIcon.Add_DoubleClick({ Open-Dashboard })
$exitItem.Add_Click({
    $notifyIcon.Visible = $false
    $refreshTimer.Stop()
    $refreshTimer.Dispose()
    $notifyIcon.Dispose()
    if ($script:serverProcess -and -not $script:serverProcess.HasExited) { $script:serverProcess.Kill() }
    if ($script:tunnelProcess -and -not $script:tunnelProcess.HasExited) { $script:tunnelProcess.Kill() }
    [System.Windows.Forms.Application]::ExitThread()
})

$notifyIcon.ShowBalloonTip(1500, "Sub2 账号池", "托盘程序已启动，双击图标打开看板", [System.Windows.Forms.ToolTipIcon]::Info)
[System.Windows.Forms.Application]::Run()

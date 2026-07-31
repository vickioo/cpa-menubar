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

function Refresh-AccountStatus {
    $refreshAccountsItem.Enabled = $false
    try {
        $result = Invoke-RestMethod "http://127.0.0.1:8765/api/refresh" -Method Post
        Update-TraySummary
        $message = "状态刷新完成：$($result.succeeded) 存活，$($result.unauthorized) 失效，$($result.failed) 异常，$($result.reset_credits_read) 个账号读到重置次数"
        $notifyIcon.ShowBalloonTip(2200, "Sub2 账号池", $message, [System.Windows.Forms.ToolTipIcon]::Info)
    } finally {
        $refreshAccountsItem.Enabled = $true
    }
}

function Update-TraySummary {
    $summary = Invoke-RestMethod "http://127.0.0.1:8765/api/summary"
    $accountResponse = Invoke-RestMethod "http://127.0.0.1:8765/api/accounts"
    $poolResponse = Invoke-RestMethod "http://127.0.0.1:8765/api/pools"
    $quotaReadable = @($accountResponse.accounts | Where-Object { $null -ne $_.weekly_used_percent }).Count
    $oauthAccounts = @($accountResponse.accounts | Where-Object { $_.authorization_type -eq "oauth" })
    $aliveAccounts = @($oauthAccounts | Where-Object { $_.authorization_alive -eq $true }).Count
    $checkedAccounts = @($oauthAccounts | Where-Object { $null -ne $_.authorization_alive }).Count
    $resetAccounts = @($oauthAccounts | Where-Object { $null -ne $_.reset_credits_available })
    $resetTotal = ($resetAccounts | Measure-Object -Property reset_credits_available -Sum).Sum
    $validItem.Text = "有效账号：$($summary.valid_accounts)"
    $usageItem.Text = "原生周限：$quotaReadable / $(@($accountResponse.accounts).Count) 个可读"
    $authItem.Text = "授权存活：$aliveAccounts / $checkedAccounts 已探测"
    $resetItem.Text = if ($resetAccounts.Count -gt 0) { "可用重置：$resetTotal 次 / $($resetAccounts.Count) 个账号" } else { "可用重置：尚未刷新" }
    foreach ($pool in $poolResponse.pools) {
        $usedPercent = if ($null -ne $pool.weekly_used_percent) { [math]::Round($pool.weekly_used_percent) } else { $null }
        $remainingPercent = if ($null -ne $usedPercent) { [math]::Max(0, 100 - $usedPercent) } else { $null }
        if ($pool.id -eq "0703") {
            $reset = if ($pool.weekly_reset_at) { ([datetimeoffset]::Parse($pool.weekly_reset_at)).ToLocalTime().ToString("MM-dd HH:mm") } else { "恢复时间暂无" }
            $pool0703Item.Text = if ($null -ne $remainingPercent) { "0703 20X：7D剩余 $remainingPercent% · $reset" } else { "0703 20X：额度暂无" }
        }
        if ($pool.id -eq "fuccc") {
            if ($null -ne $pool.weekly_limit -and $null -ne $pool.weekly_usage) {
                $remaining = [math]::Max(0, $pool.weekly_limit - $pool.weekly_usage)
                $poolFuItem.Text = "福CCC：综合剩余 $([math]::Round($remaining, 1)) / $([math]::Round($pool.weekly_limit, 1))"
            } else {
                $poolFuItem.Text = "福CCC：额度暂无"
            }
        }
    }
    $notifyIcon.Text = "Sub2 · $($summary.valid_accounts) 正常 · $quotaReadable 个周限可读"
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
$refreshAccountsItem = $menu.Items.Add("刷新授权与额度")
$reconnectItem = $menu.Items.Add("重新连接")
$menu.Items.Add("-") | Out-Null
$validItem = $menu.Items.Add("有效账号：-")
$usageItem = $menu.Items.Add("原生周限：-")
$authItem = $menu.Items.Add("授权存活：-")
$resetItem = $menu.Items.Add("可用重置：-")
$pool0703Item = $menu.Items.Add("0703 20X：加载中")
$poolFuItem = $menu.Items.Add("福CCC：加载中")
$validItem.Enabled = $false
$usageItem.Enabled = $false
$authItem.Enabled = $false
$resetItem.Enabled = $false
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
$refreshAccountsItem.Add_Click({
    try { Refresh-AccountStatus } catch {
        $notifyIcon.ShowBalloonTip(2500, "Sub2 账号池", $_.Exception.Message, [System.Windows.Forms.ToolTipIcon]::Error)
    }
})
$reconnectItem.Add_Click({
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

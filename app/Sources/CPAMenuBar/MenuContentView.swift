import SwiftUI

private enum DashboardTab: String, CaseIterable, Identifiable {
    case overview
    case accounts
    case xai

    var id: String { rawValue }

    var title: String {
        switch self {
        case .overview: "总览"
        case .accounts: "账号"
        case .xai: "xAI"
        }
    }

    var symbol: String {
        switch self {
        case .overview: "rectangle.grid.2x2"
        case .accounts: "person.2"
        case .xai: "bolt.horizontal.circle"
        }
    }
}

struct MenuContentView: View {
    @ObservedObject var model: AppModel
    @State private var selectedTab: DashboardTab = .overview
    @State private var accountFilter: AccountListFilter = .all
    @State private var expandedAccountIDs: Set<String> = []
    @State private var showTokenClearConfirmation = false
    @State private var resetAccount: Account?

    var body: some View {
        VStack(spacing: 0) {
            header
                .padding(.horizontal, 16)
                .padding(.vertical, 12)

            Divider()

            if model.showSettings || !model.isConfigured {
                ScrollView {
                    settings
                        .padding(16)
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                tabPicker
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)

                Divider()

                tabContent
                    .frame(maxWidth: .infinity, maxHeight: .infinity)

                Divider()

                footer
                    .padding(.horizontal, 16)
                    .padding(.vertical, 10)
            }
        }
        .frame(width: 400, height: 560)
        .alert("确认清除本机 Token？", isPresented: $showTokenClearConfirmation) {
            Button("取消", role: .cancel) {}
            Button("清除 Token", role: .destructive, action: model.clearToken)
        } message: {
            Text("清除后应用将立即断开 CPA，重新连接需要再次输入 Desktop Token。")
        }
        .confirmationDialog(
            "使用 1 张重置卡？",
            isPresented: Binding(
                get: { resetAccount != nil },
                set: { if !$0 { resetAccount = nil } }
            ),
            titleVisibility: .visible
        ) {
            Button("取消", role: .cancel) { resetAccount = nil }
            Button("确认使用", role: .destructive) {
                guard let account = resetAccount else { return }
                resetAccount = nil
                Task { await model.consumeResetCredit(for: account) }
            }
        } message: {
            Text("这会消耗 1 张重置卡并重置该账号当前适用的短周期额度，操作不可撤销。")
        }
    }

    private var header: some View {
        HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 2) {
                Text("CPA 状态")
                    .font(.headline)
                Text(model.statusMessage ?? "本机安全读取远端用量")
                    .font(.caption)
                    .foregroundStyle(.secondary)
                    .lineLimit(1)
            }
            Spacer()
            if model.isRefreshing {
                ProgressView().controlSize(.small)
            }
            Button {
                guard model.isConfigured else { return }
                model.showSettings.toggle()
            } label: {
                Image(systemName: model.showSettings || !model.isConfigured ? "xmark" : "gearshape")
                    .frame(width: 20, height: 20)
            }
            .buttonStyle(.plain)
            .disabled(!model.isConfigured)
            .help(model.isConfigured ? "连接设置" : "请先完成连接设置")
        }
    }

    private var tabPicker: some View {
        Picker("页面", selection: $selectedTab) {
            ForEach(DashboardTab.allCases) { tab in
                Label(tab.title, systemImage: tab.symbol)
                    .tag(tab)
            }
        }
        .pickerStyle(.segmented)
        .labelsHidden()
    }

    @ViewBuilder
    private var tabContent: some View {
        switch selectedTab {
        case .overview:
            overview
        case .accounts:
            accounts
        case .xai:
            xaiDetails
        }
    }

    private var overview: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 12) {
                serviceStatus
                overviewMetrics
                quotaOverview
                xaiOverview
                errorBanner
            }
            .padding(16)
        }
    }

    private var serviceStatus: some View {
        HStack(spacing: 8) {
            serviceBadge("CPA", healthy: model.summary?.services["cliproxy"])
            serviceBadge("Router", healthy: model.summary?.services["router"])
            Spacer()
            let count = model.summary?.source == "sub2" ? (model.summary?.focusAccounts ?? 0) : (model.summary?.refreshableAccounts ?? model.accounts.filter(\.hasRefreshToken).count)
            Label(model.summary?.source == "sub2" ? "\(count) 个重点" : "\(count) 个可刷新", systemImage: model.summary?.source == "sub2" ? "star.fill" : "arrow.triangle.2.circlepath")
                .font(.caption)
                .foregroundStyle(.secondary)
        }
    }

    private var overviewMetrics: some View {
        HStack(spacing: 8) {
            summaryMetric(
                model.summary?.source == "sub2" ? "有效" : "Codex",
                value: model.summary?.source == "sub2" ? (model.summary?.validAccounts ?? 0) : (model.summary?.codexAccounts ?? AccountListPresentation.codexAccounts(from: model.accounts).count),
                symbol: "person.2.fill"
            )
            summaryMetric(
                model.summary?.source == "sub2" ? "重点" : "K12",
                value: model.summary?.source == "sub2" ? (model.summary?.focusAccounts ?? 0) : (model.summary?.k12Accounts ?? model.accounts.filter(\.isK12).count),
                symbol: model.summary?.source == "sub2" ? "star.fill" : "graduationcap.fill"
            )
            summaryMetric(
                "异常",
                value: attentionAccounts.count,
                symbol: attentionAccounts.isEmpty ? "checkmark.circle.fill" : "exclamationmark.triangle.fill",
                tint: attentionAccounts.isEmpty ? .green : .orange
            )
        }
    }

    @ViewBuilder
    private var quotaOverview: some View {
        if let account = lowestQuotaAccount {
            let usage = account.usageSnapshot
            VStack(alignment: .leading, spacing: 9) {
                HStack {
                    Label("最低周额度", systemImage: "gauge.with.dots.needle.67percent")
                        .font(.subheadline.bold())
                    Spacer()
                    Button("查看账号") { selectedTab = .accounts }
                        .font(.caption)
                        .buttonStyle(.borderless)
                }

                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(account.emailMasked)
                            .font(.caption.bold())
                        Text(account.isK12 ? "K12" : (usage.planType ?? account.plan ?? "Codex").uppercased())
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Text(UsageFormatting.percent(usage.weeklyRemainingPercent))
                        .font(.title3.monospacedDigit().bold())
                        .foregroundStyle(quotaColor(usage.weeklyRemainingPercent))
                }

                ProgressView(value: usage.weeklyRemainingPercent ?? 0, total: 100)
                    .tint(quotaColor(usage.weeklyRemainingPercent))

                Text("距离重置 \(UsageFormatting.duration(usage.weeklyResetSeconds))")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            .cardStyle(tint: quotaColor(usage.weeklyRemainingPercent).opacity(0.08))
        } else {
            ContentUnavailableView("暂无周额度数据", systemImage: "gauge.with.dots.needle.0percent")
                .frame(maxWidth: .infinity, minHeight: 110)
        }
    }

    @ViewBuilder
    private var xaiOverview: some View {
        if let summary = model.summary, summary.xaiAccounts > 0 || summary.xaiUsage != nil {
            VStack(alignment: .leading, spacing: 9) {
                HStack {
                    Label("xAI 摘要", systemImage: "bolt.horizontal.circle.fill")
                        .font(.subheadline.bold())
                    Spacer()
                    Button("查看详情") { selectedTab = .xai }
                        .font(.caption)
                        .buttonStyle(.borderless)
                }

                if let usage = summary.xaiUsage {
                    HStack(spacing: 8) {
                        compactMetric("本月费用", value: UsageFormatting.usd(usage.officialSpendUSD))
                        compactMetric("预付余额", value: usage.hasPrepaidCredit == true ? UsageFormatting.usd(usage.prepaidBalanceUSD) : "后付费")
                        compactMetric("24h Token", value: UsageFormatting.integer(usage.totalTokens))
                    }
                } else {
                    Text(summary.xaiUsageError ?? "暂时无法读取 xAI 用量")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }
            .cardStyle(tint: Color.cyan.opacity(0.08))
        }
    }

    private var accounts: some View {
        VStack(spacing: 0) {
            VStack(alignment: .leading, spacing: 8) {
                Picker("账号筛选", selection: $accountFilter) {
                    ForEach(AccountListFilter.allCases) { filter in
                        Text(filter.title).tag(filter)
                    }
                }
                .pickerStyle(.segmented)
                .labelsHidden()

                Text("显示 \(filteredAccounts.count) / \(AccountListPresentation.codexAccounts(from: model.accounts).count) 个账号 · 重点与异常优先")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }
            .padding(.horizontal, 16)
            .padding(.vertical, 10)

            Divider()

            ScrollView {
                LazyVStack(alignment: .leading, spacing: 10) {
                    if filteredAccounts.isEmpty {
                        ContentUnavailableView(
                            accountFilter == .attention ? "没有异常账号" : "没有符合条件的账号",
                            systemImage: accountFilter == .attention ? "checkmark.circle" : "person.crop.circle.badge.questionmark"
                        )
                        .frame(maxWidth: .infinity, minHeight: 180)
                    } else {
                        ForEach(filteredAccounts) { account in
                            AccountRow(
                                account: account,
                                isExpanded: expandedAccountIDs.contains(account.id),
                                isResetting: model.resettingAccountID == account.id,
                                onToggleExpanded: { toggleExpanded(account.id) },
                                onRequestReset: { resetAccount = account }
                            )
                        }
                    }
                    errorBanner
                }
                .padding(16)
            }
        }
    }

    private var xaiDetails: some View {
        ScrollView {
            LazyVStack(alignment: .leading, spacing: 12) {
                if let summary = model.summary, summary.xaiAccounts > 0 || summary.xaiUsage != nil {
                    XAIUsageCard(
                        usage: summary.xaiUsage,
                        status: summary.xaiUsageStatus,
                        error: summary.xaiUsageError
                    )
                } else {
                    ContentUnavailableView("暂无 xAI 数据", systemImage: "bolt.horizontal.circle")
                        .frame(maxWidth: .infinity, minHeight: 180)
                }
                errorBanner
            }
            .padding(16)
        }
    }

    @ViewBuilder
    private var errorBanner: some View {
        if let error = model.errorMessage {
            Label(error, systemImage: "exclamationmark.triangle.fill")
                .font(.caption)
                .foregroundStyle(.red)
                .textSelection(.enabled)
                .padding(10)
                .frame(maxWidth: .infinity, alignment: .leading)
                .background(Color.red.opacity(0.08), in: RoundedRectangle(cornerRadius: 10))
        }
    }

    private var footer: some View {
        HStack(spacing: 12) {
            Button {
                Task { await model.refresh() }
            } label: {
                Label("刷新", systemImage: "arrow.clockwise")
            }
            .disabled(model.isRefreshing)

            if model.summary?.source != "sub2" {
                Button(action: model.addCodexAuthorization) {
                    Label(model.isAuthorizing ? "等待授权" : "添加授权", systemImage: "person.badge.plus")
                }
                .disabled(model.isAuthorizing)
            }

            Spacer()
            Button("退出") { NSApplication.shared.terminate(nil) }
        }
        .buttonStyle(.borderless)
    }

    private var settings: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("连接设置").font(.subheadline.bold())
            TextField("Bridge 地址", text: $model.endpoint)
                .textFieldStyle(.roundedBorder)
            SecureField(model.isConfigured ? "Desktop Token（留空不修改）" : "Desktop Token", text: $model.tokenInput)
                .textFieldStyle(.roundedBorder)
            HStack {
                Text("自动刷新")
                Slider(value: $model.refreshMinutes, in: 1...30, step: 1)
                Text("\(Int(model.refreshMinutes)) 分")
                    .monospacedDigit()
                    .frame(width: 42, alignment: .trailing)
            }
            Toggle("开机自动启动", isOn: $model.launchAtLogin)
            if let error = model.errorMessage {
                Text(error).font(.caption).foregroundStyle(.red).textSelection(.enabled)
            }
            HStack {
                Button("保存并连接", action: model.saveSettings)
                    .buttonStyle(.borderedProminent)
                if model.isConfigured {
                    Button("清除本机 Token", role: .destructive) {
                        showTokenClearConfirmation = true
                    }
                }
                Spacer()
                Button("退出") { NSApplication.shared.terminate(nil) }
            }
        }
    }

    private var filteredAccounts: [Account] {
        AccountListPresentation.filtered(model.accounts, by: accountFilter)
    }

    private var attentionAccounts: [Account] {
        AccountListPresentation.filtered(model.accounts, by: .attention)
    }

    private var lowestQuotaAccount: Account? {
        AccountListPresentation.codexAccounts(from: model.accounts)
            .filter { $0.usageSnapshot.weeklyRemainingPercent != nil }
            .min {
                ($0.usageSnapshot.weeklyRemainingPercent ?? 101) <
                    ($1.usageSnapshot.weeklyRemainingPercent ?? 101)
            }
    }

    private func toggleExpanded(_ accountID: String) {
        if expandedAccountIDs.contains(accountID) {
            expandedAccountIDs.remove(accountID)
        } else {
            expandedAccountIDs.insert(accountID)
        }
    }

    private func summaryMetric(_ title: String, value: Int, symbol: String, tint: Color = .accentColor) -> some View {
        VStack(alignment: .leading, spacing: 5) {
            Label(title, systemImage: symbol)
                .font(.caption2)
                .foregroundStyle(tint)
            Text("\(value)")
                .font(.title3.monospacedDigit().bold())
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(10)
        .background(.quaternary.opacity(0.45), in: RoundedRectangle(cornerRadius: 10))
    }

    private func compactMetric(_ title: String, value: String) -> some View {
        VStack(alignment: .leading, spacing: 2) {
            Text(title)
                .font(.caption2)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.caption.monospacedDigit().bold())
                .lineLimit(1)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private func serviceBadge(_ title: String, healthy: Bool?) -> some View {
        Label(title, systemImage: healthy == true ? "checkmark.circle.fill" : "xmark.circle.fill")
            .font(.caption.bold())
            .foregroundStyle(healthy == true ? .green : .red)
    }

    private func quotaColor(_ remaining: Double?) -> Color {
        guard let remaining else { return .gray }
        if remaining <= 10 { return .red }
        if remaining <= 35 { return .orange }
        return .green
    }
}

private struct XAIUsageCard: View {
    let usage: XAIUsage?
    let status: String?
    let error: String?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Label("xAI · 官方账单与 CPA 路由", systemImage: "bolt.horizontal.circle.fill")
                .font(.subheadline.bold())

            if let usage {
                officialBilling(usage)

                Divider()

                cpaRouterUsage(usage)
            } else {
                Text(error ?? "暂时无法读取 xAI 用量")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }

            if status == "unavailable", let error {
                Text(error)
                    .font(.caption2)
                    .foregroundStyle(.orange)
            }
        }
        .cardStyle(tint: Color.cyan.opacity(0.08))
    }

    @ViewBuilder
    private func officialBilling(_ usage: XAIUsage) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("xAI 官方 · 本月")
                    .font(.caption.bold())
                Spacer()
                if usage.hasOfficialBilling {
                    Text("Management API")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }
            }

            if usage.hasOfficialBilling {
                HStack(spacing: 16) {
                    metric("本月费用", value: UsageFormatting.usd(usage.officialSpendUSD))
                    if usage.hasPrepaidCredit == true {
                        metric("预付余额", value: UsageFormatting.usd(usage.prepaidBalanceUSD))
                    } else if usage.hasPrepaidCredit == false {
                        metric("计费方式", value: "后付费")
                    }
                }

                Text("统计至 \(UsageFormatting.timestamp(usage.officialPeriodEnd))")
                    .font(.caption2)
                    .foregroundStyle(.secondary)

                if usage.officialLimitReached == true {
                    Label("官方查询触达返回上限，结果可能不完整", systemImage: "exclamationmark.triangle.fill")
                        .font(.caption2)
                        .foregroundStyle(.orange)
                }
                if let officialError = usage.officialError {
                    Text(officialError)
                        .font(.caption2)
                        .foregroundStyle(.orange)
                }
            } else {
                Text(usage.officialError ?? "尚未配置 xAI Management Key，当前只显示 CPA 路由统计")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    @ViewBuilder
    private func cpaRouterUsage(_ usage: XAIUsage) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack {
                Text("CPA 路由 · 近 \(usage.windowHours) 小时")
                    .font(.caption.bold())
                Spacer()
                Text("非官方账单")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            }

            if usage.hasCPARouterUsage {
                HStack(spacing: 16) {
                    metric("请求", value: UsageFormatting.integer(usage.requests))
                    metric("成功率", value: UsageFormatting.percent(usage.successPercent))
                    metric("Token", value: UsageFormatting.integer(usage.totalTokens))
                }

                if let remainingPercent = usage.todayBudgetRemainingPercent,
                   let remainingTokens = usage.todayBudgetRemainingTokens {
                    HStack(spacing: 8) {
                        Text("路由预算")
                            .font(.caption)
                            .frame(width: 52, alignment: .leading)
                        ProgressView(value: remainingPercent, total: 100)
                            .tint(remainingPercent <= 10 ? .red : (remainingPercent <= 35 ? .orange : .cyan))
                        Text(UsageFormatting.percent(remainingPercent))
                            .font(.caption2.monospacedDigit())
                            .foregroundStyle(.secondary)
                    }
                    Text("内部剩余 \(UsageFormatting.integer(remainingTokens)) / \(UsageFormatting.integer(usage.dailyTokenLimit)) · 已用 \(UsageFormatting.integer(usage.todayTokens))")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                }

                Text("输入 \(UsageFormatting.integer(usage.inputTokens)) · 输出 \(UsageFormatting.integer(usage.outputTokens)) · 最近 \(UsageFormatting.timestamp(usage.lastRequestAt))")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
            } else {
                Text(usage.cpaError ?? "CPA 暂时无法读取 xAI 路由统计")
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
        }
    }

    private func metric(_ title: String, value: String) -> some View {
        VStack(alignment: .leading, spacing: 1) {
            Text(title)
                .font(.caption2)
                .foregroundStyle(.secondary)
            Text(value)
                .font(.caption.monospacedDigit().bold())
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

private struct AccountRow: View {
    let account: Account
    let isExpanded: Bool
    let isResetting: Bool
    let onToggleExpanded: () -> Void
    let onRequestReset: () -> Void

    var body: some View {
        let usage = account.usageSnapshot
        VStack(alignment: .leading, spacing: 9) {
            Button(action: onToggleExpanded) {
                VStack(alignment: .leading, spacing: 8) {
                    HStack(spacing: 8) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(account.displayIdentifier)
                                .font(.subheadline.bold())
                                .lineLimit(1)
                            Text(account.isK12 ? "K12" : (usage.planType ?? account.plan ?? account.provider).uppercased())
                                .font(.caption2)
                                .foregroundStyle(.secondary)
                        }
                        Spacer()
                        if account.focus == true {
                            Image(systemName: "star.fill")
                                .foregroundStyle(.yellow)
                                .help("重点账号")
                        }
                        if account.hasRefreshToken {
                            Image(systemName: "arrow.triangle.2.circlepath.circle.fill")
                                .foregroundStyle(.green)
                                .help("可自动刷新")
                        }
                        Image(systemName: isExpanded ? "chevron.up" : "chevron.down")
                            .font(.caption.bold())
                            .foregroundStyle(.secondary)
                    }

                    if account.usageStatus == "ok" {
                        compactQuotaRow(remaining: usage.weeklyRemainingPercent, reset: usage.weeklyResetSeconds)
                    } else {
                        Label(
                            account.usageError ?? (account.disabled ? "账号已停用" : "暂时无法读取用量"),
                            systemImage: "exclamationmark.triangle.fill"
                        )
                        .font(.caption)
                        .foregroundStyle(account.disabled ? .red : .orange)
                    }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)

            if isExpanded {
                Divider()

                HStack(spacing: 12) {
                    Label(account.hasRefreshToken ? "可自动刷新" : "需重新授权", systemImage: account.hasRefreshToken ? "checkmark.circle.fill" : "exclamationmark.circle.fill")
                        .foregroundStyle(account.hasRefreshToken ? .green : .orange)
                    if account.disabled {
                        Label("已停用", systemImage: "nosign")
                            .foregroundStyle(.red)
                    }
                }
                .font(.caption2)

                if account.isSub2 {
                    Text("优先级 \(account.priority ?? 0) · 24h 请求 \(account.recentRequests ?? 0) · 错误 \(account.recentErrors ?? 0)")
                        .font(.caption2)
                        .foregroundStyle(.secondary)
                    if let groups = account.groups, !groups.isEmpty {
                        Text(groups.joined(separator: " · "))
                            .font(.caption2)
                            .foregroundStyle(.secondary)
                    }
                } else {
                    creditsRow
                }
            }
        }
        .padding(10)
        .background(.quaternary.opacity(0.5), in: RoundedRectangle(cornerRadius: 10))
        .overlay {
            RoundedRectangle(cornerRadius: 10)
                .stroke(AccountListPresentation.needsAttention(account) ? Color.orange.opacity(0.45) : Color.clear)
        }
    }

    @ViewBuilder
    private var creditsRow: some View {
        HStack(spacing: 10) {
            if let credits = account.usage?.credits {
                let balance = credits.unlimited ? "不限" : (credits.balance ?? "0")
                Text("充值余额 \(balance)")
                    .font(.caption2)
                    .foregroundStyle(credits.overageLimitReached ? .red : .secondary)
            }
            if let resetCredits = account.usage?.resetCredits {
                Text("重置卡 \(resetCredits.availableCount)")
                    .font(.caption2)
                    .foregroundStyle(.secondary)
                Spacer()
                Button(isResetting ? "重置中…" : "使用重置卡", action: onRequestReset)
                    .font(.caption2)
                    .buttonStyle(.borderless)
                    .disabled(isResetting || resetCredits.applicableAvailableCount <= 0)
            }
        }
    }

    private func compactQuotaRow(remaining: Double?, reset: TimeInterval?) -> some View {
        HStack(spacing: 8) {
            Text("每周")
                .font(.caption)
                .frame(width: 32, alignment: .leading)
            ProgressView(value: remaining ?? 0, total: 100)
                .tint(color(for: remaining))
            Text(UsageFormatting.percent(remaining))
                .font(.caption.monospacedDigit().bold())
                .frame(width: 38, alignment: .trailing)
            Text(UsageFormatting.duration(reset))
                .font(.caption2)
                .foregroundStyle(.secondary)
                .frame(width: 72, alignment: .trailing)
        }
    }

    private func color(for remaining: Double?) -> Color {
        guard let remaining else { return .gray }
        if remaining <= 10 { return .red }
        if remaining <= 35 { return .orange }
        return .green
    }
}

private extension View {
    func cardStyle(tint: Color) -> some View {
        padding(12)
            .background(tint, in: RoundedRectangle(cornerRadius: 10))
    }
}

import AppKit
import Combine
import Foundation

@MainActor
final class AppModel: ObservableObject {
    @Published var summary: Summary?
    @Published var accounts: [Account] = []
    @Published var isRefreshing = false
    @Published var isAuthorizing = false
    @Published var resettingAccountID: String?
    @Published var errorMessage: String?
    @Published var statusMessage: String?
    @Published var showSettings = false
    @Published var endpoint: String
    @Published var tokenInput = ""
    @Published var refreshMinutes: Double
    @Published var launchAtLogin = LoginItemController.isEnabled

    private let defaults = UserDefaults.standard
    private let oauthListener = OAuthCallbackListener()
    private var refreshTask: Task<Void, Never>?

    init() {
        endpoint = defaults.string(forKey: "bridgeEndpoint") ?? "https://cpa.example.com/desktop/v1"
        let storedMinutes = defaults.double(forKey: "refreshMinutes")
        refreshMinutes = storedMinutes > 0 ? storedMinutes : 5
        if defaults.object(forKey: "launchAtLoginConfigured") == nil {
            do {
                try LoginItemController.setEnabled(true)
                defaults.set(true, forKey: "launchAtLoginConfigured")
            } catch {
                statusMessage = "可在设置中开启开机启动"
            }
        }
        launchAtLogin = LoginItemController.isEnabled
        startAutoRefresh()
        Task { await refresh() }
    }

    deinit {
        refreshTask?.cancel()
        oauthListener.stop()
    }

    var isConfigured: Bool {
        URL(string: endpoint) != nil && KeychainStore.loadToken()?.isEmpty == false
    }

    var minimumRemaining: Double? {
        accounts.compactMap { $0.usageSnapshot.weeklyRemainingPercent }.min()
    }

    var menuLabel: String {
        guard isConfigured else { return "设置" }
        guard let minimumRemaining else { return accounts.isEmpty ? "--" : "在线" }
        return UsageFormatting.percent(minimumRemaining)
    }

    var statusSymbol: String {
        if !isConfigured { return "gauge.with.dots.needle.0percent" }
        if errorMessage != nil { return "exclamationmark.triangle.fill" }
        guard let minimumRemaining else { return "gauge.with.dots.needle.50percent" }
        if minimumRemaining <= 10 { return "gauge.with.dots.needle.100percent" }
        if minimumRemaining <= 35 { return "gauge.with.dots.needle.67percent" }
        return "gauge.with.dots.needle.33percent"
    }

    func saveSettings() {
        errorMessage = nil
        let trimmedEndpoint = endpoint.trimmingCharacters(in: .whitespacesAndNewlines)
        guard let url = URL(string: trimmedEndpoint),
              url.scheme == "https" || url.host == "localhost" || url.host == "127.0.0.1"
        else {
            errorMessage = "Bridge 地址必须使用 HTTPS"
            return
        }
        endpoint = trimmedEndpoint.trimmingCharacters(in: CharacterSet(charactersIn: "/"))
        defaults.set(endpoint, forKey: "bridgeEndpoint")
        refreshMinutes = min(60, max(1, refreshMinutes))
        defaults.set(refreshMinutes, forKey: "refreshMinutes")
        if !tokenInput.isEmpty {
            do {
                try KeychainStore.saveToken(tokenInput.trimmingCharacters(in: .whitespacesAndNewlines))
                tokenInput = ""
            } catch {
                errorMessage = error.localizedDescription
                return
            }
        }
        do {
            try LoginItemController.setEnabled(launchAtLogin)
        } catch {
            launchAtLogin = LoginItemController.isEnabled
            errorMessage = "开机启动设置失败：\(error.localizedDescription)"
        }
        startAutoRefresh()
        Task { await refresh() }
    }

    func clearToken() {
        do {
            try KeychainStore.deleteToken()
            summary = nil
            accounts = []
            statusMessage = "本机授权已清除"
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func refresh() async {
        guard !isRefreshing else { return }
        guard let client = makeClient() else {
            showSettings = true
            return
        }
        isRefreshing = true
        defer { isRefreshing = false }
        do {
            async let summaryRequest = client.summary()
            async let accountsRequest = client.accounts()
            let (newSummary, accountResponse) = try await (summaryRequest, accountsRequest)
            summary = newSummary
            accounts = accountResponse.accounts
            errorMessage = nil
            statusMessage = "更新于 \(Date().formatted(date: .omitted, time: .shortened))"
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    func addCodexAuthorization() {
        guard !isAuthorizing else { return }
        guard let client = makeClient() else {
            showSettings = true
            return
        }
        isAuthorizing = true
        errorMessage = nil
        statusMessage = "正在等待浏览器授权"
        do {
            try oauthListener.start { [weak self] result in
                Task { @MainActor in
                    guard let self else { return }
                    switch result {
                    case let .success(callbackURL):
                        await self.completeOAuth(client: client, callbackURL: callbackURL)
                    case let .failure(error):
                        self.isAuthorizing = false
                        self.errorMessage = error.localizedDescription
                    }
                }
            }
        } catch {
            isAuthorizing = false
            errorMessage = "无法监听本机 1455 端口：\(error.localizedDescription)"
            return
        }
        Task {
            do {
                let response = try await client.startOAuth()
                NSWorkspace.shared.open(response.url)
            } catch {
                oauthListener.stop()
                isAuthorizing = false
                errorMessage = error.localizedDescription
            }
        }
    }

    func consumeResetCredit(for account: Account) async {
        guard resettingAccountID == nil else { return }
        guard let client = makeClient() else {
            showSettings = true
            return
        }
        resettingAccountID = account.id
        errorMessage = nil
        defer { resettingAccountID = nil }
        do {
            _ = try await client.consumeResetCredit(accountID: account.id)
            statusMessage = "已使用 1 张重置卡：\(account.emailMasked)"
            await refresh()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func completeOAuth(client: BridgeClient, callbackURL: URL) async {
        defer { isAuthorizing = false }
        do {
            let response = try await client.completeOAuth(redirectURL: callbackURL)
            statusMessage = response.accountMasked.map { "已添加授权：\($0)" } ?? "授权已添加"
            await refresh()
        } catch {
            errorMessage = error.localizedDescription
        }
    }

    private func makeClient() -> BridgeClient? {
        guard let baseURL = URL(string: endpoint), let token = KeychainStore.loadToken(), !token.isEmpty else {
            return nil
        }
        return BridgeClient(baseURL: baseURL, token: token)
    }

    private func startAutoRefresh() {
        refreshTask?.cancel()
        let interval = UInt64(max(1, refreshMinutes) * 60 * 1_000_000_000)
        refreshTask = Task { [weak self] in
            while !Task.isCancelled {
                try? await Task.sleep(nanoseconds: interval)
                guard !Task.isCancelled else { return }
                await self?.refresh()
            }
        }
    }
}

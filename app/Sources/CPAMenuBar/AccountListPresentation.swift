import Foundation

enum AccountListFilter: String, CaseIterable, Identifiable {
    case all
    case plus
    case k12
    case focus
    case attention

    var id: String { rawValue }

    var title: String {
        switch self {
        case .all: "全部"
        case .plus: "Plus"
        case .k12: "K12"
        case .focus: "重点"
        case .attention: "异常"
        }
    }
}

enum AccountListPresentation {
    static func codexAccounts(from accounts: [Account]) -> [Account] {
        accounts.filter { $0.provider == "codex" || $0.isK12 }
    }

    static func filtered(_ accounts: [Account], by filter: AccountListFilter) -> [Account] {
        let codexAccounts = codexAccounts(from: accounts)
        let matchingAccounts = codexAccounts.filter { account in
            switch filter {
            case .all:
                true
            case .plus:
                !account.isK12
            case .k12:
                account.isK12
            case .focus:
                account.focus == true
            case .attention:
                needsAttention(account)
            }
        }
        return sorted(matchingAccounts)
    }

    static func needsAttention(_ account: Account) -> Bool {
        account.disabled ||
            account.usageStatus != "ok" ||
            (account.usageSnapshot.weeklyRemainingPercent ?? 100) <= 10
    }

    static func sorted(_ accounts: [Account]) -> [Account] {
        accounts.sorted { left, right in
            if (left.focus == true) != (right.focus == true) { return left.focus == true }
            let leftRank = attentionRank(left)
            let rightRank = attentionRank(right)
            if leftRank != rightRank {
                return leftRank < rightRank
            }

            let leftRemaining = left.usageSnapshot.weeklyRemainingPercent ?? 101
            let rightRemaining = right.usageSnapshot.weeklyRemainingPercent ?? 101
            if leftRemaining != rightRemaining {
                return leftRemaining < rightRemaining
            }

            return left.displayIdentifier.localizedCaseInsensitiveCompare(right.displayIdentifier) == .orderedAscending
        }
    }

    private static func attentionRank(_ account: Account) -> Int {
        if account.disabled { return 0 }
        if account.usageStatus != "ok" { return 1 }
        if (account.usageSnapshot.weeklyRemainingPercent ?? 100) <= 10 { return 2 }
        return 3
    }
}

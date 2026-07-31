import Foundation

enum UsageParser {
    static func parse(_ payload: UsagePayload?) -> UsageSnapshot {
        guard let payload else {
            return UsageSnapshot(
                planType: nil,
                primaryUsedPercent: nil,
                primaryResetSeconds: nil,
                weeklyUsedPercent: nil,
                weeklyResetSeconds: nil
            )
        }

        let primarySlot = payload.rateLimit?.primaryWindow
        let secondarySlot = payload.rateLimit?.secondaryWindow
        var primaryWindow: UsageWindow?
        var weeklyWindow: UsageWindow?

        assign(primarySlot, legacyWeekly: false, primary: &primaryWindow, weekly: &weeklyWindow)
        assign(secondarySlot, legacyWeekly: true, primary: &primaryWindow, weekly: &weeklyWindow)

        return UsageSnapshot(
            planType: payload.planType,
            primaryUsedPercent: primaryWindow?.usedPercent,
            primaryResetSeconds: primaryWindow?.resetSeconds,
            weeklyUsedPercent: weeklyWindow?.usedPercent,
            weeklyResetSeconds: weeklyWindow?.resetSeconds
        )
    }

    private static func assign(
        _ window: UsageWindow?,
        legacyWeekly: Bool,
        primary: inout UsageWindow?,
        weekly: inout UsageWindow?
    ) {
        guard let window else { return }
        if let duration = window.limitWindowSeconds {
            if duration <= 6 * 60 * 60 {
                primary = primary ?? window
                return
            }
            if duration >= 24 * 60 * 60 {
                weekly = weekly ?? window
                return
            }
        }
        if legacyWeekly {
            weekly = weekly ?? window
        } else {
            primary = primary ?? window
        }
    }
}

enum UsageFormatting {
    static func percent(_ value: Double?) -> String {
        guard let value else { return "--" }
        return "\(Int(value.rounded()))%"
    }

    static func duration(_ seconds: TimeInterval?) -> String {
        guard let seconds else { return "--" }
        let totalMinutes = max(0, Int(seconds / 60))
        let days = totalMinutes / 1_440
        let hours = (totalMinutes % 1_440) / 60
        let minutes = totalMinutes % 60
        if days > 0 { return "\(days)天 \(hours)小时" }
        if hours > 0 { return "\(hours)小时 \(minutes)分" }
        return "\(minutes)分钟"
    }

    static func integer(_ value: Int64) -> String {
        value.formatted(.number.grouping(.automatic))
    }

    static func usd(_ value: Double?) -> String {
        guard let value else { return "--" }
        return value.formatted(
            .currency(code: "USD")
                .precision(.fractionLength(2 ... 4))
        )
    }

    static func timestamp(_ value: String?) -> String {
        guard let value, let date = ISO8601DateFormatter().date(from: value) else { return "--" }
        return date.formatted(date: .omitted, time: .shortened)
    }
}

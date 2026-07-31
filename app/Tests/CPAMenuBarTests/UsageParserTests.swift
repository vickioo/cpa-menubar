import XCTest
@testable import CPAMenuBar

final class UsageParserTests: XCTestCase {
    func testDualWindows() throws {
        let payload = try decode(#"{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":42,"reset_after_seconds":3600,"limit_window_seconds":18000},"secondary_window":{"used_percent":10,"reset_after_seconds":86400,"limit_window_seconds":604800}}}"#)
        let snapshot = UsageParser.parse(payload)
        XCTAssertEqual(snapshot.primaryRemainingPercent, 58)
        XCTAssertEqual(snapshot.weeklyRemainingPercent, 90)
    }

    func testWeeklyWindowCanOccupyPrimarySlot() throws {
        let payload = try decode(#"{"rate_limit":{"primary_window":{"used_percent":39,"reset_after_seconds":474388,"limit_window_seconds":604800},"secondary_window":null}}"#)
        let snapshot = UsageParser.parse(payload)
        XCTAssertNil(snapshot.primaryRemainingPercent)
        XCTAssertEqual(snapshot.weeklyRemainingPercent, 61)
    }

    func testReversedWindowSlotsUseDuration() throws {
        let payload = try decode(#"{"rate_limit":{"primary_window":{"used_percent":25,"limit_window_seconds":604800},"secondary_window":{"used_percent":40,"limit_window_seconds":18000}}}"#)
        let snapshot = UsageParser.parse(payload)
        XCTAssertEqual(snapshot.primaryRemainingPercent, 60)
        XCTAssertEqual(snapshot.weeklyRemainingPercent, 75)
    }

    func testDecodesCreditsAndResetCards() throws {
        let payload = try decode(#"{"credits":{"has_credits":true,"unlimited":false,"overage_limit_reached":false,"balance":"12.50"},"rate_limit_reset_credits":{"available_count":3,"applicable_available_count":1}}"#)
        XCTAssertEqual(payload.credits?.balance, "12.50")
        XCTAssertEqual(payload.resetCredits?.availableCount, 3)
        XCTAssertEqual(payload.resetCredits?.applicableAvailableCount, 1)
    }

    func testDecodesXAIUsageAndCalculatesRates() throws {
        let usage = try JSONDecoder().decode(
            XAIUsage.self,
            from: Data(#"{"window_hours":24,"requests":23,"successes":22,"errors":1,"input_tokens":4522,"output_tokens":3529,"total_tokens":8051,"today_tokens":1421,"daily_token_limit":1000000,"last_request_at":"2026-07-27T13:20:53+08:00"}"#.utf8)
        )
        XCTAssertEqual(usage.totalTokens, 8051)
        XCTAssertEqual(usage.successPercent ?? 0, 95.652173913, accuracy: 0.000001)
        XCTAssertEqual(usage.todayBudgetUsedPercent ?? 0, 0.1421, accuracy: 0.000001)
        XCTAssertEqual(usage.todayBudgetRemainingTokens, 998579)
        XCTAssertEqual(usage.todayBudgetRemainingPercent ?? 0, 99.8579, accuracy: 0.000001)
    }

    func testDecodesOfficialXAIBilling() throws {
        let usage = try JSONDecoder().decode(
            XAIUsage.self,
            from: Data(#"{"official_status":"ok","official_period_start":"2026-07-01T00:00:00+08:00","official_period_end":"2026-07-27T22:00:00+08:00","official_spend_usd":0.75,"prepaid_balance_usd":12.34,"has_prepaid_credit":true,"official_limit_reached":false,"cpa_status":"ok","window_hours":24,"requests":4,"successes":4,"errors":0,"input_tokens":30,"output_tokens":10,"total_tokens":40,"today_tokens":40,"daily_token_limit":100}"#.utf8)
        )
        XCTAssertTrue(usage.hasOfficialBilling)
        XCTAssertTrue(usage.hasCPARouterUsage)
        XCTAssertEqual(usage.officialSpendUSD, 0.75)
        XCTAssertEqual(usage.prepaidBalanceUSD, 12.34)
        XCTAssertEqual(usage.hasPrepaidCredit, true)
        XCTAssertTrue(UsageFormatting.usd(usage.officialSpendUSD).hasSuffix("0.75"))
    }

    func testZeroXAIRequestsHasNoSuccessPercentage() throws {
        let usage = try JSONDecoder().decode(
            XAIUsage.self,
            from: Data(#"{"window_hours":24,"requests":0,"successes":0,"errors":0,"input_tokens":0,"output_tokens":0,"total_tokens":0,"today_tokens":0,"daily_token_limit":0}"#.utf8)
        )
        XCTAssertNil(usage.successPercent)
        XCTAssertNil(usage.todayBudgetUsedPercent)
        XCTAssertNil(usage.todayBudgetRemainingTokens)
        XCTAssertNil(usage.todayBudgetRemainingPercent)
    }

    private func decode(_ json: String) throws -> UsagePayload {
        try JSONDecoder().decode(UsagePayload.self, from: Data(json.utf8))
    }
}

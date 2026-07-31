import XCTest
@testable import CPAMenuBar

final class AccountListPresentationTests: XCTestCase {
    func testFiltersCodexAccountsAndExcludesXAI() throws {
        let accounts = try makeAccounts()

        XCTAssertEqual(AccountListPresentation.filtered(accounts, by: .all).map(\.id), [
            "disabled", "error", "low", "k12", "healthy",
        ])
        XCTAssertEqual(AccountListPresentation.filtered(accounts, by: .plus).map(\.id), [
            "disabled", "error", "low", "healthy",
        ])
        XCTAssertEqual(AccountListPresentation.filtered(accounts, by: .k12).map(\.id), ["k12"])
    }

    func testAttentionFilterIncludesFailuresAndLowQuota() throws {
        let accounts = try makeAccounts()

        XCTAssertEqual(AccountListPresentation.filtered(accounts, by: .attention).map(\.id), [
            "disabled", "error", "low",
        ])
        XCTAssertFalse(AccountListPresentation.needsAttention(accounts.first { $0.id == "healthy" }!))
    }

    private func makeAccounts() throws -> [Account] {
        try JSONDecoder().decode(
            [Account].self,
            from: Data(
                #"""
                [
                  {"id":"healthy","provider":"codex","email_masked":"h***@example.com","disabled":false,"has_refresh_token":true,"is_k12":false,"usage_status":"ok","usage":{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":20,"limit_window_seconds":604800}}}},
                  {"id":"k12","provider":"codex","email_masked":"k***@example.com","disabled":false,"has_refresh_token":false,"is_k12":true,"usage_status":"ok","usage":{"rate_limit":{"primary_window":{"used_percent":50,"limit_window_seconds":604800}}}},
                  {"id":"low","provider":"codex","email_masked":"l***@example.com","disabled":false,"has_refresh_token":true,"is_k12":false,"usage_status":"ok","usage":{"rate_limit":{"primary_window":{"used_percent":95,"limit_window_seconds":604800}}}},
                  {"id":"error","provider":"codex","email_masked":"e***@example.com","disabled":false,"has_refresh_token":true,"is_k12":false,"usage_status":"error","usage_error":"暂时无法读取用量"},
                  {"id":"disabled","provider":"codex","email_masked":"d***@example.com","disabled":true,"has_refresh_token":false,"is_k12":false,"usage_status":"ok"},
                  {"id":"xai","provider":"xai","email_masked":"x***@example.com","disabled":false,"has_refresh_token":true,"is_k12":false,"usage_status":"ok"}
                ]
                """#.utf8
            )
        )
    }
}

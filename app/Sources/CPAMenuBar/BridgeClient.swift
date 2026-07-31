import Foundation

struct BridgeClient: Sendable {
    let baseURL: URL
    let token: String

    func summary() async throws -> Summary {
        try await request(path: "summary")
    }

    func accounts() async throws -> AccountsResponse {
        try await request(path: "accounts")
    }

    func startOAuth() async throws -> OAuthStartResponse {
        try await request(path: "oauth/codex/start", method: "POST")
    }

    func completeOAuth(redirectURL: URL) async throws -> OAuthStatusResponse {
        try await request(
            path: "oauth/codex/callback",
            method: "POST",
            body: OAuthCallbackRequest(redirectURL: redirectURL.absoluteString)
        )
    }

    func consumeResetCredit(accountID: String) async throws -> ResetCreditResponse {
        try await request(
            path: "accounts/\(accountID)/reset-credit",
            method: "POST",
            body: ResetCreditRequest()
        )
    }

    private func request<Response: Decodable, Body: Encodable>(
        path: String,
        method: String = "GET",
        body: Body?
    ) async throws -> Response {
        let url = baseURL.appending(path: path)
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.timeoutInterval = 30
        request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        request.setValue("application/json", forHTTPHeaderField: "Accept")
        if let body {
            request.httpBody = try JSONEncoder().encode(body)
            request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw BridgeClientError.invalidResponse
        }
        guard 200..<300 ~= httpResponse.statusCode else {
            let message = (try? JSONDecoder().decode(BridgeErrorResponse.self, from: data).error) ?? "HTTP \(httpResponse.statusCode)"
            throw BridgeClientError.server(message)
        }
        return try JSONDecoder().decode(Response.self, from: data)
    }

    private func request<Response: Decodable>(path: String, method: String = "GET") async throws -> Response {
        try await request(path: path, method: method, body: Optional<EmptyBody>.none)
    }
}

private struct EmptyBody: Encodable {}

enum BridgeClientError: LocalizedError {
    case invalidResponse
    case server(String)

    var errorDescription: String? {
        switch self {
        case .invalidResponse:
            return "Bridge 返回无效响应"
        case let .server(message):
            return message
        }
    }
}

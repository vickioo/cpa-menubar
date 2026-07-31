import Foundation
import Network

final class OAuthCallbackListener: @unchecked Sendable {
    private var listener: NWListener?
    private let queue = DispatchQueue(label: "com.example.cpa-menubar.oauth")
    private let lock = NSLock()
    private var completion: ((Result<URL, Error>) -> Void)?

    func start(completion: @escaping (Result<URL, Error>) -> Void) throws {
        stop()
        let listener = try NWListener(using: .tcp, on: 1455)
        lock.lock()
        self.listener = listener
        self.completion = completion
        lock.unlock()

        listener.stateUpdateHandler = { [weak self] state in
            if case let .failed(error) = state {
                self?.finish(.failure(error))
            }
        }
        listener.newConnectionHandler = { [weak self] connection in
            self?.handle(connection)
        }
        listener.start(queue: queue)
    }

    func stop() {
        lock.lock()
        listener?.cancel()
        listener = nil
        completion = nil
        lock.unlock()
    }

    private func handle(_ connection: NWConnection) {
        connection.start(queue: queue)
        connection.receive(minimumIncompleteLength: 1, maximumLength: 64 * 1024) { [weak self] data, _, _, error in
            guard let self else { return }
            if let error {
                connection.cancel()
                finish(.failure(error))
                return
            }
            guard let data,
                  let request = String(data: data, encoding: .utf8),
                  let firstLine = request.components(separatedBy: "\r\n").first,
                  let target = firstLine.split(separator: " ").dropFirst().first,
                  let url = URL(string: "http://localhost:1455\(target)")
            else {
                sendResponse(connection, success: false)
                finish(.failure(OAuthListenerError.invalidRequest))
                return
            }
            sendResponse(connection, success: true)
            finish(.success(url))
        }
    }

    private func sendResponse(_ connection: NWConnection, success: Bool) {
        let message = success ? "授权已收到，可以关闭此页面。" : "授权回调无效，请返回应用重试。"
        let body = "<html><meta charset=\"utf-8\"><body style=\"font-family:-apple-system;padding:48px\"><h2>CPA Menu</h2><p>\(message)</p></body></html>"
        let response = "HTTP/1.1 \(success ? "200 OK" : "400 Bad Request")\r\nContent-Type: text/html; charset=utf-8\r\nContent-Length: \(body.utf8.count)\r\nConnection: close\r\n\r\n\(body)"
        connection.send(content: Data(response.utf8), completion: .contentProcessed { _ in connection.cancel() })
    }

    private func finish(_ result: Result<URL, Error>) {
        lock.lock()
        let completion = completion
        self.completion = nil
        listener?.cancel()
        listener = nil
        lock.unlock()
        completion?(result)
    }
}

enum OAuthListenerError: LocalizedError {
    case invalidRequest

    var errorDescription: String? { "无法读取 OAuth 回调" }
}

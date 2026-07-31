import Foundation
import Security

let token = String(data: FileHandle.standardInput.readDataToEndOfFile(), encoding: .utf8)?
    .trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
guard token.count >= 32 else {
    fputs("Desktop Token 无效\n", stderr)
    exit(1)
}

let query: [String: Any] = [
    kSecClass as String: kSecClassGenericPassword,
    kSecAttrService as String: "com.example.cpa-menubar",
    kSecAttrAccount as String: "desktop-bridge-token"
]
let attributes: [String: Any] = [kSecValueData as String: Data(token.utf8)]
let status = SecItemUpdate(query as CFDictionary, attributes as CFDictionary)
if status == errSecItemNotFound {
    var insert = query
    insert[kSecValueData as String] = Data(token.utf8)
    insert[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
    let insertStatus = SecItemAdd(insert as CFDictionary, nil)
    guard insertStatus == errSecSuccess else {
        fputs("钥匙串写入失败：\(insertStatus)\n", stderr)
        exit(1)
    }
} else if status != errSecSuccess {
    fputs("钥匙串更新失败：\(status)\n", stderr)
    exit(1)
}

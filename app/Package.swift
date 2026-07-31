// swift-tools-version: 6.2

import PackageDescription

let package = Package(
    name: "CPAMenuBar",
    platforms: [.macOS(.v14)],
    products: [
        .executable(name: "CPAMenuBar", targets: ["CPAMenuBar"])
    ],
    targets: [
        .executableTarget(name: "CPAMenuBar"),
        .testTarget(name: "CPAMenuBarTests", dependencies: ["CPAMenuBar"])
    ]
)

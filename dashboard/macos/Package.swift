// swift-tools-version: 5.9
import PackageDescription

let package = Package(
    name: "4xLive",
    platforms: [.macOS(.v13)],
    targets: [
        .executableTarget(
            name: "4xLive",
            path: "Sources"
        )
    ]
)

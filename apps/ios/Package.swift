// swift-tools-version: 6.1
import PackageDescription
import Foundation

let localSDK = ProcessInfo.processInfo.environment["MCP_MOBILE_PATH"]
let mobile: Package.Dependency = localSDK.map { .package(name: "mcp-mobile", path: $0) }
    ?? .package(url: "https://github.com/tavric-labs/mcp-mobile.git", from: "0.1.0")

let skipRuntime = ProcessInfo.processInfo.environment["CULLY_SKIP_LLAMA"] == "1"
var engineTargets: [Target] = [.target(name: "CullyEngine", dependencies: skipRuntime ? [] : ["llama"], path: "Engine")]
if !skipRuntime {
    if let localRuntime = ProcessInfo.processInfo.environment["CULLY_LLAMA_PATH"] {
        engineTargets.append(.binaryTarget(name: "llama", path: localRuntime))
    } else { engineTargets.append(.binaryTarget(name: "llama", url: "https://github.com/ggml-org/llama.cpp/releases/download/b11718/llama-b11718-xcframework.zip", checksum: "1c4e5dccf8114c8cbff3280440fa7eef5d58a28745ed6e7f5d73bde335f61c5b")) }
}
let package = Package(
    name: "CullyMobile",
    platforms: [.iOS("16.4"), .macOS("13.3")],
    products: [.executable(name: "CullyMobile", targets: ["CullyMobile"]), .library(name: "CullyEngine", targets: ["CullyEngine"])],
    dependencies: [mobile],
    targets: engineTargets + [.executableTarget(name: "CullyMobile", dependencies: [
        .product(name: "MCPMobile", package: "mcp-mobile"), "CullyEngine",
    ], path: "Sources", resources: [.copy("Brand"), .copy("Skills"), .copy("Models")])]
)

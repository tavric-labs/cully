import Foundation
import CryptoKit
import SwiftUI

@MainActor final class ModelSetup: ObservableObject {
    @Published var status = "Set up local AI when you are ready."
    @Published var ready = false
    @Published var progress = 0.0
    @Published var busy = false
    private var operation: Task<Void, Never>?
    struct Weight: Decodable, Sendable { let id: String; let file: String; let bytes: Int64; let sha256: String; let url: URL }
    static let directory = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("CullyModels", isDirectory: true)
    static let languageURL = directory.appendingPathComponent("qwen2.5-0.5b-instruct-q4_k_m.gguf")
    init() {
        operation = Task {
            ready = (try? await Self.verifyExisting()) == true
            if ready { status = "Local language model ready" }
        }
    }
    private static func weight() throws -> Weight {
        struct Manifest: Decodable { let models: [Weight] }
        guard let url = CullyResources.bundle.url(forResource: "models", withExtension: "json", subdirectory: "Models"),
              let model = try JSONDecoder().decode(Manifest.self, from: Data(contentsOf: url)).models.first(where: { $0.id == "language" }) else { throw CocoaError(.fileReadCorruptFile) }
        return model
    }
    nonisolated private static func matches(_ url: URL, _ model: Weight) throws -> Bool {
        guard let size = try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize, Int64(size) == model.bytes else { return false }
        let input = try FileHandle(forReadingFrom: url); defer { try? input.close() }
        var hash = SHA256()
        while let data = try input.read(upToCount: 65536), !data.isEmpty {
            try Task.checkCancellation(); hash.update(data: data)
        }
        return hash.finalize().map { String(format: "%02x", $0) }.joined() == model.sha256
    }
    private static func verifyExisting() async throws -> Bool {
        let weight = try weight(), url = languageURL
        return try await Task.detached(priority: .utility) { try matches(url, weight) }.value
    }
    func start() {
        guard !busy else { return }
        operation?.cancel(); busy = true; status = "Downloading language model…"
        operation = Task {
            defer { busy = false }
            do {
                let model = try Self.weight()
                try FileManager.default.createDirectory(at: Self.directory, withIntermediateDirectories: true)
                let volume = try Self.directory.resourceValues(forKeys: [.volumeAvailableCapacityForImportantUsageKey])
                guard (volume.volumeAvailableCapacityForImportantUsage ?? 0) > model.bytes + 100 * 1024 * 1024 else { throw CocoaError(.fileWriteOutOfSpace) }
                var request = URLRequest(url: model.url); request.timeoutInterval = 900
                let reporter = WeightProgress { [weak self] value in Task { @MainActor in self?.progress = value } }
                let (temporary, response) = try await URLSession.shared.download(for: request, delegate: reporter)
                defer { try? FileManager.default.removeItem(at: temporary) }
                guard let response = response as? HTTPURLResponse, response.statusCode == 200, response.url?.scheme == "https" else { throw URLError(.badServerResponse) }
                status = "Verifying language model…"
                let valid = try await Task.detached(priority: .utility) { try Self.matches(temporary, model) }.value
                try Task.checkCancellation()
                guard valid else { throw CocoaError(.fileReadCorruptFile) }
                if FileManager.default.fileExists(atPath: Self.languageURL.path) { _ = try FileManager.default.replaceItemAt(Self.languageURL, withItemAt: temporary) }
                else { try FileManager.default.moveItem(at: temporary, to: Self.languageURL) }
                var directory = Self.directory; var values = URLResourceValues(); values.isExcludedFromBackup = true; try directory.setResourceValues(values)
                ready = true; status = "Local language model ready"
            } catch is CancellationError { status = "Setup paused. Tap Setup to retry." }
            catch { status = "Setup could not finish. Check your internet and allow 650 MiB of free space, then retry." }
        }
    }
    func cancel() { operation?.cancel(); status = "Setup paused. Tap Setup to retry." }
}

private final class WeightProgress: NSObject, URLSessionDownloadDelegate, @unchecked Sendable {
    private let update: @Sendable (Double) -> Void
    init(update: @escaping @Sendable (Double) -> Void) { self.update = update }
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didFinishDownloadingTo location: URL) {}
    func urlSession(_ session: URLSession, downloadTask: URLSessionDownloadTask, didWriteData bytesWritten: Int64, totalBytesWritten: Int64, totalBytesExpectedToWrite: Int64) {
        if totalBytesExpectedToWrite > 0 { update(Double(totalBytesWritten) / Double(totalBytesExpectedToWrite)) }
    }
}

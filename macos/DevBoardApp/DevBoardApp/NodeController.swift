import Combine
import Foundation
import AppKit

@MainActor
final class NodeController: ObservableObject {
    @Published private(set) var serviceResult: ProductResult?
    @Published private(set) var nodeStatus: NodeStatus?
    @Published private(set) var integrationResults: [String: ProductResult] = [:]
    @Published private(set) var busy = false
    @Published var notice: String?

    init() {
        refresh()
    }

    var serviceHealthy: Bool {
        serviceResult?.ok == true && serviceResult?.status == "healthy"
    }

    var hubConfigured: Bool {
        guard let endpoint = nodeStatus?.hubEndpoint.trimmingCharacters(in: .whitespacesAndNewlines) else { return false }
        return !endpoint.isEmpty
    }

    func refresh() {
        busy = true
        Task { @MainActor [weak self] in
            guard let self else { return }
            let service = await Self.runProduct(["service", "status"])
            self.serviceResult = service
            await self.fetchNodeStatus()
            let combined = await Self.runProduct(["integrations", "status"])
            self.integrationResults = combined.data ?? [:]
            self.busy = false
        }
    }

    func installOrRepairNode() {
        runAndRefresh(["service", "install"])
    }

    func restartNode() {
        runAndRefresh(["service", "restart"])
    }

    func install(provider: IntegrationProvider) {
        runAndRefresh(["integrations", "install", provider.rawValue])
    }

    func remove(provider: IntegrationProvider) {
        runAndRefresh(["integrations", "remove", provider.rawValue])
    }

    func openLocalSettings() {
        open(urlString: "http://127.0.0.1:8787/settings")
    }

    func openHub(path: String) {
        guard let endpoint = nodeStatus?.hubEndpoint.trimmingCharacters(in: .whitespacesAndNewlines), !endpoint.isEmpty else { return }
        let base = endpoint.hasSuffix("/") ? String(endpoint.dropLast()) : endpoint
        open(urlString: base + path)
    }

    func integrationStatus(for provider: IntegrationProvider) -> ProductResult? {
        integrationResults[provider.rawValue]
    }

    private func runAndRefresh(_ args: [String]) {
        busy = true
        Task { @MainActor [weak self] in
            guard let self else { return }
            let result = await Self.runProduct(args)
            self.notice = result.message ?? result.status
            self.refresh()
        }
    }

    private nonisolated static func runProduct(_ args: [String]) async -> ProductResult {
        let data = await Task.detached(priority: .userInitiated) { () -> Data? in
            guard let helper = Bundle.main.url(forResource: "devboard-bootstrap", withExtension: nil) else {
                return nil
            }
            let process = Process()
            process.executableURL = helper
            process.arguments = ["product"] + args
            let output = Pipe()
            process.standardOutput = output
            process.standardError = Pipe()
            do {
                try process.run()
                process.waitUntilExit()
                return output.fileHandleForReading.readDataToEndOfFile()
            } catch {
                return nil
            }
        }.value
        guard let data else {
            return ProductResult(schemaVersion: 1, ok: false, status: "helper_failed", message: "DevBoard product command failed.")
        }
        do {
            return try JSONDecoder().decode(ProductResult.self, from: data)
        } catch {
            return ProductResult(schemaVersion: 1, ok: false, status: "helper_failed", message: "DevBoard product command returned invalid data.")
        }
    }

    private func fetchNodeStatus() async {
        guard let url = URL(string: "http://127.0.0.1:8787/api/node/status") else {
            nodeStatus = nil
            return
        }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.cachePolicy = .reloadIgnoringLocalCacheData
        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            guard (response as? HTTPURLResponse)?.statusCode == 200 else {
                nodeStatus = nil
                return
            }
            nodeStatus = try JSONDecoder().decode(NodeStatus.self, from: data)
        } catch {
            nodeStatus = nil
        }
    }

    private func open(urlString: String) {
        guard let url = URL(string: urlString) else { return }
        NSWorkspace.shared.open(url)
    }
}

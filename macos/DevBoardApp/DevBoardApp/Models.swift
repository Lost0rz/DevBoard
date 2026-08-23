import Foundation

struct ProductResult: Decodable {
    let schemaVersion: Int?
    let ok: Bool
    let status: String
    let message: String?
    let data: [String: ProductResult]?

    init(schemaVersion: Int?, ok: Bool, status: String, message: String?, data: [String: ProductResult]? = nil) {
        self.schemaVersion = schemaVersion
        self.ok = ok
        self.status = status
        self.message = message
        self.data = data
    }
}

struct NodeStatus: Decodable {
    let schemaVersion: Int
    let serviceRunning: Bool
    let nodeId: String
    let displayName: String
    let hubEndpoint: String
    let uplinkEnabled: Bool
    let tokenConfigured: Bool
    let uplinkRunning: Bool
    let connected: Bool
    let lastAttemptAt: String?
    let lastSuccessAt: String?
    let lastErrorClass: String
}

enum IntegrationProvider: String, CaseIterable, Identifiable {
    case codex
    case claudeCode = "claude-code"

    var id: String { rawValue }
    var title: String { self == .codex ? "Codex" : "Claude Code" }
}

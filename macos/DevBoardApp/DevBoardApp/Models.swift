import Foundation

indirect enum JSONValue: Decodable {
    case object([String: JSONValue])
    case array([JSONValue])
    case string(String)
    case number(Double)
    case bool(Bool)
    case null

    init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if container.decodeNil() {
            self = .null
        } else if let value = try? container.decode(Bool.self) {
            self = .bool(value)
        } else if let value = try? container.decode(Double.self) {
            self = .number(value)
        } else if let value = try? container.decode(String.self) {
            self = .string(value)
        } else if let value = try? container.decode([String: JSONValue].self) {
            self = .object(value)
        } else if let value = try? container.decode([JSONValue].self) {
            self = .array(value)
        } else {
            throw DecodingError.typeMismatch(
                JSONValue.self,
                DecodingError.Context(codingPath: decoder.codingPath, debugDescription: "Unsupported JSON value")
            )
        }
    }
}

struct ProductResult: Decodable {
    let schemaVersion: Int?
    let ok: Bool
    let status: String
    let message: String?
    let data: [String: JSONValue]?

    init(schemaVersion: Int?, ok: Bool, status: String, message: String?, data: [String: JSONValue]? = nil) {
        self.schemaVersion = schemaVersion
        self.ok = ok
        self.status = status
        self.message = message
        self.data = data
    }

    init?(jsonValue: JSONValue) {
        guard case let .object(object) = jsonValue,
              case let .bool(ok)? = object["ok"],
              case let .string(status)? = object["status"] else {
            return nil
        }

        if case let .number(value)? = object["schemaVersion"] {
            schemaVersion = Int(exactly: value)
        } else {
            schemaVersion = nil
        }
        self.ok = ok
        self.status = status
        if case let .string(value)? = object["message"] {
            message = value
        } else {
            message = nil
        }
        if case let .object(value)? = object["data"] {
            data = value
        } else {
            data = nil
        }
    }

    var providerResults: [String: ProductResult] {
        (data ?? [:]).compactMapValues(ProductResult.init(jsonValue:))
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

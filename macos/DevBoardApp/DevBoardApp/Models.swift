import Foundation
import Darwin

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

    var quotaAccounts: [QuotaAccount] {
        guard case let .array(values)? = data?["accounts"] else { return [] }
        return values.compactMap(QuotaAccount.init(jsonValue:))
    }
}

// ProductCommandRunning is the App's process boundary. The production
// implementation owns a bounded Process, while self-tests inject a fake
// runner and never invoke CodexBar or any real helper.
protocol ProductCommandRunning {
    func run(_ args: [String], input: Data?, timeout: TimeInterval) async -> ProductResult
}

extension ProductCommandRunning {
    func run(_ args: [String], timeout: TimeInterval) async -> ProductResult {
        await run(args, input: nil, timeout: timeout)
    }
}

struct ProductProcessExecution {
    let stdout: Data
    let terminationStatus: Int32
    let timedOut: Bool
    let overflow: Bool
}

// This is the injectable process boundary used by BundleProductCommandRunner.
// Tests can provide a fake executor or exercise the real bounded executor with
// /bin/sh; neither path invokes CodexBar.
protocol ProductProcessExecuting {
    func run(executableURL: URL, arguments: [String], stdin: Data?, timeout: TimeInterval) async -> ProductProcessExecution
}

extension ProductProcessExecuting {
    func run(executableURL: URL, arguments: [String], timeout: TimeInterval) async -> ProductProcessExecution {
        await run(executableURL: executableURL, arguments: arguments, stdin: nil, timeout: timeout)
    }
}

struct BoundedProductProcessExecutor: ProductProcessExecuting {
    func run(executableURL: URL, arguments: [String], stdin: Data?, timeout: TimeInterval) async -> ProductProcessExecution {
        await Task.detached(priority: .userInitiated) {
            Self.execute(executableURL: executableURL, arguments: arguments, stdin: stdin, timeout: timeout)
        }.value
    }

    private static func execute(executableURL: URL, arguments: [String], stdin: Data?, timeout: TimeInterval) -> ProductProcessExecution {
        let process = Process()
        process.executableURL = executableURL
        process.arguments = arguments

        let inputPipe = stdin == nil ? nil : Pipe()
        if let inputPipe {
            process.standardInput = inputPipe
        } else {
            process.standardInput = FileHandle.nullDevice
        }

        let output = Pipe()
        let readHandle = output.fileHandleForReading
        let processFinished = DispatchSemaphore(value: 0)
        let readerFinished = DispatchSemaphore(value: 0)
        let lock = NSLock()
        var captured = Data()
        var overflow = false
        var readerSignalled = false

        func signalReaderFinished() {
            lock.lock()
            defer { lock.unlock() }
            guard !readerSignalled else { return }
            readerSignalled = true
            readerFinished.signal()
        }

        // availableData is consumed on every readability callback, including
        // the empty EOF callback. The handler remains installed until that EOF
        // signal is observed below, so no caller can copy captured data while
        // this reader is still mutating it.
        readHandle.readabilityHandler = { handle in
            let chunk = handle.availableData
            if chunk.isEmpty {
                signalReaderFinished()
                return
            }
            lock.lock()
            if !overflow {
                if captured.count + chunk.count > ProductCommandBudget.maxOutputBytes {
                    overflow = true
                } else {
                    captured.append(chunk)
                }
            }
            let shouldTerminate = overflow
            lock.unlock()
            if shouldTerminate && process.isRunning {
                process.terminate()
            }
        }
        process.standardOutput = output
        process.standardError = FileHandle.nullDevice
        process.terminationHandler = { _ in processFinished.signal() }

        do {
            try process.run()
            if let stdin, let inputPipe {
                inputPipe.fileHandleForWriting.write(stdin)
                try? inputPipe.fileHandleForWriting.close()
            }
        } catch {
            readHandle.readabilityHandler = nil
            return ProductProcessExecution(stdout: Data(), terminationStatus: -1, timedOut: false, overflow: false)
        }

        var timedOut = false
        if processFinished.wait(timeout: .now() + max(timeout, 0.01)) == .timedOut {
            timedOut = true
            process.terminate()
            if processFinished.wait(timeout: .now() + 0.5) == .timedOut {
                if process.processIdentifier > 0 {
                    kill(process.processIdentifier, SIGKILL)
                }
                // SIGKILL is the final bounded termination fallback. A child
                // that ignored SIGTERM must not survive the product command.
                if processFinished.wait(timeout: .now() + 1.0) == .timedOut {
                    // Do not proceed to the reader handshake until the OS
                    // confirms the child is gone. This is the last safe
                    // fallback if the termination callback was delayed.
                    process.waitUntilExit()
                    _ = processFinished.wait(timeout: .now() + 0.5)
                }
            }
        }

        // Process termination closes the pipe writer. Wait for the reader's
        // EOF callback before touching the handler or captured bytes. This is
        // intentionally an unbounded wait after the child is gone: returning
        // with a live reader would reintroduce truncation and a data race.
        readerFinished.wait()
        readHandle.readabilityHandler = nil
        process.terminationHandler = nil

        lock.lock()
        let data = captured
        let didOverflow = overflow
        lock.unlock()
        return ProductProcessExecution(
            stdout: data,
            terminationStatus: process.terminationStatus,
            timedOut: timedOut,
            overflow: didOverflow
        )
    }
}

enum ProductCommandBudget {
    static let maxOutputBytes = 256 * 1024
    static let quotaProviderProbeSeconds: TimeInterval = 15
    static let quotaGraceSeconds: TimeInterval = 5

    static func timeout(for args: [String]) -> TimeInterval {
        guard let command = args.first else { return 5 }
        if command == "mac", let action = args.dropFirst().first {
            return action == "configure" ? 20 : 5
        }
        if command == "quota", let action = args.dropFirst().first {
            switch action {
            case "detect", "configure":
                // The Go side probes Codex and Z.ai independently with a
                // 15-second budget. The App budget covers both plus grace.
                return quotaProviderProbeSeconds * 2 + quotaGraceSeconds
            case "status":
                // Local snapshot/status is read-only and must not probe a
                // provider, but it still gets its own bounded budget.
                return 5
            default:
                break
            }
        }
        if command == "service" || command == "integrations" {
            return 15
        }
        return 10
    }
}

struct QuotaAccount: Identifiable {
    let provider: String
    let accountKey: String
    let displayLabel: String
    let sourceHealth: String

    var id: String { accountKey }

    init?(jsonValue: JSONValue) {
        guard case let .object(object) = jsonValue,
              case let .string(provider)? = object["provider"],
              case let .string(accountKey)? = object["accountKey"],
              case let .string(displayLabel)? = object["displayLabel"],
              case let .string(sourceHealth)? = object["sourceHealth"] else {
            return nil
        }
        self.provider = provider
        self.accountKey = accountKey
        self.displayLabel = displayLabel
        self.sourceHealth = sourceHealth
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

struct MacSetupState: Equatable {
    let nodeID: String
    let displayName: String
    let hubEndpoint: String
    let tokenConfigured: Bool
    let configurationReady: Bool

    init?(result: ProductResult) {
        guard let data = result.data,
              case let .string(nodeID)? = data["nodeId"],
              case let .string(displayName)? = data["displayName"],
              case let .string(hubEndpoint)? = data["hubEndpoint"],
              case let .bool(tokenConfigured)? = data["tokenConfigured"],
              case let .bool(configurationReady)? = data["configurationReady"] else {
            return nil
        }
        self.nodeID = nodeID
        self.displayName = displayName
        self.hubEndpoint = hubEndpoint
        self.tokenConfigured = tokenConfigured
        self.configurationReady = configurationReady
    }
}

struct MacSetupRequestPayload: Encodable {
    let nodeId: String
    let displayName: String
    let hubEndpoint: String
    let nodeToken: String
}

enum IntegrationProvider: String, CaseIterable, Identifiable {
    case codex
    case claudeCode = "claude-code"

    var id: String { rawValue }
    var title: String { self == .codex ? "Codex" : "Claude Code" }
}

enum MenuSurfaceState: String {
    case healthy
    case notRunning
    case unhealthy
    case connected
    case disconnected
    case available
    case staleOrDegraded
    case attention
    case unavailable
    case notConfigured
    case cliUnavailable

    var title: String {
        switch self {
        case .healthy: return "Healthy"
        case .notRunning: return "Not running"
        case .unhealthy: return "Unhealthy"
        case .connected: return "Connected"
        case .disconnected: return "Disconnected"
        case .available: return "Available"
        case .staleOrDegraded: return "Stale or Degraded"
        case .attention: return "Needs attention"
        case .unavailable: return "Unavailable"
        case .notConfigured: return "Not configured"
        case .cliUnavailable: return "CodexBar CLI unavailable"
        }
    }

    var icon: String {
        switch self {
        case .healthy: return "checkmark.circle.fill"
        case .notRunning: return "pause.circle.fill"
        case .unhealthy: return "xmark.octagon.fill"
        case .connected: return "link.circle.fill"
        case .disconnected: return "link.badge.plus"
        case .available: return "checkmark.seal.fill"
        case .staleOrDegraded: return "clock.badge.exclamationmark"
        case .attention: return "exclamationmark.triangle.fill"
        case .unavailable: return "questionmark.circle"
        case .notConfigured: return "circle.dashed"
        case .cliUnavailable: return "xmark.circle.fill"
        }
    }
}

enum MenuStatusTone: String {
    case healthy
    case disconnected
    case fault
}

extension MenuSurfaceState {
    var tone: MenuStatusTone {
        switch self {
        case .healthy, .connected, .available:
            return .healthy
        case .attention, .staleOrDegraded, .unhealthy, .cliUnavailable:
            return .fault
        case .notRunning, .disconnected, .unavailable, .notConfigured:
            return .disconnected
        }
    }
}

struct MenuBarStatusModel: Equatable {
    let node: MenuSurfaceState
    let hub: MenuSurfaceState
    let codex: MenuSurfaceState
    let claudeCode: MenuSurfaceState
    let quota: MenuSurfaceState

    static func make(
        service: ProductResult?,
        nodeStatus: NodeStatus?,
        integrations: [String: ProductResult],
        quota: ProductResult?,
        refreshState: RefreshState = .fresh
    ) -> MenuBarStatusModel {
        let node: MenuSurfaceState
        if service?.ok == true && service?.status == "healthy" {
            node = nodeStatus == nil ? .unavailable : .healthy
        } else if service?.status == "not_running" || service?.status == "not_configured" {
            node = .notRunning
        } else if service?.status == "unhealthy" {
            node = .unhealthy
        } else if service == nil {
            node = .unavailable
        } else {
            node = .unavailable
        }

        let hub: MenuSurfaceState
        if nodeStatus == nil {
            hub = .unavailable
        } else if nodeStatus?.connected == true {
            hub = .connected
        } else if nodeStatus?.uplinkEnabled == false {
            hub = .notConfigured
        } else {
            hub = .disconnected
        }

        func integrationState(_ key: String) -> MenuSurfaceState {
            guard let result = integrations[key] else { return .unavailable }
            switch result.status {
            case "configured", "configured_requires_trust":
                // Codex uses configured_requires_trust as a conservative
                // capability note, but the managed hooks are installed and
                // valid. It is not a broken connection.
                return .healthy
            case "not_configured":
                return .notConfigured
            case "unavailable":
                return .unavailable
            case "helper_failed", "repair_required", "cleanup_required", "manual_configuration_required", "configured_but_disabled", "stable_binary_missing":
                return .attention
            default:
                return result.ok ? .healthy : .attention
            }
        }

        let quotaState: MenuSurfaceState
        switch quota?.status {
        case "quota_available", "quota_detected": quotaState = .available
        case "quota_not_configured", "quota_configuration_required": quotaState = .notConfigured
        case "quota_cli_unavailable": quotaState = .cliUnavailable
        case "quota_degraded", "quota_stale", "quota_configured": quotaState = .staleOrDegraded
        case "quota_unavailable", "quota_identity_invalid", "helper_failed": quotaState = .unavailable
        case nil: quotaState = .unavailable
        default: quotaState = .staleOrDegraded
        }
        if refreshState != .fresh && quotaState == .available {
            // A failed refresh must not leave a previously healthy-looking
            // quota row in the menu without a freshness warning.
            return MenuBarStatusModel(node: node, hub: hub, codex: integrationState("codex"), claudeCode: integrationState("claude-code"), quota: .staleOrDegraded)
        }
        return MenuBarStatusModel(
            node: node,
            hub: hub,
            codex: integrationState("codex"),
            claudeCode: integrationState("claude-code"),
            quota: quotaState
        )
    }
}

enum RefreshState: String {
    case fresh
    case degraded
    case unavailable
}

enum AppLifecyclePolicy {
    // Menu-bar Quit only terminates the UI process. LaunchAgent ownership and
    // service lifecycle are intentionally absent from this action.
    static let quitCallsServiceLifecycle = false
}

enum RefreshPolicy {
    static let intervalSeconds: TimeInterval = 8

    static func shouldStart(hasActiveTask: Bool) -> Bool {
        !hasActiveTask
    }

    static func shouldStartPeriodicRefresh(hasScheduleTask: Bool) -> Bool {
        !hasScheduleTask
    }

    static func failureState(service: ProductResult?, integrations: ProductResult?, quota: ProductResult?) -> RefreshState {
        if service == nil || service?.status == "helper_failed" { return .unavailable }
        if integrations?.status == "helper_failed" || quota?.status == "helper_failed" { return .degraded }
        return .fresh
    }
}

// Pure refresh gate used by the controller and self-test. It models the
// single in-flight operation invariant without requiring a real helper or
// network endpoint.
struct RefreshGate {
    private(set) var active = false

    mutating func begin() -> Bool {
        guard !active else { return false }
        active = true
        return true
    }

    mutating func end() -> Bool {
        guard active else { return false }
        active = false
        return true
    }
}

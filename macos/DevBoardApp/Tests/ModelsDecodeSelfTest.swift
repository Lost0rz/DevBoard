import Foundation

@main
struct ModelsDecodeSelfTest {
    static func main() async throws {
        let decoder = JSONDecoder()

        let direct = Data(#"{"schemaVersion":1,"ok":true,"status":"installed","message":"ok","data":{"serviceRunning":true,"pid":4242,"binaryPath":"/tmp/devboard"}}"#.utf8)
        let directResult = try decoder.decode(ProductResult.self, from: direct)
        guard directResult.ok, directResult.status == "installed", directResult.providerResults.isEmpty else {
            throw SelfTestError.directResult
        }

        let combined = Data(#"{"schemaVersion":1,"ok":false,"status":"not_configured","data":{"codex":{"schemaVersion":1,"ok":true,"status":"configured_requires_trust","data":{"provider":"codex","configured":true}},"claude-code":{"schemaVersion":1,"ok":false,"status":"not_configured","data":{"provider":"claude-code","configured":false}}}}"#.utf8)
        let combinedResult = try decoder.decode(ProductResult.self, from: combined)
        let providers = combinedResult.providerResults
        guard providers["codex"]?.status == "configured_requires_trust",
              providers["claude-code"]?.status == "not_configured" else {
            throw SelfTestError.combinedResult
        }

        let healthyService = ProductResult(schemaVersion: 1, ok: true, status: "healthy", message: nil)
        let status = NodeStatus(schemaVersion: 1, serviceRunning: true, nodeId: "mac-a", displayName: "Mac A", hubEndpoint: "http://nas.local", uplinkEnabled: true, tokenConfigured: true, uplinkRunning: true, connected: true, lastAttemptAt: nil, lastSuccessAt: nil, lastErrorClass: "")
        let integrations = [
            "codex": ProductResult(schemaVersion: 1, ok: true, status: "configured_requires_trust", message: nil),
            "claude-code": ProductResult(schemaVersion: 1, ok: true, status: "configured", message: nil),
        ]
        let menu = MenuBarStatusModel.make(service: healthyService, nodeStatus: status, integrations: integrations, quota: ProductResult(schemaVersion: 1, ok: true, status: "quota_available", message: nil))
        guard menu.node == .healthy, menu.hub == .connected, menu.codex == .attention, menu.claudeCode == .healthy, menu.quota == .available,
              menu.node.title == "Healthy", menu.hub.title == "Connected", menu.quota.title == "Available",
              !MenuSurfaceState.healthy.icon.isEmpty,
              AppLifecyclePolicy.quitCallsServiceLifecycle == false,
              RefreshPolicy.shouldStart(hasActiveTask: false),
              !RefreshPolicy.shouldStart(hasActiveTask: true),
              RefreshPolicy.shouldStartPeriodicRefresh(hasScheduleTask: false),
              !RefreshPolicy.shouldStartPeriodicRefresh(hasScheduleTask: true),
              RefreshPolicy.intervalSeconds >= 5 && RefreshPolicy.intervalSeconds <= 10,
              ProductCommandBudget.timeout(for: ["quota", "detect"]) >= 35,
              ProductCommandBudget.timeout(for: ["quota", "configure"]) >= 35,
              ProductCommandBudget.timeout(for: ["quota", "status"]) < 10,
              ProductCommandBudget.timeout(for: ["service", "status"]) > ProductCommandBudget.timeout(for: ["quota", "status"]),
              ProductCommandBudget.maxOutputBytes == 256 * 1024 else {
            throw SelfTestError.menuStatus
        }

        let unavailable = MenuBarStatusModel.make(service: nil, nodeStatus: nil, integrations: [:], quota: nil)
        guard unavailable.node == .unavailable, unavailable.hub == .unavailable, unavailable.codex == .unavailable,
              unavailable.claudeCode == .unavailable, unavailable.quota == .unavailable else {
            throw SelfTestError.menuUnavailable
        }

        let notRunning = MenuBarStatusModel.make(
            service: ProductResult(schemaVersion: 1, ok: false, status: "not_running", message: nil),
            nodeStatus: nil, integrations: [:], quota: ProductResult(schemaVersion: 1, ok: false, status: "quota_not_configured", message: nil)
        )
        guard notRunning.node == .notRunning, notRunning.quota == .notConfigured else { throw SelfTestError.statusMatrix }

        let unhealthy = MenuBarStatusModel.make(
            service: ProductResult(schemaVersion: 1, ok: false, status: "unhealthy", message: nil),
            nodeStatus: status, integrations: ["codex": ProductResult(schemaVersion: 1, ok: false, status: "unavailable", message: nil)],
            quota: ProductResult(schemaVersion: 1, ok: false, status: "quota_unavailable", message: nil)
        )
        guard unhealthy.node == .unhealthy, unhealthy.hub == .connected, unhealthy.codex == .unavailable, unhealthy.quota == .unavailable else {
            throw SelfTestError.statusMatrix
        }

        let disconnected = NodeStatus(schemaVersion: 1, serviceRunning: true, nodeId: "mac-a", displayName: "Mac A", hubEndpoint: "http://nas.local", uplinkEnabled: true, tokenConfigured: true, uplinkRunning: true, connected: false, lastAttemptAt: nil, lastSuccessAt: nil, lastErrorClass: "")
        let noHub = NodeStatus(schemaVersion: 1, serviceRunning: true, nodeId: "mac-a", displayName: "Mac A", hubEndpoint: "", uplinkEnabled: false, tokenConfigured: false, uplinkRunning: false, connected: false, lastAttemptAt: nil, lastSuccessAt: nil, lastErrorClass: "")
        guard MenuBarStatusModel.make(service: healthyService, nodeStatus: disconnected, integrations: [:], quota: nil).hub == .disconnected,
              MenuBarStatusModel.make(service: healthyService, nodeStatus: noHub, integrations: [:], quota: nil).hub == .notConfigured,
              MenuSurfaceState.notRunning.title == "Not running",
              MenuSurfaceState.unhealthy.title == "Unhealthy",
              MenuSurfaceState.disconnected.title == "Disconnected",
              MenuSurfaceState.staleOrDegraded.title == "Stale or Degraded" else {
            throw SelfTestError.statusMatrix
        }

        let stale = MenuBarStatusModel.make(service: healthyService, nodeStatus: status, integrations: integrations, quota: ProductResult(schemaVersion: 1, ok: false, status: "quota_degraded", message: nil))
        let configuredOnly = MenuBarStatusModel.make(service: healthyService, nodeStatus: status, integrations: integrations, quota: ProductResult(schemaVersion: 1, ok: true, status: "quota_configured", message: nil))
        guard stale.quota == .staleOrDegraded, configuredOnly.quota == .staleOrDegraded else { throw SelfTestError.statusMatrix }

        // A missing CodexBar CLI is its own menu state, never a generic
        // "quota unavailable": the label must name the actionable cause.
        let cliMissing = MenuBarStatusModel.make(service: healthyService, nodeStatus: status, integrations: integrations, quota: ProductResult(schemaVersion: 1, ok: false, status: "quota_cli_unavailable", message: nil))
        guard cliMissing.quota == .cliUnavailable,
              cliMissing.quota.title == "CodexBar CLI unavailable",
              !cliMissing.quota.icon.isEmpty else { throw SelfTestError.statusMatrix }

        let integrationMatrix: [(String, MenuSurfaceState)] = [
            ("configured", .healthy),
            ("configured_requires_trust", .attention),
            ("unavailable", .unavailable),
            ("not_configured", .notConfigured),
        ]
        for (integrationStatus, expected) in integrationMatrix {
            let integration = ProductResult(schemaVersion: 1, ok: integrationStatus == "configured" || integrationStatus == "configured_requires_trust", status: integrationStatus, message: nil)
            let matrix = MenuBarStatusModel.make(service: healthyService, nodeStatus: status, integrations: ["codex": integration, "claude-code": integration], quota: ProductResult(schemaVersion: 1, ok: true, status: "quota_available", message: nil))
            guard matrix.codex == expected, matrix.claudeCode == expected else { throw SelfTestError.statusMatrix }
        }
        guard RefreshPolicy.failureState(service: nil, integrations: nil, quota: nil) == .unavailable,
              RefreshPolicy.failureState(service: healthyService, integrations: ProductResult(schemaVersion: 1, ok: false, status: "helper_failed", message: nil), quota: nil) == .degraded else {
            throw SelfTestError.refreshFailure
        }

        let setupStatus = ProductResult(
            schemaVersion: 1,
            ok: true,
            status: "setup_status",
            message: nil,
            data: [
                "nodeId": .string("mac-0123456789abcdef0123456789abcdef"),
                "displayName": .string("Studio Mac"),
                "hubEndpoint": .string("http://nas.local"),
                "tokenConfigured": .bool(true),
                "configurationReady": .bool(true),
            ]
        )
        guard let setupState = MacSetupState(result: setupStatus),
              setupState.nodeID == "mac-0123456789abcdef0123456789abcdef",
              setupState.displayName == "Studio Mac",
              setupState.hubEndpoint == "http://nas.local",
              setupState.tokenConfigured,
              setupState.configurationReady else {
            throw SelfTestError.nativeSetupState
        }

        let protectedToken = "abcdefghijklmnopqrstuvwxyz012345"
        let protectedInput = try JSONEncoder().encode(MacSetupRequestPayload(
            nodeId: setupState.nodeID,
            displayName: "Studio Mac",
            hubEndpoint: setupState.hubEndpoint,
            nodeToken: protectedToken
        ))
        let recordingExecutor = RecordingProductProcessExecutor(
            execution: ProductProcessExecution(stdout: Data(#"{"schemaVersion":1,"ok":true,"status":"configured_connected"}"#.utf8), terminationStatus: 0, timedOut: false, overflow: false)
        )
        let protectedRunner = BundleProductCommandRunner(executor: recordingExecutor, executableURL: URL(fileURLWithPath: "/tmp/devboard-bootstrap"))
        _ = await protectedRunner.run(["mac", "configure"], input: protectedInput, timeout: 20)
        guard !recordingExecutor.arguments.joined(separator: " ").contains(protectedToken),
              recordingExecutor.stdin == protectedInput else {
            throw SelfTestError.nativeSetupSecretBoundary
        }

        var gate = RefreshGate()
        guard gate.begin(), !gate.begin(), gate.end(), gate.begin() else { throw SelfTestError.refreshGate }

        let fake = FakeProductRunner()
        let fakeResult = await fake.run(["quota", "detect"], timeout: ProductCommandBudget.timeout(for: ["quota", "detect"]))
        guard fakeResult.status == "quota_degraded", fake.lastArguments == ["quota", "detect"], fake.lastTimeout >= 35 else {
            throw SelfTestError.fakeRunner
        }

        try await runProcessExecutorSelfTests()
    }

    private static func runProcessExecutorSelfTests() async throws {
        let executor = BoundedProductProcessExecutor()
        let shell = URL(fileURLWithPath: "/bin/sh")
        let json = #"{"schemaVersion":1,"ok":true,"status":"installed","message":"ok"}"#
        let printJSON = "printf '%s' '" + json + "'"

        // Repeated short results catch the old race where a callback still
        // owned the final bytes when the handler was cleared.
        for _ in 0..<100 {
            let result = await executor.run(executableURL: shell, arguments: ["-c", printJSON], timeout: 2)
            guard result.stdout == Data(json.utf8), !result.timedOut, !result.overflow else {
                throw SelfTestError.processShortJSON
            }
        }

        let delayed = await executor.run(executableURL: shell, arguments: ["-c", "sleep 0.05; \(printJSON)"], timeout: 2)
        guard delayed.stdout == Data(json.utf8), !delayed.timedOut, !delayed.overflow else {
            throw SelfTestError.processDelayed
        }

        let stdinFixture = Data("protected-setup-fixture".utf8)
        let stdinResult = await executor.run(executableURL: shell, arguments: ["-c", "cat"], stdin: stdinFixture, timeout: 2)
        guard stdinResult.stdout == stdinFixture, !stdinResult.timedOut, !stdinResult.overflow else {
            throw SelfTestError.processStdin
        }

        // A shell builtin keeps this a single process, so returning from the
        // executor after terminationHandler is also an assertion that it is
        // no longer running.
        let timeout = await executor.run(executableURL: shell, arguments: ["-c", "trap 'exit 0' TERM; while :; do :; done"], timeout: 0.1)
        guard timeout.timedOut, timeout.terminationStatus >= 0 else {
            throw SelfTestError.processTimeout
        }

        let sigkill = await executor.run(executableURL: shell, arguments: ["-c", "trap '' TERM; while :; do :; done"], timeout: 0.1)
        guard sigkill.timedOut, sigkill.terminationStatus >= 0 else {
            throw SelfTestError.processSIGKILL
        }

        let exact = await executor.run(
            executableURL: shell,
            arguments: ["-c", "dd if=/dev/zero bs=262144 count=1 2>/dev/null"],
            timeout: 2
        )
        guard exact.stdout.count == ProductCommandBudget.maxOutputBytes, !exact.overflow, !exact.timedOut else {
            throw SelfTestError.processExactLimit
        }

        let oversized = await executor.run(
            executableURL: shell,
            arguments: ["-c", "dd if=/dev/zero bs=262145 count=1 2>/dev/null"],
            timeout: 2
        )
        guard oversized.overflow, oversized.stdout.count <= ProductCommandBudget.maxOutputBytes else {
            throw SelfTestError.processOverflow
        }

        let nonzero = await executor.run(
            executableURL: shell,
            arguments: ["-c", "\(printJSON); exit 7"],
            timeout: 2
        )
        guard nonzero.terminationStatus != 0, let decoded = try? JSONDecoder().decode(ProductResult.self, from: nonzero.stdout), decoded.status == "installed" else {
            throw SelfTestError.processNonzeroJSON
        }

        let injectedValid = BundleProductCommandRunner(
            executor: FixedProductProcessExecutor(execution: ProductProcessExecution(stdout: Data(json.utf8), terminationStatus: 7, timedOut: false, overflow: false)),
            executableURL: shell
        )
        let parsedNonzero = await injectedValid.run(["quota", "status"], timeout: 1)
        guard parsedNonzero.status == "installed", parsedNonzero.ok else {
            throw SelfTestError.processRunnerInjection
        }

        for invalidExecution in [
            ProductProcessExecution(stdout: Data(json.utf8), terminationStatus: 0, timedOut: true, overflow: false),
            ProductProcessExecution(stdout: Data(json.utf8), terminationStatus: 0, timedOut: false, overflow: true),
            ProductProcessExecution(stdout: Data("not-json".utf8), terminationStatus: 0, timedOut: false, overflow: false),
        ] {
            let runner = BundleProductCommandRunner(executor: FixedProductProcessExecutor(execution: invalidExecution), executableURL: shell)
            let result = await runner.run(["quota", "status"], timeout: 1)
            guard !result.ok, result.status == "helper_failed" else {
                throw SelfTestError.processRunnerInvalid
            }
        }
    }
}

private enum SelfTestError: Error {
    case directResult
    case combinedResult
    case menuStatus
    case menuUnavailable
    case statusMatrix
    case refreshGate
    case fakeRunner
    case refreshFailure
    case processShortJSON
    case processDelayed
    case processStdin
    case processTimeout
    case processSIGKILL
    case processExactLimit
    case processOverflow
    case processNonzeroJSON
    case processRunnerInjection
    case processRunnerInvalid
    case nativeSetupState
    case nativeSetupSecretBoundary
}

private final class FakeProductRunner: ProductCommandRunning {
    var lastArguments: [String] = []
    var lastTimeout: TimeInterval = 0

    func run(_ args: [String], input: Data?, timeout: TimeInterval) async -> ProductResult {
        lastArguments = args
        lastTimeout = timeout
        return ProductResult(schemaVersion: 1, ok: false, status: "quota_degraded", message: "fake")
    }
}

private struct FixedProductProcessExecutor: ProductProcessExecuting {
    let execution: ProductProcessExecution

    func run(executableURL: URL, arguments: [String], stdin: Data?, timeout: TimeInterval) async -> ProductProcessExecution {
        execution
    }
}

private final class RecordingProductProcessExecutor: ProductProcessExecuting {
    let execution: ProductProcessExecution
    private(set) var arguments: [String] = []
    private(set) var stdin: Data?

    init(execution: ProductProcessExecution) {
        self.execution = execution
    }

    func run(executableURL: URL, arguments: [String], stdin: Data?, timeout: TimeInterval) async -> ProductProcessExecution {
        self.arguments = arguments
        self.stdin = stdin
        return execution
    }
}

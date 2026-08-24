import Combine
import Foundation
import AppKit
import ServiceManagement

@MainActor
final class NodeController: ObservableObject {
    @Published private(set) var serviceResult: ProductResult?
    @Published private(set) var nodeStatus: NodeStatus?
    @Published private(set) var integrationResults: [String: ProductResult] = [:]
    @Published private(set) var quotaStatusResult: ProductResult?
    @Published private(set) var quotaDetectionResult: ProductResult?
    @Published var quotaLabels: [String: String] = [:]
    @Published private(set) var loginItemState = "not_registered"
    @Published private(set) var busy = false
    @Published private(set) var refreshState: RefreshState = .unavailable
    @Published private(set) var lastRefreshAt: Date?
    @Published var notice: String?

    private let productRunner: ProductCommandRunning
    private var activeTask: Task<Void, Never>?
    private var refreshScheduleTask: Task<Void, Never>?

    init(runner: ProductCommandRunning = BundleProductCommandRunner()) {
        productRunner = runner
        refresh()
        refreshLoginItemStatus()
    }

    deinit {
        activeTask?.cancel()
        refreshScheduleTask?.cancel()
    }

    var serviceHealthy: Bool {
        serviceResult?.ok == true && serviceResult?.status == "healthy"
    }

    var hubConfigured: Bool {
        guard let endpoint = nodeStatus?.hubEndpoint.trimmingCharacters(in: .whitespacesAndNewlines) else { return false }
        return !endpoint.isEmpty
    }

    var menuStatus: MenuBarStatusModel {
        MenuBarStatusModel.make(service: serviceResult, nodeStatus: nodeStatus, integrations: integrationResults, quota: quotaStatusResult, refreshState: refreshState)
    }

    func refresh() {
        startOperation { [weak self] in
            guard let self else { return }
            await self.reloadStatus()
        }
    }

    // MenuBarExtra creates and destroys its content as the menu opens and
    // closes. Opening always requests an immediate bounded refresh; the
    // schedule is owned by the menu content and is cancelled on collapse.
    func menuDidAppear() {
        refresh()
        guard RefreshPolicy.shouldStartPeriodicRefresh(hasScheduleTask: refreshScheduleTask != nil) else { return }
        refreshScheduleTask = Task { @MainActor [weak self] in
            guard let self else { return }
            while !Task.isCancelled {
                do {
                    try await Task.sleep(nanoseconds: UInt64(RefreshPolicy.intervalSeconds * 1_000_000_000))
                } catch {
                    return
                }
                guard !Task.isCancelled else { return }
                self.refresh()
            }
        }
    }

    func menuDidDisappear() {
        refreshScheduleTask?.cancel()
        refreshScheduleTask = nil
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

    func detectQuota() {
        startOperation { [weak self] in
            guard let self else { return }
            let result = await self.runProduct(["quota", "detect"])
            self.quotaDetectionResult = result
            for account in result.quotaAccounts where account.provider == "codex" && self.quotaLabels[account.accountKey] == nil {
                self.quotaLabels[account.accountKey] = account.displayLabel
            }
            self.notice = result.message ?? result.status
        }
    }

    func saveQuota() {
        guard let allAccounts = quotaDetectionResult?.quotaAccounts else {
            notice = "Choose a unique Codex A or Codex B label for every detected Codex account."
            return
        }
        let accounts = allAccounts.filter { $0.provider == "codex" }
        let glmAccounts = allAccounts.filter { $0.provider == "zai" }
        let labels = accounts.compactMap { quotaLabels[$0.accountKey] }
        guard accounts.count == 2, glmAccounts.count == 1,
              labels.count == accounts.count,
              Set(labels) == Set(["Codex A", "Codex B"]) else {
            notice = "Mac A quota setup requires two Codex accounts, one GLM account, and unique Codex A/B labels."
            return
        }
        var args = ["quota", "configure"]
        for account in accounts.sorted(by: { $0.accountKey < $1.accountKey }) {
            args += ["--assign", "\(account.accountKey)=\(quotaLabels[account.accountKey] ?? "")"]
        }
        startOperation { [weak self] in
            guard let self else { return }
            let result = await self.runProduct(args)
            self.quotaStatusResult = result
            self.notice = result.message ?? result.status
            self.quotaDetectionResult = await self.runProduct(["quota", "detect"])
        }
    }

    func setQuotaLabel(_ label: String, for accountKey: String) {
        quotaLabels[accountKey] = label
    }

    func refreshLoginItemStatus() {
        switch SMAppService.mainApp.status {
        case .enabled:
            loginItemState = "enabled"
        case .requiresApproval:
            loginItemState = "requires_approval"
        case .notRegistered:
            loginItemState = "not_registered"
        case .notFound:
            loginItemState = "unavailable"
        @unknown default:
            loginItemState = "unavailable"
        }
    }

    func setLaunchAtLogin(_ enabled: Bool) {
        do {
            if enabled {
                try SMAppService.mainApp.register()
            } else {
                try SMAppService.mainApp.unregister()
            }
            refreshLoginItemStatus()
        } catch {
            // Login-item registration is independent from the LaunchAgent;
            // never report its failure as a Node failure.
            notice = "Launch at Login could not be changed. Review macOS Login Items."
            refreshLoginItemStatus()
        }
    }

    func openLocalSettings() {
        open(urlString: "http://127.0.0.1:8787/settings")
    }

    func openDisplay() {
        if hubConfigured {
            openHub(path: "/display")
        } else {
            open(urlString: "http://127.0.0.1:8787/display")
        }
    }

    func openHub(path: String) {
        guard let url = hubURL(path: path) else { return }
        NSWorkspace.shared.open(url)
    }

    func integrationStatus(for provider: IntegrationProvider) -> ProductResult? {
        integrationResults[provider.rawValue]
    }

    private func runAndRefresh(_ args: [String]) {
        startOperation { [weak self] in
            guard let self else { return }
            let result = await self.runProduct(args)
            self.notice = result.message ?? result.status
            await self.reloadStatus()
        }
    }

    private func startOperation(_ operation: @escaping @MainActor () async -> Void) {
        guard RefreshPolicy.shouldStart(hasActiveTask: activeTask != nil) else { return }
        busy = true
        activeTask = Task { @MainActor [weak self] in
            guard let self else { return }
            await operation()
            self.busy = false
            self.activeTask = nil
        }
    }

    private func reloadStatus() async {
        let service = await runProduct(["service", "status"])
        serviceResult = service
        var nodeFetchOK = true
        if service.ok && service.status == "healthy" {
            nodeFetchOK = await fetchNodeStatus()
        } else {
            // A responsive process on 8787 is not necessarily the managed
            // LaunchAgent-owned Node. Do not trust its product status until
            // the helper has verified PID ownership and role-aware health.
            nodeStatus = nil
        }
        let combined = await runProduct(["integrations", "status"])
        integrationResults = combined.providerResults
        let quota = await runProduct(["quota", "status"])
        quotaStatusResult = quota
        refreshState = RefreshPolicy.failureState(service: service, integrations: combined, quota: quota)
        if service.ok && service.status == "healthy" && !nodeFetchOK {
            refreshState = .degraded
        }
        lastRefreshAt = Date()
        if refreshState == .unavailable {
            notice = "Status refresh unavailable. Some product state cannot be verified."
        } else if refreshState == .degraded {
            notice = "Status refresh degraded. Some product state may be stale."
        }
    }

    private func runProduct(_ args: [String]) async -> ProductResult {
        await productRunner.run(args, timeout: ProductCommandBudget.timeout(for: args))
    }

    private func fetchNodeStatus() async -> Bool {
        guard let url = URL(string: "http://127.0.0.1:8787/api/node/status") else {
            nodeStatus = nil
            return false
        }
        var request = URLRequest(url: url)
        request.httpMethod = "GET"
        request.cachePolicy = .reloadIgnoringLocalCacheData
        request.timeoutInterval = 2
        do {
            let (data, response) = try await URLSession.shared.data(for: request)
            guard (response as? HTTPURLResponse)?.statusCode == 200 else {
                nodeStatus = nil
                return false
            }
            nodeStatus = try JSONDecoder().decode(NodeStatus.self, from: data)
            return true
        } catch {
            nodeStatus = nil
            return false
        }
    }

    private func open(urlString: String) {
        guard let url = URL(string: urlString) else { return }
        NSWorkspace.shared.open(url)
    }

    private func hubURL(path: String) -> URL? {
        guard let endpoint = nodeStatus?.hubEndpoint.trimmingCharacters(in: .whitespacesAndNewlines),
              var components = URLComponents(string: endpoint),
              components.scheme == "http" || components.scheme == "https",
              components.host != nil,
              components.user == nil,
              components.password == nil,
              components.query == nil,
              components.fragment == nil else {
            return nil
        }
        components.path = path
        return components.url
    }
}

struct BundleProductCommandRunner: ProductCommandRunning {
    private let executor: ProductProcessExecuting
    private let executableURL: URL?

    init(executor: ProductProcessExecuting = BoundedProductProcessExecutor(), executableURL: URL? = nil) {
        self.executor = executor
        self.executableURL = executableURL
    }

    func run(_ args: [String], timeout: TimeInterval) async -> ProductResult {
        guard let helper = executableURL ?? Bundle.main.url(forResource: "devboard-bootstrap", withExtension: nil) else {
            return ProductResult(schemaVersion: 1, ok: false, status: "helper_failed", message: "DevBoard product command failed.")
        }
        let execution = await executor.run(executableURL: helper, arguments: ["product"] + args, timeout: timeout)
        guard !execution.timedOut,
              !execution.overflow,
              execution.stdout.count <= ProductCommandBudget.maxOutputBytes else {
            return ProductResult(schemaVersion: 1, ok: false, status: "helper_failed", message: "DevBoard product command exceeded its bounded execution budget.")
        }
        do {
            // A helper may use a non-zero exit status for a valid product
            // result (for example, quota is degraded but still reportable).
            // The schema/timeout/output checks above are the trust boundary;
            // terminationStatus is deliberately not treated as parse failure.
            return try JSONDecoder().decode(ProductResult.self, from: execution.stdout)
        } catch {
            return ProductResult(schemaVersion: 1, ok: false, status: "helper_failed", message: "DevBoard product command returned invalid data.")
        }
    }
}

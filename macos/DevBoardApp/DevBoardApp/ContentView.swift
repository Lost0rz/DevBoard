import SwiftUI
import AppKit

private enum SettingsTab: String, CaseIterable, Hashable, Identifiable {
    case overview
    case setup
    case usage
    case logs

    var id: String { rawValue }

    var title: String {
        switch self {
        case .overview: return "Status"
        case .setup: return "Setup"
        case .usage: return "Usage"
        case .logs: return "Logs"
        }
    }

    var subtitle: String {
        switch self {
        case .overview: return "See the whole Mac at a glance"
        case .setup: return "Complete the post-install steps"
        case .usage: return "Quota accounts and display names"
        case .logs: return "Local diagnostics and log files"
        }
    }

    var icon: String {
        switch self {
        case .overview: return "waveform.path.ecg"
        case .setup: return "checklist"
        case .usage: return "chart.bar.xaxis"
        case .logs: return "doc.text.magnifyingglass"
        }
    }
}

private struct LocalLogFile: Identifiable {
    let id: String
    let name: String
    let description: String
}

struct SettingsView: View {
    @Environment(\.scenePhase) private var scenePhase
    @ObservedObject var controller: NodeController

    @State private var selectedTab: SettingsTab = .overview

    private let logFiles = [
        LocalLogFile(id: "node-out", name: "node.out.log", description: "Normal Node lifecycle and runtime output."),
        LocalLogFile(id: "node-err", name: "node.err.log", description: "Node warnings and failures. Credentials are redacted by the product boundary.")
    ]

    var body: some View {
        NavigationSplitView {
            List(selection: $selectedTab) {
                Section("Settings") {
                    ForEach(SettingsTab.allCases) { tab in
                        Label {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(tab.title)
                                Text(tab.subtitle)
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                            }
                        } icon: {
                            Image(systemName: tab.icon)
                                .frame(width: 22)
                        }
                        .tag(tab)
                        .padding(.vertical, 3)
                    }
                }
            }
            .listStyle(.sidebar)
            .navigationTitle("DevBoard")
            .navigationSplitViewColumnWidth(min: 190, ideal: 215, max: 245)
        } detail: {
            VStack(spacing: 0) {
                pageHeader
                Divider()
                ScrollView {
                    activePage
                        .frame(maxWidth: 900, alignment: .leading)
                        .padding(28)
                }
            }
        }
        .frame(minWidth: 920, minHeight: 620)
        .onAppear {
            controller.refresh()
            controller.prepareMacSetup()
        }
        .onChange(of: scenePhase) { phase in
            if phase == .active {
                controller.refresh()
                controller.prepareMacSetup()
            }
        }
    }

    private var pageHeader: some View {
        HStack(alignment: .center, spacing: 14) {
            VStack(alignment: .leading, spacing: 4) {
                Text("SETTINGS")
                    .font(.caption)
                    .fontWeight(.bold)
                    .foregroundStyle(.secondary)
                Text(selectedTab.title)
                    .font(.largeTitle)
                    .fontWeight(.bold)
                Text(selectedTab.subtitle)
                    .font(.callout)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if controller.busy {
                ProgressView()
                    .controlSize(.small)
                    .accessibilityLabel("Updating")
            }
            Button {
                controller.refresh()
                controller.prepareMacSetup()
            } label: {
                Image(systemName: "arrow.clockwise")
            }
            .buttonStyle(.bordered)
            .help("Refresh all status")
        }
        .padding(.horizontal, 28)
        .padding(.vertical, 22)
    }

    @ViewBuilder
    private var activePage: some View {
        switch selectedTab {
        case .overview:
            overviewPage
        case .setup:
            setupPage
        case .usage:
            usagePage
        case .logs:
            logsPage
        }
    }

    private var overviewPage: some View {
        VStack(alignment: .leading, spacing: 20) {
            HStack(alignment: .top, spacing: 14) {
                Image(systemName: overallState.icon)
                    .font(.system(size: 30, weight: .semibold))
                    .foregroundStyle(stateColor(overallState))
                    .frame(width: 46, height: 46)
                    .background(stateColor(overallState).opacity(0.12))
                    .clipShape(RoundedRectangle(cornerRadius: 13))
                VStack(alignment: .leading, spacing: 4) {
                    Text(overallTitle)
                        .font(.title2)
                        .fontWeight(.semibold)
                    Text(overallDescription)
                        .foregroundStyle(.secondary)
                }
                Spacer()
                Text(overallState.title)
                    .font(.callout.weight(.semibold))
                    .foregroundStyle(stateColor(overallState))
            }
            .padding(20)
            .background(.quaternary.opacity(0.45))
            .clipShape(RoundedRectangle(cornerRadius: 16))

            SettingsSection(
                title: "Connection status",
                eyebrow: "LIVE STATUS",
                subtitle: "These signals are read-only here. Use Setup when something needs changing."
            ) {
                LazyVGrid(columns: [GridItem(.flexible()), GridItem(.flexible()), GridItem(.flexible())], spacing: 12) {
                    SettingsStatusCard(title: "Background Node", state: controller.menuStatus.node)
                    SettingsStatusCard(title: "Hub", state: controller.menuStatus.hub)
                    SettingsStatusCard(title: "Codex", state: controller.menuStatus.codex)
                    SettingsStatusCard(title: "Claude Code", state: controller.menuStatus.claudeCode)
                    SettingsStatusCard(title: "Quota", state: controller.menuStatus.quota)
                    SettingsStatusCard(title: "Refresh", state: refreshSurfaceState)
                }
                HStack {
                    Label("Last checked", systemImage: "clock")
                        .foregroundStyle(.secondary)
                    Spacer()
                    Text(lastRefreshText)
                        .textSelection(.enabled)
                }
                .font(.callout)
            }

            SettingsSection(
                title: "Common actions",
                eyebrow: "SHORTCUTS",
                subtitle: "Useful views live here without duplicating configuration controls."
            ) {
                HStack(spacing: 10) {
                    Button("Open Monitoring Display") { controller.openDisplay() }
                    Button("Open NAS Admin") { controller.openHub(path: "/admin") }
                        .disabled(!controller.hubConfigured)
                }
            }

            noticeView
        }
    }

    private var setupPage: some View {
        VStack(alignment: .leading, spacing: 20) {
            SettingsSection(
                title: "Mac identity",
                eyebrow: "STEP 1 · IDENTITY",
                subtitle: "The Node ID is fixed for this Mac. The display name is only the label shown in DevBoard."
            ) {
                SettingsDetail(label: "Node ID", value: controller.setupState?.nodeID ?? "Preparing…")
                SettingsDetail(
                    label: "Node status",
                    value: controller.serviceHealthy ? "LaunchAgent-owned" : "Not verified"
                )
            }

            SettingsSection(
                title: "Background Node service",
                eyebrow: "POST-INSTALL SERVICE",
                subtitle: "Install or repair the per-user LaunchAgent without changing the NAS connection or saved credentials."
            ) {
                HStack(alignment: .center, spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text(controller.serviceHealthy ? "Background Node is healthy" : "Background Node needs attention")
                            .fontWeight(.semibold)
                        Text("Use this after installing a new DMG, or when the Node is not running. It preserves the existing node.yaml.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button("Install or Repair Node") { controller.installOrRepairNode() }
                        .buttonStyle(.borderedProminent)
                        .disabled(controller.busy)
                }
            }

            SettingsSection(
                title: "Hub connection",
                eyebrow: "STEP 1 · HUB CONNECTION",
                subtitle: "All Hub connection fields are kept together. Leave the token empty to keep the existing token."
            ) {
                VStack(alignment: .leading, spacing: 12) {
                    TextField("Display name", text: $controller.setupDisplayName)
                        .textFieldStyle(.roundedBorder)
                    TextField("Hub endpoint, e.g. http://192.168.28.103:8787", text: $controller.setupEndpoint)
                        .textFieldStyle(.roundedBorder)
                    SecureField(
                        controller.setupState?.tokenConfigured == true ? "Node token (leave blank to keep current)" : "Node token",
                        text: $controller.setupToken
                    )
                    .textFieldStyle(.roundedBorder)
                    HStack(spacing: 10) {
                        Button("Save & Test") { controller.saveMacSetup() }
                            .buttonStyle(.borderedProminent)
                            .disabled(controller.setupBusy || controller.setupState == nil)
                        Spacer()
                    }
                    Text("Save & Test persists the protected config, repairs the Node when needed, and verifies the resulting connection.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
            }

            SettingsSection(
                title: "Provider connections",
                eyebrow: "STEP 2 · CODE HOOKS",
                subtitle: "Install / Repair is only needed once or when status reports missing or partial hooks. Once configured, this requires no daily action."
            ) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(IntegrationProvider.allCases) { provider in
                        ProviderConnectionRow(
                            provider: provider,
                            message: integrationMessage(provider),
                            setupTitle: providerSetupTitle(provider),
                            setupDisabled: !providerNeedsSetup(provider),
                            action: { controller.install(provider: provider) }
                        )
                        if provider != IntegrationProvider.allCases.last {
                            Divider().padding(.vertical, 12)
                        }
                    }
                }
            }

            SettingsSection(
                title: "Launch at login",
                eyebrow: "STEP 3 · APP STARTUP",
                subtitle: "Keep the DevBoard menu bar app available after you sign in to macOS. This is separate from the background Node service."
            ) {
                HStack(alignment: .center, spacing: 12) {
                    VStack(alignment: .leading, spacing: 3) {
                        Text("Start DevBoard when you sign in")
                            .fontWeight(.semibold)
                        Text(launchAtLoginStatus)
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    if controller.loginItemState == "requires_approval" {
                        Button("Open Login Items") { controller.openLoginItemsSettings() }
                    }
                    Toggle("Start DevBoard when you sign in", isOn: Binding(
                        get: { controller.loginItemState == "enabled" },
                        set: { controller.setLaunchAtLogin($0) }
                    ))
                    .labelsHidden()
                }
            }

            noticeView
        }
    }

    private var usagePage: some View {
        VStack(alignment: .leading, spacing: 20) {
            SettingsSection(
                title: "Quota accounts",
                eyebrow: "STEP 4 · QUOTA",
                subtitle: "Detect the supported local accounts, edit saved display names, and let the background Node publish sanitized usage."
            ) {
                HStack {
                    VStack(alignment: .leading, spacing: 4) {
                        Text(quotaStatusTitle)
                            .fontWeight(.semibold)
                        Text("Credentials stay in their provider locations; DevBoard stores only the safe account-to-label mapping.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                    Spacer()
                    Button(quotaDetectionButtonTitle) { controller.detectQuota() }
                    Button("Confirm & Save names") { controller.saveQuota() }
                        .buttonStyle(.borderedProminent)
                        .disabled(!canSaveQuota)
                }

                Text("Detect or refresh the accounts to load the current saved names. Edit any name below, then Confirm & Save names writes the mapping without Terminal commands.")
                    .font(.caption)
                    .foregroundStyle(.secondary)

                if let accounts = controller.quotaDetectionResult?.quotaAccounts, !accounts.isEmpty {
                    VStack(alignment: .leading, spacing: 10) {
                        Divider().padding(.vertical, 2)
                        ForEach(accounts) { account in
                            HStack(alignment: .center, spacing: 12) {
                                Image(systemName: account.provider == "zai" ? "sparkles" : "person.crop.circle")
                                    .foregroundStyle(.secondary)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(account.provider == "zai" ? "Z.ai account" : "Codex account")
                                        .fontWeight(.semibold)
                                    Text(account.sourceHealth)
                                        .font(.caption)
                                        .foregroundStyle(.secondary)
                                }
                                Spacer()
                                TextField("Display name", text: Binding(
                                    get: { controller.quotaLabels[account.accountKey] ?? "" },
                                    set: { controller.setQuotaLabel($0, for: account.accountKey) }
                                ))
                                .textFieldStyle(.roundedBorder)
                                .frame(width: 210)
                            }
                        }
                        Text("Saved names remain editable here. Changes take effect after Confirm & Save names.")
                            .font(.caption)
                            .foregroundStyle(.secondary)
                    }
                } else {
                    Text("No accounts loaded yet. Select Detect accounts to load local identities and any saved names.")
                        .font(.callout)
                        .foregroundStyle(.secondary)
                }
            }

            SettingsSection(
                title: "How this works",
                eyebrow: "SAFE BY DEFAULT",
                subtitle: "Quota configuration is separate from Hub credentials, but it is now in one predictable place."
            ) {
                SettingsBullet(text: "Detect reads supported local provider status with a bounded timeout.")
                SettingsBullet(text: "Confirm & Save names writes only sanitized display labels and restarts the Node automatically when required.")
                SettingsBullet(text: "Account keys and credentials never appear in the UI, logs, or process arguments.")
            }

            noticeView
        }
        .onAppear { controller.prepareQuotaEditing() }
    }

    private var logsPage: some View {
        VStack(alignment: .leading, spacing: 20) {
            SettingsSection(
                title: "Local logs",
                eyebrow: "DIAGNOSTICS",
                subtitle: "Logs are grouped by purpose so normal output and failures are easy to find. They remain on this Mac."
            ) {
                VStack(alignment: .leading, spacing: 0) {
                    ForEach(logFiles) { file in
                        HStack(alignment: .top, spacing: 12) {
                            Image(systemName: "doc.text")
                                .foregroundStyle(.secondary)
                                .frame(width: 22)
                            VStack(alignment: .leading, spacing: 3) {
                                Text(file.name)
                                    .fontWeight(.semibold)
                                Text(file.description)
                                    .font(.caption)
                                    .foregroundStyle(.secondary)
                            }
                            Spacer()
                            Button("Reveal") { controller.revealLocalLog(named: file.name) }
                        }
                        if file.id != logFiles.last?.id {
                            Divider().padding(.vertical, 12)
                        }
                    }
                }
                HStack {
                    Text("~/Library/Logs/DevBoard")
                        .font(.caption.monospaced())
                        .foregroundStyle(.secondary)
                        .textSelection(.enabled)
                    Spacer()
                    Button("Open log folder") { controller.openLocalLogs() }
                }
                .padding(.top, 12)
            }

            SettingsSection(
                title: "Refresh information",
                eyebrow: "STATUS CONTEXT",
                subtitle: "Use the status tab for the current connection picture; this page is for inspecting local evidence."
            ) {
                SettingsDetail(label: "Last status check", value: lastRefreshText)
                SettingsDetail(label: "Refresh state", value: controller.refreshState.rawValue.capitalized)
            }

            noticeView
        }
    }

    private var noticeView: some View {
        Group {
            if let notice = controller.notice {
                Label(notice, systemImage: "info.circle")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                    .textSelection(.enabled)
                    .padding(.horizontal, 4)
            }
        }
    }

    private var overallState: MenuSurfaceState {
        if controller.refreshState == .unavailable { return .unavailable }
        if controller.menuStatus.node == .healthy && controller.menuStatus.hub == .connected {
            return .connected
        }
        if controller.menuStatus.node == .notRunning || controller.menuStatus.hub == .notConfigured {
            return .notConfigured
        }
        return .staleOrDegraded
    }

    private var overallTitle: String {
        switch overallState {
        case .connected: return "This Mac is connected"
        case .notConfigured: return "Connection needs setup"
        case .unavailable: return "Status is unavailable"
        default: return "Connection needs attention"
        }
    }

    private var overallDescription: String {
        switch overallState {
        case .connected: return "The background Node is running and the Hub accepted the latest connection."
        case .notConfigured: return "Open Setup to connect this Mac and configure the optional code hooks."
        case .unavailable: return "DevBoard could not verify the local product helper. Try Refresh or inspect Logs."
        default: return "One or more signals are stale, disconnected, or waiting for provider setup."
        }
    }

    private var refreshSurfaceState: MenuSurfaceState {
        switch controller.refreshState {
        case .fresh: return .healthy
        case .degraded: return .staleOrDegraded
        case .unavailable: return .unavailable
        }
    }

    private var lastRefreshText: String {
        guard let date = controller.lastRefreshAt else { return "Not checked yet" }
        return date.formatted(date: .omitted, time: .shortened)
    }

    private var quotaStatusTitle: String {
        guard let result = controller.quotaStatusResult else { return "Quota status not loaded" }
        return result.message ?? result.status.replacingOccurrences(of: "_", with: " ").capitalized
    }

    private var quotaDetectionButtonTitle: String {
        if let accounts = controller.quotaDetectionResult?.quotaAccounts, !accounts.isEmpty {
            return "Refresh accounts"
        }
        return "Detect accounts"
    }

    private var launchAtLoginStatus: String {
        switch controller.loginItemState {
        case "enabled": return "Enabled in macOS Login Items."
        case "requires_approval": return "macOS requires approval in System Settings → General → Login Items."
        case "not_registered": return "Disabled. Turn this on to register DevBoard with macOS."
        default: return "Login Items are unavailable for this app build."
        }
    }

    private var canSaveQuota: Bool {
        guard let allAccounts = controller.quotaDetectionResult?.quotaAccounts else { return false }
        let codex = allAccounts.filter { $0.provider == "codex" }
        let glm = allAccounts.filter { $0.provider == "zai" }
        let accounts = codex + glm
        let labels = accounts.compactMap {
            controller.quotaLabels[$0.accountKey]?.trimmingCharacters(in: .whitespacesAndNewlines)
        }
        return codex.count == 2 && glm.count == 1 && labels.count == accounts.count && Set(labels).count == labels.count
    }

    private func integrationMessage(_ provider: IntegrationProvider) -> String {
        guard let result = controller.integrationStatus(for: provider) else { return "Status unavailable" }
        if provider == .codex && result.status == "configured_requires_trust" {
            return "Configured. Desktop trust cannot be verified here; local session monitoring remains available."
        }
        return result.message ?? result.status.replacingOccurrences(of: "_", with: " ").capitalized
    }

    private func providerNeedsSetup(_ provider: IntegrationProvider) -> Bool {
        guard let result = controller.integrationStatus(for: provider) else { return true }
        switch result.status {
        case "not_configured", "repair_required", "unavailable", "helper_failed", "stable_binary_missing":
            return true
        default:
            return false
        }
    }

    private func providerSetupTitle(_ provider: IntegrationProvider) -> String {
        guard let result = controller.integrationStatus(for: provider) else { return "Install hooks" }
        switch result.status {
        case "not_configured": return "Install hooks"
        case "repair_required", "unavailable", "helper_failed", "stable_binary_missing": return "Repair hooks"
        case "configured": return "Configured"
        case "configured_requires_trust": return "Configured"
        case "configured_but_disabled": return "Disabled by provider"
        case "manual_configuration_required": return "Manual setup required"
        default: return "Review status"
        }
    }

    private func stateColor(_ state: MenuSurfaceState) -> Color {
        switch state {
        case .healthy, .connected, .available: return .green
        case .attention, .staleOrDegraded: return .orange
        case .notConfigured, .notRunning: return .secondary
        case .unhealthy, .unavailable, .cliUnavailable, .disconnected: return .red
        }
    }
}

private struct SettingsSection<Content: View>: View {
    let title: String
    let eyebrow: String
    let subtitle: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(eyebrow)
                .font(.caption)
                .fontWeight(.bold)
                .foregroundStyle(.secondary)
            Text(title)
                .font(.title2)
                .fontWeight(.semibold)
            Text(subtitle)
                .font(.callout)
                .foregroundStyle(.secondary)
            content()
        }
        .padding(18)
        .background(.quaternary.opacity(0.35))
        .clipShape(RoundedRectangle(cornerRadius: 14))
    }
}

private struct SettingsStatusCard: View {
    let title: String
    let state: MenuSurfaceState

    var body: some View {
        HStack(alignment: .top, spacing: 9) {
            Image(systemName: state.icon)
                .foregroundStyle(color)
            VStack(alignment: .leading, spacing: 3) {
                Text(title)
                    .font(.callout.weight(.semibold))
                Text(state.title)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer(minLength: 0)
        }
        .padding(12)
        .background(Color(nsColor: .controlBackgroundColor).opacity(0.72))
        .clipShape(RoundedRectangle(cornerRadius: 11))
    }

    private var color: Color {
        switch state {
        case .healthy, .connected, .available: return .green
        case .attention, .staleOrDegraded: return .orange
        case .notConfigured, .notRunning: return .secondary
        case .unhealthy, .unavailable, .cliUnavailable, .disconnected: return .red
        }
    }
}

private struct ProviderConnectionRow: View {
    let provider: IntegrationProvider
    let message: String
    let setupTitle: String
    let setupDisabled: Bool
    let action: () -> Void

    var body: some View {
        HStack(alignment: .top, spacing: 12) {
            Image(systemName: provider == .codex ? "chevron.left.forwardslash.chevron.right" : "terminal")
                .foregroundStyle(.secondary)
                .frame(width: 24)
            VStack(alignment: .leading, spacing: 3) {
                Text(provider.title)
                    .fontWeight(.semibold)
                Text(message)
                    .font(.caption)
                    .foregroundStyle(.secondary)
            }
            Spacer()
            Button(setupTitle, action: action)
                .disabled(setupDisabled)
        }
    }
}

private struct SettingsDetail: View {
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(label)
                .foregroundStyle(.secondary)
            Spacer()
            Text(value)
                .multilineTextAlignment(.trailing)
                .textSelection(.enabled)
        }
        .font(.callout)
    }
}

private struct SettingsBullet: View {
    let text: String

    var body: some View {
        Label(text, systemImage: "checkmark.circle")
            .font(.callout)
            .foregroundStyle(.secondary)
    }
}

struct MenuBarView: View {
    @ObservedObject var controller: NodeController
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            MenuConnectionSummary(controller: controller)
            Divider()
            Button("Settings…") { openWindow(id: "settings") }
            Button("View Monitoring Display") { controller.openDisplay() }
            Button("Open NAS Admin") { controller.openHub(path: "/admin") }
            Divider()
            Button("Quit") {
                // App-only termination. The LaunchAgent-owned Node remains
                // loaded and supervised after this process exits.
                if !AppLifecyclePolicy.quitCallsServiceLifecycle {
                    NSApplication.shared.terminate(nil)
                }
            }
        }
        .padding(14)
        .frame(width: 280)
        .onAppear { controller.menuDidAppear() }
        .onDisappear { controller.menuDidDisappear() }
    }
}

private struct MenuConnectionSummary: View {
    @ObservedObject var controller: NodeController

    var body: some View {
        VStack(alignment: .leading, spacing: 7) {
            Label("Current connection", systemImage: connectionState.icon)
                .font(.headline)
                .foregroundStyle(menuStatusColor(connectionState))
            MenuStatusRow(title: "Node", state: controller.menuStatus.node)
            MenuStatusRow(title: "Hub", state: controller.menuStatus.hub)
            MenuStatusRow(title: "Codex", state: controller.menuStatus.codex)
            MenuStatusRow(title: "Claude Code", state: controller.menuStatus.claudeCode)
            MenuStatusRow(title: "Quota", state: controller.menuStatus.quota)
        }
    }

    private var connectionState: MenuSurfaceState {
        if controller.menuStatus.node == .healthy && controller.menuStatus.hub == .connected {
            return .connected
        }
        if controller.menuStatus.node == .notRunning || controller.menuStatus.hub == .notConfigured {
            return .notConfigured
        }
        if controller.refreshState == .unavailable {
            return .unavailable
        }
        return .staleOrDegraded
    }
}

private struct MenuStatusRow: View {
    let title: String
    let state: MenuSurfaceState

    var body: some View {
        Label("\(title): \(state.title)", systemImage: state.icon)
            .font(.callout)
            .foregroundStyle(menuStatusColor(state))
            .accessibilityLabel("\(title), \(state.title)")
    }
}

private func menuStatusColor(_ state: MenuSurfaceState) -> Color {
    switch state.tone {
    case .healthy:
        return .green
    case .fault:
        return .orange
    case .disconnected:
        return .red
    }
}

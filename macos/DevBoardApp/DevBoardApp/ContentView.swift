import SwiftUI
import AppKit

struct ContentView: View {
    @Environment(\.scenePhase) private var scenePhase
    @ObservedObject var controller: NodeController

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                setupHeader
                setupIdentity
                setupForm
                setupAdvanced
                setupResult
            }
            .padding(28)
        }
        .frame(minWidth: 560, minHeight: 460)
        .onAppear { controller.prepareMacSetup() }
        .onChange(of: scenePhase) { phase in
            if phase == .active {
                controller.prepareMacSetup()
            }
        }
    }

    private var setupHeader: some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 5) {
                Text("DEVBOARD NATIVE SETUP").font(.caption).fontWeight(.bold).foregroundStyle(.secondary)
                Text("Configure Mac").font(.largeTitle).fontWeight(.bold)
                Text("Set up the background Node and test its Hub connection.")
                    .foregroundStyle(.secondary)
            }
            Spacer()
            if controller.setupBusy {
                ProgressView().controlSize(.small)
            }
        }
    }

    private var setupIdentity: some View {
        ProductSection(title: "MAC IDENTITY", subtitle: "Node ID is product-managed and cannot be edited here.") {
            ProductDetail(label: "Node ID", value: controller.setupState?.nodeID ?? "Preparing…")
            ProductDetail(
                label: "Background Node",
                value: controller.serviceHealthy ? "LaunchAgent-owned" : "Install or repair from More…"
            )
        }
    }

    private var setupForm: some View {
        ProductSection(title: "CONFIGURATION", subtitle: "Token input is protected and is never shown after Save & Test.") {
            VStack(alignment: .leading, spacing: 12) {
                TextField("Display name", text: $controller.setupDisplayName)
                    .textFieldStyle(.roundedBorder)
                SecureField(
                    controller.setupState?.tokenConfigured == true ? "Node Token (leave blank to keep current)" : "Node Token",
                    text: $controller.setupToken
                )
                .textFieldStyle(.roundedBorder)
                Button("Save & Test") { controller.saveMacSetup() }
                    .buttonStyle(.borderedProminent)
                    .disabled(controller.setupBusy || controller.setupState == nil)
            }
        }
    }

    private var setupAdvanced: some View {
        DisclosureGroup("Advanced") {
            VStack(alignment: .leading, spacing: 12) {
                Text("The endpoint defaults to the current product configuration. Use Browser Settings only as a fallback.")
                    .font(.callout)
                    .foregroundStyle(.secondary)
                TextField("NAS endpoint", text: $controller.setupEndpoint)
                    .textFieldStyle(.roundedBorder)
                Button("Open Browser Settings") { controller.openLocalSettings() }
            }
            .padding(.top, 8)
        }
    }

    @ViewBuilder
    private var setupResult: some View {
        if let notice = controller.notice {
            Text(notice)
                .font(.callout)
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
        }
    }
}

struct AdvancedView: View {
    @ObservedObject var controller: NodeController

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                Text("More / Advanced").font(.largeTitle).fontWeight(.bold)
                ProductSection(title: "BACKGROUND SERVICE", subtitle: "LaunchAgent remains the Node lifecycle authority.") {
                    HStack {
                        Button("Install / Repair") { controller.installOrRepairNode() }
                        Button("Restart Background Service") { controller.restartNode() }
                    }
                    Text("Quitting DevBoard App never stops or unloads the background Node.")
                        .font(.caption)
                        .foregroundStyle(.secondary)
                }
                integrationsSection
                quotaSection
                ProductSection(title: "FALLBACKS", subtitle: "These surfaces are optional and are not required for first setup.") {
                    Button("Open Browser Settings") { controller.openLocalSettings() }
                    Button("Open Logs") { controller.openLocalLogs() }
                    Toggle("Launch DevBoard App at Login", isOn: Binding(
                        get: { controller.loginItemState == "enabled" },
                        set: { controller.setLaunchAtLogin($0) }
                    ))
                    .disabled(controller.loginItemState == "unavailable")
                    ProductDetail(label: "Login item", value: controller.loginItemState)
                }
                if let notice = controller.notice {
                    Text(notice).font(.callout).foregroundStyle(.secondary)
                }
            }
            .padding(28)
        }
        .frame(minWidth: 620, minHeight: 540)
    }

    private var integrationsSection: some View {
        ProductSection(title: "INTEGRATIONS", subtitle: "Provider Hook maintenance is an advanced action.") {
            VStack(alignment: .leading, spacing: 12) {
                ForEach(IntegrationProvider.allCases) { provider in
                    HStack(alignment: .firstTextBaseline) {
                        VStack(alignment: .leading, spacing: 3) {
                            Text(provider.title).fontWeight(.semibold)
                            Text(integrationMessage(provider)).font(.caption).foregroundStyle(.secondary)
                        }
                        Spacer()
                        Button("Install / Repair") { controller.install(provider: provider) }
                        Button("Remove") { controller.remove(provider: provider) }
                    }
                }
            }
        }
    }

    private var quotaSection: some View {
        ProductSection(title: "QUOTA SETUP", subtitle: "Optional, sanitized provider status. Credentials remain outside DevBoard.") {
            HStack {
                Button("Detect") { controller.detectQuota() }
                Spacer()
                Button("Save") { controller.saveQuota() }
                    .disabled(!canSaveQuota)
            }
            if let accounts = controller.quotaDetectionResult?.quotaAccounts, !accounts.isEmpty {
                VStack(alignment: .leading, spacing: 10) {
                    ForEach(accounts) { account in
                        HStack(alignment: .firstTextBaseline) {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(account.provider == "zai" ? "GLM" : "Codex account")
                                    .fontWeight(.semibold)
                                Text(account.accountKey)
                                    .font(.caption2)
                                    .foregroundStyle(.secondary)
                                    .textSelection(.enabled)
                            }
                            Spacer()
                            if account.provider == "zai" {
                                Label("GLM", systemImage: "checkmark.circle")
                            } else {
                                Picker("Account label", selection: Binding(
                                    get: { controller.quotaLabels[account.accountKey] ?? "" },
                                    set: { controller.setQuotaLabel($0, for: account.accountKey) }
                                )) {
                                    Text("Choose label").tag("")
                                    Text("Codex A").tag("Codex A")
                                    Text("Codex B").tag("Codex B")
                                }
                                .labelsHidden()
                            }
                        }
                    }
                }
            }
        }
    }

    private var canSaveQuota: Bool {
        guard let allAccounts = controller.quotaDetectionResult?.quotaAccounts else { return false }
        let codex = allAccounts.filter { $0.provider == "codex" }
        let glm = allAccounts.filter { $0.provider == "zai" }
        let labels = codex.compactMap { controller.quotaLabels[$0.accountKey] }
        return codex.count == 2 && glm.count == 1 && labels.count == 2 && Set(labels) == Set(["Codex A", "Codex B"])
    }

    private func integrationMessage(_ provider: IntegrationProvider) -> String {
        guard let result = controller.integrationStatus(for: provider) else { return "Status unavailable" }
        if provider == .codex && result.status == "configured_requires_trust" {
            return "Installed; CLI trust still requires user review."
        }
        return result.message ?? result.status
    }
}

struct MenuBarView: View {
    @ObservedObject var controller: NodeController
    @Environment(\.openWindow) private var openWindow

    var body: some View {
        VStack(alignment: .leading, spacing: 9) {
            MenuConnectionSummary(controller: controller)
            Divider()
            Button("Configure Mac…") { openWindow(id: "configureMac") }
            Button("View Monitoring Display") { controller.openDisplay() }
            Button("Open NAS Admin") { controller.openHub(path: "/admin") }
            Menu("More…") {
                Button("Install / Repair") { controller.installOrRepairNode() }
                Button("Restart Background Service") { controller.restartNode() }
                Divider()
                Button("Open Browser Settings") { controller.openLocalSettings() }
                Button("Integrations") { openWindow(id: "advanced") }
                Button("Quota Setup") { openWindow(id: "advanced") }
                Button("Logs") { controller.openLocalLogs() }
            }
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
            .accessibilityLabel("\(title), \(state.title)")
    }
}

private struct ProductDetail: View {
    let label: String
    let value: String

    var body: some View {
        HStack(alignment: .firstTextBaseline) {
            Text(label).foregroundStyle(.secondary)
            Spacer()
            Text(value).multilineTextAlignment(.trailing).textSelection(.enabled)
        }
        .font(.callout)
    }
}

private struct ProductSection<Content: View>: View {
    let title: String
    let subtitle: String
    @ViewBuilder let content: () -> Content

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text(title).font(.title2).fontWeight(.semibold)
            Text(subtitle).font(.callout).foregroundStyle(.secondary)
            content()
        }
        .padding(18)
        .background(.quaternary.opacity(0.35))
        .clipShape(RoundedRectangle(cornerRadius: 14))
    }
}

import SwiftUI

struct ContentView: View {
    @Environment(\.scenePhase) private var scenePhase
    @StateObject private var controller = NodeController()

    var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 18) {
                header
                nodeSection
                hubSection
                integrationsSection
            }
            .padding(28)
            .disabled(controller.busy)
        }
        .frame(minWidth: 640, minHeight: 560)
        .onChange(of: scenePhase) { phase in
            if phase == .active {
                controller.refresh()
            }
        }
    }

    private var header: some View {
        HStack(alignment: .top) {
            VStack(alignment: .leading, spacing: 4) {
                Text("DEVBOARD PRODUCT").font(.caption).fontWeight(.bold).foregroundStyle(.secondary)
                Text("DevBoard").font(.largeTitle).fontWeight(.bold)
                Text("A focused controller for the local Node, Hub connection, and provider integrations.")
                    .foregroundStyle(.secondary)
                if let notice = controller.notice {
                    Text(notice).font(.callout).foregroundStyle(.secondary)
                }
            }
            Spacer()
            if controller.busy {
                ProgressView().controlSize(.small)
            }
            Button("Refresh") { controller.refresh() }
        }
    }

    private var nodeSection: some View {
        ProductSection(title: "DEVBOARD NODE", subtitle: nodeSubtitle) {
            HStack {
                Label(controller.serviceHealthy ? "Verified healthy" : nodeStatusLabel, systemImage: controller.serviceHealthy ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
                Spacer()
                if controller.serviceHealthy {
                    Button("Restart Node") { controller.restartNode() }
                } else {
                    Button("Install / Repair Background Node") { controller.installOrRepairNode() }
                }
                Button("Open Local Settings") { controller.openLocalSettings() }
                    .disabled(!controller.serviceHealthy)
            }
        }
    }

    private var hubSection: some View {
        ProductSection(title: "HUB", subtitle: hubSubtitle) {
            VStack(alignment: .leading, spacing: 8) {
                ProductDetail(label: "Endpoint", value: hubEndpoint)
                ProductDetail(label: "Uplink", value: uplinkStatus)
                ProductDetail(label: "Connection", value: hubStatus)
                if let errorClass = controller.nodeStatus?.lastErrorClass, !errorClass.isEmpty {
                    ProductDetail(label: "Last error", value: errorClass)
                }
                HStack {
                    Spacer()
                    if controller.hubConfigured {
                        Button("Open Hub Dashboard") { controller.openHub(path: "/display") }
                        Button("Open Hub Admin") { controller.openHub(path: "/admin") }
                    }
                }
            }
        }
    }

    private var integrationsSection: some View {
        ProductSection(title: "INTEGRATIONS", subtitle: "Provider hooks are managed at the user level.") {
            VStack(alignment: .leading, spacing: 12) {
                ForEach(IntegrationProvider.allCases) { provider in
                    HStack(alignment: .top) {
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

    private var hubStatus: String {
        guard let node = controller.nodeStatus else { return "Local Node status unavailable" }
        if node.connected { return "Connected" }
        return node.uplinkEnabled ? "Disconnected" : "Not configured"
    }

    private var hubSubtitle: String {
        guard controller.nodeStatus != nil else { return "Available after the managed Node is verified healthy." }
        return controller.hubConfigured ? "Outbound Node to Hub status from the local runtime." : "No Hub endpoint configured. Open Local Settings to connect one."
    }

    private var hubEndpoint: String {
        guard let endpoint = controller.nodeStatus?.hubEndpoint, !endpoint.isEmpty else { return "Not configured" }
        return endpoint
    }

    private var uplinkStatus: String {
        guard let node = controller.nodeStatus else { return "Unavailable" }
        if !node.uplinkEnabled { return "Disabled" }
        return node.uplinkRunning ? "Running" : "Not running"
    }

    private var nodeSubtitle: String {
        controller.serviceResult?.message ?? "Checking the per-user LaunchAgent and local Node ownership."
    }

    private var nodeStatusLabel: String {
        switch controller.serviceResult?.status {
        case "not_running": return "Not running"
        case "unhealthy": return "Ownership or health check failed"
        default: return "Install / Repair required"
        }
    }

    private func integrationMessage(_ provider: IntegrationProvider) -> String {
        guard let result = controller.integrationStatus(for: provider) else { return "Status unavailable" }
        if provider == .codex && result.status == "configured_requires_trust" {
            return "CLI hook configuration installed. Codex Desktop has no /hooks review UI and requires the local session observer."
        }
        return result.message ?? result.status
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

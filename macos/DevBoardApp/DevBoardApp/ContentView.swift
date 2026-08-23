import SwiftUI

struct ContentView: View {
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
        }
        .frame(minWidth: 640, minHeight: 560)
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text("DEVBOARD PRODUCT").font(.caption).fontWeight(.bold).foregroundStyle(.secondary)
            Text("DevBoard").font(.largeTitle).fontWeight(.bold)
            Text("A focused controller for the local Node, Hub connection, and provider integrations.")
                .foregroundStyle(.secondary)
            if let notice = controller.notice {
                Text(notice).font(.callout).foregroundStyle(.secondary)
            }
        }
    }

    private var nodeSection: some View {
        ProductSection(title: "DevBoard Node", subtitle: controller.serviceHealthy ? "Background Node is healthy." : "Background Node is not installed or healthy.") {
            HStack {
                Label(controller.serviceHealthy ? "Running" : "Install / Repair required", systemImage: controller.serviceHealthy ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
                Spacer()
                if controller.serviceHealthy {
                    Button("Restart Node") { controller.restartNode() }
                } else {
                    Button("Install / Repair Background Node") { controller.installOrRepairNode() }
                }
                Button("Open Local Settings") { controller.openLocalSettings() }
            }
        }
    }

    private var hubSection: some View {
        ProductSection(title: "Hub", subtitle: hubSubtitle) {
            HStack {
                Label(hubStatus, systemImage: controller.nodeStatus?.connected == true ? "arrow.triangle.2.circlepath.circle.fill" : "circle.dashed")
                Spacer()
                if controller.hubConfigured {
                    Button("Open Hub Dashboard") { controller.openHub(path: "/display") }
                    Button("Open Hub Admin") { controller.openHub(path: "/admin") }
                }
            }
        }
    }

    private var integrationsSection: some View {
        ProductSection(title: "Integrations", subtitle: "Provider hooks are managed at the user level.") {
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
        return node.connected ? "Connected" : (node.uplinkEnabled ? "Configured, not connected" : "Uplink disabled")
    }

    private var hubSubtitle: String {
        guard let endpoint = controller.nodeStatus?.hubEndpoint, !endpoint.isEmpty else { return "No Hub endpoint configured." }
        return endpoint
    }

    private func integrationMessage(_ provider: IntegrationProvider) -> String {
        guard let result = controller.integrationStatus(for: provider) else { return "Status unavailable" }
        if provider == .codex && result.status == "configured_requires_trust" {
            return "Configuration installed. Review and trust the DevBoard hook in Codex /hooks."
        }
        return result.message ?? result.status
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

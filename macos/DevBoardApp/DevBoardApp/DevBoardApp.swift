import SwiftUI
import AppKit

@main
struct DevBoardApp: App {
    @StateObject private var controller = NodeController()
    @NSApplicationDelegateAdaptor(DevBoardAppDelegate.self) private var appDelegate

    var body: some Scene {
        MenuBarExtra {
            MenuBarView(controller: controller)
        } label: {
            Label("DevBoard", systemImage: "chart.bar.xaxis")
        }
        .menuBarExtraStyle(.window)

        Window("Settings", id: "settings") {
            SettingsView(controller: controller)
        }
        .windowResizability(.contentMinSize)
    }
}

final class DevBoardAppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
    }
}

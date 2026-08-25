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

        Window("Configure Mac", id: "configureMac") {
            ContentView(controller: controller)
        }
        .windowResizability(.contentSize)

        Window("DevBoard Advanced", id: "advanced") {
            AdvancedView(controller: controller)
        }
        .windowResizability(.contentSize)
    }
}

final class DevBoardAppDelegate: NSObject, NSApplicationDelegate {
    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
    }
}

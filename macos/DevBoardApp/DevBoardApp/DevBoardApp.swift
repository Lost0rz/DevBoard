import SwiftUI

@main
struct DevBoardApp: App {
    var body: some Scene {
        WindowGroup("DevBoard") {
            ContentView()
        }
        .windowResizability(.contentSize)
    }
}

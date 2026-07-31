import SwiftUI

@main
struct CPAMenuBarApp: App {
    @StateObject private var model = AppModel()

    var body: some Scene {
        MenuBarExtra {
            MenuContentView(model: model)
        } label: {
            Label(model.menuLabel, systemImage: model.statusSymbol)
        }
        .menuBarExtraStyle(.window)
    }
}

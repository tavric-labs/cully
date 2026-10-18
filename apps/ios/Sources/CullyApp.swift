import SwiftUI

@main struct CullyMobileApp: App {
    @StateObject private var model = CullyModel()
    @Environment(\.scenePhase) private var scenePhase

    var body: some Scene {
        WindowGroup {
            CullyRootView().environmentObject(model).preferredColorScheme(.dark)
                .onChange(of: scenePhase) { phase in
                    if phase == .background { model.pause() }
                }
        }
    }
}

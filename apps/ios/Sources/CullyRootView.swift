import SwiftUI

private enum CullyTab: String, CaseIterable { case today = "Today", tasks = "Tasks", advisor = "Advisor"
    var icon: String { switch self { case .today: "sun.max"; case .tasks: "checkmark.circle"; case .advisor: "sparkle" } }
}
private enum Theme {
    static let background = Color(red: 15/255, green: 27/255, blue: 23/255)
    static let card = Color(red: 27/255, green: 46/255, blue: 37/255)
    static let gold = Color(red: 238/255, green: 185/255, blue: 110/255)
    static let ink = Color(red: 235/255, green: 239/255, blue: 232/255)
}

struct CullyRootView: View {
    @EnvironmentObject private var model: CullyModel
    @State private var tab = CullyTab.today
    @State private var showConnect = false
    @State private var showTask = false
    @State private var draft = ""
    @State private var showConversation = false
    @State private var question = ""
    @StateObject private var voice = LocalVoiceCapture()
    @Environment(\.scenePhase) private var phase

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                CullyBrandMark().frame(width: 44, height: 44).accessibilityLabel("Cully")
                VStack(alignment: .leading, spacing: 2) {
                    Text("Cully").font(.system(size: 30, weight: .semibold, design: .rounded))
                    Text("A little clarity. A good next step.").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                Text("SELF").font(.caption2.weight(.semibold)).tracking(2).padding(10)
                    .background(Theme.card, in: Capsule()).foregroundStyle(Theme.gold)
            }.padding(.horizontal, 22).padding(.top, 18).padding(.bottom, 14)
            TabView(selection: $tab) {
                today.tag(CullyTab.today).tabItem { Label("Today", systemImage: CullyTab.today.icon) }
                tasks.tag(CullyTab.tasks).tabItem { Label("Tasks", systemImage: CullyTab.tasks.icon) }
                advisor.tag(CullyTab.advisor).tabItem { Label("Advisor", systemImage: CullyTab.advisor.icon) }
            }.tint(Theme.gold)
            HStack(spacing: 12) {
                TextField("Ask Cully…", text: $question).textFieldStyle(.roundedBorder).onSubmit { ask() }
                Button { Task { await voice.toggle() } } label: { Image(systemName: voice.recording ? "stop.fill" : "mic.fill") }
                    .accessibilityLabel(voice.recording ? "Stop listening" : "Speak to Cully")
                Button { ask() } label: { Image(systemName: "arrow.up.circle.fill").font(.title2) }
                    .accessibilityLabel("Send question").disabled(question.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.chatBusy)
            }.padding(.horizontal, 22).padding(.top, 10).tint(Theme.gold)
            if voice.recording { Text(voice.status).font(.caption).foregroundStyle(.secondary) }
            Button { showConnect = true } label: {
                HStack { Circle().fill(model.connection == "Cully connected" ? .green : .secondary).frame(width: 6, height: 6)
                    Text(model.connection).font(.caption); Spacer(); Image(systemName: "arrow.up.right") }
                    .foregroundStyle(.secondary).padding(.horizontal, 22).padding(.vertical, 10)
            }.buttonStyle(.plain)
        }.foregroundStyle(Theme.ink).background(Theme.background)
        .frame(minWidth: 330, minHeight: 600)
        .sheet(isPresented: $showConnect) { connectionSheet }
        .sheet(isPresented: $showTask) { taskSheet }
        .sheet(isPresented: $showConversation) { conversationSheet }
        .onChange(of: voice.transcript) { question = $0 }
        .onChange(of: phase) { if $0 != .active { voice.stop() } }
        .alert("Cully", isPresented: Binding(get: { model.message != nil }, set: { if !$0 { model.message = nil } })) {
            Button("OK") { model.message = nil }
        } message: { Text(model.message ?? "") }
    }

    private var today: some View {
        page {
            Text(Date(), format: .dateTime.weekday(.wide).month(.abbreviated).day()).font(.caption).foregroundStyle(Theme.gold)
            heading("A little clarity.\nA good next step.", subtitle: "Your work, gathered into one calm place.")
            card("YOUR FOCUS") {
                Text(model.focus?.title ?? "One thing at a time.").font(.title2.weight(.medium))
                Text(model.focus == nil ? "Choose a task to keep close today." : "A personal task saved on this device.").font(.subheadline).foregroundStyle(.secondary)
                Button(model.focus == nil ? "Choose a focus" : "View your tasks") { tab = .tasks }.buttonStyle(.bordered)
            }
            card("YOUR BRIEF") {
                Text("\(model.openTasks.count) open · \(model.tasks.filter(\.completed).count) completed").font(.headline)
                if let updated = model.briefUpdated {
                    Text("Refreshed \(updated.formatted(date: .omitted, time: .shortened)) · saved context, not live activity").font(.caption).foregroundStyle(.secondary)
                } else { Text("Bring in recent work when you are ready.").font(.subheadline).foregroundStyle(.secondary) }
                if model.notes.isEmpty {
                    Text("Your recent handoffs and next steps will appear here. No updates have been loaded yet.").foregroundStyle(.secondary)
                }
                ForEach(model.notes.prefix(3)) { note in
                    VStack(alignment: .leading, spacing: 5) {
                        Text(note.summary).font(.subheadline)
                        Text("Saved · \(note.date) · \(note.assistant)").font(.caption2).foregroundStyle(.secondary)
                    }.padding(.vertical, 4)
                }
                Button { model.refreshBrief() } label: { Label(model.busy ? "Refreshing…" : "Refresh brief", systemImage: "arrow.clockwise") }
                    .buttonStyle(.bordered).disabled(model.busy)
            }
            LocalSetupCard(setup: model.setup)
            Text("Personal tasks stay on this device. Your brief reads your private Cully context when you ask.").font(.caption).foregroundStyle(.secondary)
        }
    }

    private var tasks: some View {
        page {
            heading("Make space\nfor what matters.", subtitle: "Personal tasks · saved on this device")
            Button { draft = ""; showTask = true } label: { Label("Add a task", systemImage: "plus").frame(maxWidth: .infinity).padding(7) }.buttonStyle(.borderedProminent)
            if model.tasks.isEmpty { card("A FRESH START") { Text("What is one thing you want to move forward?").font(.title2); Text("Keep it small. You can choose a focus after adding it.").foregroundStyle(.secondary) } }
            ForEach(model.tasks) { task in
                card(task.completed ? "COMPLETED" : model.focusID == task.id ? "YOUR FOCUS" : "NEXT UP") {
                    HStack(alignment: .top) {
                        Button { model.toggleTask(task.id) } label: { Image(systemName: task.completed ? "checkmark.circle.fill" : "circle").font(.title2) }.buttonStyle(.plain)
                        Text(task.title).font(.title3).strikethrough(task.completed).frame(maxWidth: .infinity, alignment: .leading)
                    }
                    HStack {
                        if !task.completed { Button(model.focusID == task.id ? "Clear focus" : "Focus on this") { model.focusTask(task.id) }.buttonStyle(.bordered) }
                        Spacer(); Button("Remove", role: .destructive) { model.removeTask(task.id) }.font(.caption)
                    }
                }
            }
        }
    }

    private var advisor: some View {
        page {
            heading("A useful nudge.\nRoom to decide.", subtitle: "Based on your tasks and the saved context you loaded.")
            card("A SMALL NEXT STEP") {
                Text(model.focus.map { "Start with “\($0.title)”." } ?? (model.openTasks.isEmpty ? "Capture the thing on your mind." : "Choose one open task to focus on.")).font(.title2)
                Text("A suggestion from your local task list. You stay in control.").font(.caption).foregroundStyle(.secondary)
                Button("Choose your next step") { tab = .tasks }.buttonStyle(.bordered)
            }
            ForEach(model.notes.filter { !($0.issue ?? "").isEmpty || !($0.nextSteps ?? "").isEmpty }.prefix(3)) { note in
                card("FROM A SAVED HANDOFF · \(note.date)") {
                    if let issue = note.issue, !issue.isEmpty { Text(issue).font(.headline) }
                    if let next = note.nextSteps, !next.isEmpty {
                        Text(next)
                        Button("Make this a personal task") { draft = next; showTask = true }.buttonStyle(.bordered)
                    }
                    Text("Reported by \(note.assistant) · may have changed since this note").font(.caption2).foregroundStyle(.secondary)
                }
            }
            Text("Live coding-session health is not connected in this preview. Refresh your brief for reported blockers and next steps.").font(.caption).foregroundStyle(.secondary)
        }
    }

    private func ask() {
        let text = question.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty else { return }
        voice.stop(); question = ""; showConversation = true; model.ask(text)
    }
    private var conversationSheet: some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack { Text("Ask Cully").font(.title.bold()); Spacer(); Button("Done") { showConversation = false } }
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    ForEach(model.conversation) { item in
                        VStack(alignment: .leading, spacing: 6) {
                            Text(item.role == "user" ? "YOU" : "CULLY").font(.caption2).foregroundStyle(Theme.gold)
                            Text(item.text).textSelection(.enabled)
                        }.padding(18).frame(maxWidth: .infinity, alignment: .leading).background(Theme.card, in: RoundedRectangle(cornerRadius: 18))
                    }
                    if model.chatBusy { ProgressView("Thinking on your device…") }
                }
            }
            HStack {
                TextField("Ask Cully…", text: $question).textFieldStyle(.roundedBorder).onSubmit { ask() }
                Button { Task { await voice.toggle() } } label: { Image(systemName: voice.recording ? "stop.fill" : "mic.fill") }.accessibilityLabel("Speak to Cully")
                Button("Send") { ask() }.disabled(question.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.chatBusy)
            }
            Text("Small local model · review important answers. Private Cully context is read only when the model requests it.").font(.caption).foregroundStyle(.secondary)
        }.padding(22).foregroundStyle(Theme.ink).background(Theme.background).tint(Theme.gold).frame(minWidth: 330, minHeight: 480)
        .onDisappear { voice.stop() }
    }
    private var connectionSheet: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("Connect your Cully").font(.title.bold())
            Text("Sign in with your browser. Private service credentials stay on your server.").foregroundStyle(.secondary)
            TextField("HTTPS Cully MCP URL", text: $model.endpoint).textFieldStyle(.roundedBorder)
            Button(model.busy ? "Signing in…" : "Sign in with browser") { model.connect() }.buttonStyle(.borderedProminent).disabled(model.busy)
            Button("Disconnect and sign out", role: .destructive) { model.disconnect() }
            Button("Done") { showConnect = false }
        }.padding(28).frame(minWidth: 310).tint(Theme.gold)
    }
    private var taskSheet: some View {
        VStack(alignment: .leading, spacing: 18) {
            Text("A personal task").font(.title.bold())
            TextField("What would you like to move forward?", text: $draft).textFieldStyle(.roundedBorder)
            Text("Saved on this device. It is not shared to a workspace.").font(.caption).foregroundStyle(.secondary)
            HStack { Button("Cancel") { showTask = false }; Spacer(); Button("Add task") { model.addTask(draft); showTask = false }.buttonStyle(.borderedProminent).disabled(draft.trimmingCharacters(in: .whitespaces).isEmpty) }
        }.padding(28).frame(minWidth: 310).tint(Theme.gold)
    }
    private func page<Content: View>(@ViewBuilder _ content: () -> Content) -> some View {
        ScrollView { VStack(alignment: .leading, spacing: 18, content: content).padding(22).frame(maxWidth: 700, alignment: .leading).frame(maxWidth: .infinity) }.background(Theme.background)
    }
    private func heading(_ title: String, subtitle: String) -> some View {
        VStack(alignment: .leading, spacing: 10) { Text(title).font(.system(size: 34, weight: .semibold, design: .rounded)); Text(subtitle).foregroundStyle(.secondary).font(.subheadline) }.padding(.bottom, 4)
    }
    private func card<Content: View>(_ title: String, @ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 12) { Text(title).font(.caption2.weight(.semibold)).tracking(1.4).foregroundStyle(Theme.gold); content() }
            .padding(20).frame(maxWidth: .infinity, alignment: .leading).background(Theme.card, in: RoundedRectangle(cornerRadius: 22))
    }
}

private struct CullyBrandMark: View {
    var body: some View {
        GeometryReader { proxy in
            let scale = proxy.size.width / 64
            ZStack {
                RoundedRectangle(cornerRadius: 17 * scale).fill(Color(red: 24/255, green: 75/255, blue: 56/255))
                Path { path in
                    path.addArc(center: CGPoint(x: 28.583592135 * scale, y: 32 * scale), radius: 18 * scale,
                                startAngle: .radians(-0.729727656), endAngle: .radians(0.729727656), clockwise: true)
                }.stroke(Color(red: 231/255, green: 246/255, blue: 219/255), style: StrokeStyle(lineWidth: 8 * scale, lineCap: .round))
                Circle().fill(Theme.gold).frame(width: 10 * scale, height: 10 * scale).position(x: 43 * scale, y: 32 * scale)
            }
        }
    }
}

private struct LocalSetupCard: View {
    @ObservedObject var setup: ModelSetup
    @State private var confirm = false
    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("ON YOUR DEVICE").font(.caption2.weight(.semibold)).foregroundStyle(Theme.gold)
            Text(setup.status).font(.subheadline)
            if setup.busy { ProgressView(value: setup.progress); Button("Pause download") { setup.cancel() } }
            else if !setup.ready { Button("Set up Cully · 469 MiB") { confirm = true }.buttonStyle(.borderedProminent) }
            Text("One language model download. Voice uses your iPhone’s on-device speech support. No cloud inference fee.").font(.caption).foregroundStyle(.secondary)
        }.padding(20).frame(maxWidth: .infinity, alignment: .leading).background(Theme.card, in: RoundedRectangle(cornerRadius: 22))
        .alert("Set up local AI?", isPresented: $confirm) {
            Button("Later", role: .cancel) {}; Button("Download") { setup.start() }
        } message: { Text("Download 469 MiB of model weights. Allow 650 MiB of free space and use Wi-Fi if possible. Nothing downloads until you press Download.") }
    }
}

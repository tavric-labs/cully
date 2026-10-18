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
    @State private var weekly = false
    @State private var tab = CullyTab.today
    @State private var showConnect = false
    @State private var showTask = false
    @State private var draft = ""
    @State private var selectedNote: ContextNote?
    @State private var showConversation = false
    @State private var question = ""
    @StateObject private var voice = LocalVoiceCapture()
    @Environment(\.scenePhase) private var phase

    var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 12) {
                CullyBrandMark().frame(width: 44, height: 44).accessibilityLabel("Cully")
                VStack(alignment: .leading, spacing: 2) {
                    Text("Cully").font(.title.weight(.semibold))
                    Text("A little clarity. A good next step.").font(.caption).foregroundStyle(.secondary)
                }
                Spacer()
                Button { showConnect = true } label: {
                    HStack(spacing: 6) { Circle().fill(model.connection == "Cully connected" ? Color.green : Theme.gold).frame(width: 6, height: 6); Text(model.connection == "Cully connected" ? "SELF" : "Connect").font(.caption.weight(.semibold)) }.padding(12).background(Theme.card, in: Capsule())
                }.buttonStyle(.plain).accessibilityLabel(model.connection)
            }.padding(.horizontal, 22).padding(.top, 18).padding(.bottom, 14)
            TabView(selection: $tab) {
                today.tag(CullyTab.today).tabItem { Label("Today", systemImage: CullyTab.today.icon) }
                tasks.tag(CullyTab.tasks).tabItem { Label("Tasks", systemImage: CullyTab.tasks.icon) }
                advisor.tag(CullyTab.advisor).tabItem { Label("Advisor", systemImage: CullyTab.advisor.icon) }
            }.tint(Theme.gold)
            HStack(spacing: 12) {
                TextField("Ask Cully…", text: $question).textFieldStyle(.roundedBorder).onSubmit { ask() }
                Button { Task { await voice.toggle() } } label: { Image(systemName: voice.recording ? "stop.fill" : "mic.fill") }
                    .frame(minWidth: 44, minHeight: 44).background(Theme.gold, in: Circle()).foregroundStyle(Theme.background).accessibilityLabel(voice.recording ? "Stop listening" : "Speak to Cully").disabled(model.chatBusy || model.reflectionBusy)
                Button { ask() } label: { Image(systemName: "arrow.up.circle.fill").font(.title2) }
                    .accessibilityLabel("Send question").disabled(question.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.chatBusy || model.reflectionBusy)
            }.padding(12).background(Theme.card, in: RoundedRectangle(cornerRadius: 26)).overlay(RoundedRectangle(cornerRadius: 26).stroke(Theme.gold.opacity(0.22), lineWidth: 1)).shadow(color: .black.opacity(0.2), radius: 12, y: 5).padding(.horizontal, 16).padding(.top, 10).tint(Theme.gold)
            Label(model.chatBusy || model.reflectionBusy ? "Thinking on your device…" : voice.recording ? voice.status : "Tap the mic or type · on-device voice", systemImage: model.chatBusy || model.reflectionBusy ? "sparkles" : voice.recording ? "waveform" : "mic").font(.caption).foregroundStyle(Theme.gold).padding(.top, 5)
        }.foregroundStyle(Theme.ink).background(Theme.background)
        .frame(minWidth: 330, minHeight: 600).preferredColorScheme(.dark)
        .sheet(isPresented: $showConnect) { connectionSheet }
        .sheet(isPresented: $showTask) { taskSheet }
        .sheet(item: $selectedNote) { note in recallSheet(note) }
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
            heading("Today starts here.", subtitle: "Pick up the thread. Make room for what comes next.")
            card("YOUR DAY, IN CONTEXT") {
                Text(model.briefStatus).font(.subheadline).foregroundStyle(.secondary)
                Picker("Review period", selection: $weekly) { Text("Last 24 hours").tag(false); Text("Past 7 days").tag(true) }.pickerStyle(.segmented)
                if let updated = model.briefUpdated { Text("Refreshed \(updated.formatted(date: .omitted, time: .shortened))").font(.caption) }
                if !model.reflection.isEmpty { Text(model.reflection).textSelection(.enabled) }
                if !model.reflectionStatus.isEmpty { Text(model.reflectionStatus).font(.caption).foregroundStyle(.secondary) }
                if !model.notes.isEmpty {
                    Button { model.reflectOnBrief() } label: { Label(model.reflectionBusy ? "Reflecting…" : "Find my next step", systemImage: "sparkles") }.buttonStyle(.bordered).disabled(model.reflectionBusy || model.chatBusy)
                }
                Button { model.refreshBrief() } label: { Label(model.busy ? "Gathering notes…" : "Refresh my day", systemImage: "arrow.clockwise") }.buttonStyle(.bordered).disabled(model.busy)
            }
            if model.briefUpdated != nil { continuationCard }
            card("YOUR FOCUS") {
                Text(model.focus?.title ?? "One thing at a time.").font(.title2.weight(.medium))
                Text(model.focus == nil ? "Choose a task to keep close today." : "A personal task saved on this device.").font(.subheadline).foregroundStyle(.secondary)
                Button(model.focus == nil ? "Choose a focus" : "View your tasks") { tab = .tasks }.buttonStyle(.bordered)
            }
            ForEach(learningNotes) { note in
                card("LEARN NEXT · \(localDate(note))") {
                    Text(note.learning ?? "").font(.headline)
                    Text("A recorded learning, not a measure of what you know. From sampled seven-day notes.").font(.caption).foregroundStyle(.secondary)
                    Text("Optional practice · Explain it without notes, then compare with what you saved.").font(.subheadline)
                    Button("Draft a practice question") { draftQuestion(note, prompt: "Use saved Cully evidence to help me recall this recorded learning: \(note.learning ?? ""). Ask one question and suggest a small exercise; do not assume what I know.") }.buttonStyle(.bordered)
                }
            }
            if model.briefUpdated != nil {
            card("PROJECTS IN THESE NOTES") {
                if projects.isEmpty { Text("No project was attached to these sampled notes.").foregroundStyle(.secondary) }
                ForEach(projects, id: \.self) { project in Text(project.replacingOccurrences(of: "https://github.com/", with: "")).font(.subheadline) }
            }
            }
            ForEach(visibleNotes) { note in
                card(model.dayNotes.contains(where: { $0.id == note.id }) ? "LAST 24 HOURS" : "EARLIER THIS WEEK") {
                    Text(note.summary).font(.headline)
                    Text("\(localDate(note)) · \(note.assistant)").font(.caption).foregroundStyle(.secondary)
                    Button("Recall how / why") { selectedNote = note }.buttonStyle(.bordered)
                }
            }
            if model.briefUpdated != nil {
            card("WHAT THIS COVERS") {
                Text(visibleNotes.isEmpty ? "No saved notes in this window yet. This does not mean you did no work." : "\(visibleNotes.count) sampled notes\(model.hasMoreDay || (weekly && model.hasMoreWeek) ? " · more notes available" : ""). This is not a complete activity report.").font(.subheadline)
                Text("Rolling \(weekly ? "7 days" : "24 hours") ending at refresh. Times use your device’s timezone.").font(.caption).foregroundStyle(.secondary)
            }
            }
            LocalSetupCard(setup: model.setup)
            Text("Personal tasks stay on this device. Your brief loads private saved notes when you connect. Nothing changes automatically.").font(.caption).foregroundStyle(.secondary)
        }
    }

    private func localDate(_ note: ContextNote) -> String {
        guard let stamp = note.occurredAt, let date = ISO8601DateFormatter().date(from: stamp) else { return note.date }
        return date.formatted(date: .abbreviated, time: .shortened)
    }
    private var continuationNotes: [ContextNote] { Array(model.notes.filter { !($0.nextSteps ?? "").isEmpty }.prefix(3)) }
    private var learningNotes: [ContextNote] { Array(model.notes.filter { !($0.learning ?? "").isEmpty }.prefix(2)) }
    private var continuationCard: some View {
        card("CONTINUE TODAY") {
            Text("Pick up a thread, on your terms.").font(.title2)
            Text("Last-planned next steps from sampled seven-day notes. They may have changed; choose what still matters.").font(.caption).foregroundStyle(.secondary)
            if continuationNotes.isEmpty { Text("No next step was recorded in these notes. Your local focus can still guide today.").foregroundStyle(.secondary) }
            ForEach(continuationNotes) { note in
                VStack(alignment: .leading, spacing: 10) {
                    Text("\((note.projectURL ?? "Personal work").replacingOccurrences(of: "https://github.com/", with: "")) · \(localDate(note))").font(.caption).foregroundStyle(Theme.gold)
                    Text(note.nextSteps ?? "").font(.headline)
                    if let issue = note.issue, !issue.isEmpty { Text("Reported blocker · \(issue)").font(.subheadline).foregroundStyle(.secondary) }
                    HStack { Button("Make a task") { draft = note.nextSteps ?? ""; showTask = true }; Button("Recall why") { selectedNote = note } }.buttonStyle(.bordered)
                }.padding(.vertical, 8)
            }
        }
    }
    private func draftQuestion(_ note: ContextNote, prompt: String) {
        question = prompt + (note.projectURL.map { " Project: \($0)" } ?? "")
        selectedNote = nil
    }
    private func recallSheet(_ note: ContextNote) -> some View {
        VStack(alignment: .leading, spacing: 18) {
            HStack { Text(note.entryType == "decision" ? "A saved decision" : "Recall how / why").font(.title2.bold()); Spacer(); Button("Done") { selectedNote = nil } }
            ScrollView {
                VStack(alignment: .leading, spacing: 18) {
                    Text(localDate(note)).font(.caption).foregroundStyle(Theme.gold)
                    if let project = note.projectURL { Text(project.replacingOccurrences(of: "https://github.com/", with: "")).font(.caption) }
                    Text(note.summary).font(.headline)
                    if let value = note.approach, !value.isEmpty { card("HOW WE APPROACHED IT") { Text(value) } }
                    if let value = note.outcome, !value.isEmpty { card("SAVED OUTCOME") { Text(value) } }
                    if let value = note.issue, !value.isEmpty { card("REPORTED BLOCKER") { Text(value) } }
                    if let value = note.learning, !value.isEmpty { card("RECORDED LEARNING") { Text(value) } }
                    if let value = note.nextSteps, !value.isEmpty { card("LAST PLANNED") { Text(value) } }
                    Text("Saved preview, not live status. Missing details have not been inferred.").font(.caption).foregroundStyle(.secondary)
                }
            }
            Button("Draft a follow-up") { draftQuestion(note, prompt: "Find relevant saved Cully evidence before answering. Recall how we worked on: \(note.summary). Explain the recorded approach and what is uncertain; do not invent a reason.") }.buttonStyle(.borderedProminent)
        }.padding(22).foregroundStyle(Theme.ink).background(Theme.background).tint(Theme.gold).frame(minWidth: 330, minHeight: 480)
    }
    private var visibleNotes: [ContextNote] { weekly ? model.notes : model.dayNotes }
    private var projects: [String] { Array(Set(visibleNotes.compactMap(\.projectURL))).sorted() }

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
            if !model.reflection.isEmpty { card("YOUR LOCAL ADVISOR") { Text(model.reflection); Text("AI suggestions from sampled saved notes. Review before acting.").font(.caption).foregroundStyle(.secondary) } }
            if !model.notes.isEmpty { Button("Reflect on my week") { model.reflectOnBrief() }.buttonStyle(.bordered).disabled(model.reflectionBusy || model.chatBusy) }
            ForEach(model.notes.filter { !($0.issue ?? "").isEmpty || !($0.nextSteps ?? "").isEmpty }.prefix(3)) { note in
                card("FROM A SAVED HANDOFF · \(localDate(note))") {
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
        guard !text.isEmpty, !model.chatBusy, !model.reflectionBusy else { return }
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
                Button("Send") { ask() }.disabled(question.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || model.chatBusy || model.reflectionBusy)
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
        VStack(alignment: .leading, spacing: 10) { Text(title).font(.largeTitle.weight(.semibold)); Text(subtitle).foregroundStyle(.secondary).font(.subheadline) }.padding(.bottom, 4)
    }
    private func card<Content: View>(_ title: String, @ViewBuilder _ content: () -> Content) -> some View {
        VStack(alignment: .leading, spacing: 12) { Text(title).font(.caption2.weight(.semibold)).tracking(1.4).foregroundStyle(Theme.gold); content() }
            .padding(20).frame(maxWidth: .infinity, alignment: .leading).background(Theme.card, in: RoundedRectangle(cornerRadius: 22)).overlay(RoundedRectangle(cornerRadius: 22).stroke(.white.opacity(0.07), lineWidth: 1))
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
        }.padding(20).frame(maxWidth: .infinity, alignment: .leading).background(Theme.card, in: RoundedRectangle(cornerRadius: 22)).overlay(RoundedRectangle(cornerRadius: 22).stroke(.white.opacity(0.07), lineWidth: 1))
        .alert("Set up local AI?", isPresented: $confirm) {
            Button("Later", role: .cancel) {}; Button("Download") { setup.start() }
        } message: { Text("Download 469 MiB of model weights. Allow 650 MiB of free space and use Wi-Fi if possible. Nothing downloads until you press Download.") }
    }
}

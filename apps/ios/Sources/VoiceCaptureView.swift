import SwiftUI
#if os(iOS)
@preconcurrency import Speech
@preconcurrency import AVFoundation
#endif

@MainActor final class LocalVoiceCapture: ObservableObject {
    @Published var transcript = ""
    @Published var status = "Your microphone starts only when you tap the microphone."
    @Published var recording = false
    #if os(iOS)
    private let engine = AVAudioEngine()
    private var request: SFSpeechAudioBufferRecognitionRequest?
    private var task: SFSpeechRecognitionTask?
    private var stopTask: Task<Void, Never>?
    private var tapped = false
    func toggle() async {
        if recording { stop(); return }
        let speech = await withCheckedContinuation { continuation in
            SFSpeechRecognizer.requestAuthorization { continuation.resume(returning: $0) }
        }
        guard speech == .authorized else { status = "Allow speech recognition in Settings to use voice capture."; return }
        let microphone = await withCheckedContinuation { continuation in
            AVAudioSession.sharedInstance().requestRecordPermission { continuation.resume(returning: $0) }
        }
        guard microphone else { status = "Allow microphone access to use voice capture."; return }
        guard let recognizer = SFSpeechRecognizer(locale: Locale.current), recognizer.supportsOnDeviceRecognition else {
            status = "On-device speech is unavailable for this language. You can type your task instead."; return
        }
        do {
            try AVAudioSession.sharedInstance().setCategory(.record, mode: .measurement, options: .duckOthers)
            try AVAudioSession.sharedInstance().setActive(true)
            let request = SFSpeechAudioBufferRecognitionRequest()
            request.requiresOnDeviceRecognition = true; request.shouldReportPartialResults = true
            self.request = request
            engine.inputNode.installTap(onBus: 0, bufferSize: 1024, format: engine.inputNode.outputFormat(forBus: 0)) { buffer, _ in request.append(buffer) }
            tapped = true
            task = recognizer.recognitionTask(with: request) { [weak self] result, error in
                let text = result?.bestTranscription.formattedString
                let finished = result?.isFinal == true || error != nil
                Task { @MainActor in
                    if let text { self?.transcript = text }
                    if finished { self?.stop() }
                }
            }
            engine.prepare(); try engine.start()
            recording = true; status = "Listening on your device · 15 seconds maximum"
            stopTask = Task { [weak self] in try? await Task.sleep(for: .seconds(15)); if !Task.isCancelled { self?.stop() } }
        } catch { stop(); status = "Could not start voice capture. You can type your task instead." }
    }
    func stop() {
        engine.stop()
        if tapped { engine.inputNode.removeTap(onBus: 0); tapped = false }
        request?.endAudio(); task?.cancel(); task = nil; request = nil
        stopTask?.cancel(); stopTask = nil
        if recording { status = "Review your words, then save when ready." }
        recording = false
        try? AVAudioSession.sharedInstance().setActive(false, options: .notifyOthersOnDeactivation)
    }
    #else
    func toggle() async { status = "Voice capture is prepared for iPhone. Type a task here while testing the layout on Mac." }
    func stop() { recording = false }
    #endif
}

package dev.local.aiphone;

import android.app.Activity;
import android.media.AudioFormat;
import android.media.AudioRecord;
import android.media.MediaRecorder;
import android.speech.tts.TextToSpeech;
import android.speech.tts.Voice;
import org.json.JSONObject;
import java.io.*;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;
import java.util.Locale;
import java.util.concurrent.Executors;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.TimeUnit;

/** Offline microphone -> Whisper CLI -> text; speech output only with a declared offline voice. */
final class VoiceIO {
    interface Listener {
        void state(String text, boolean busy);
        void transcript(String text);
        void error(String text);
    }
    private final Activity activity;
    private final Listener listener;
    private final Runnable beforeRecognition;
    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    private TextToSpeech speech;
    private volatile boolean recording, active = true, offlineVoice;
    private volatile Process recognizer;

    VoiceIO(Activity activity, Listener listener, Runnable beforeRecognition) {
        this.activity = activity; this.listener = listener; this.beforeRecognition = beforeRecognition;
        speech = new TextToSpeech(activity, code -> activity.runOnUiThread(() -> configureVoice(code)));
    }
    private void configureVoice(int result) {
        String selected = "none";
        if (result == TextToSpeech.SUCCESS && speech != null && speech.getVoices() != null) {
            for (Voice voice : speech.getVoices()) {
                if (voice.getLocale().getLanguage().equals(Locale.ENGLISH.getLanguage())
                        && !voice.isNetworkConnectionRequired()
                        && (voice.getFeatures() == null || !voice.getFeatures().contains(TextToSpeech.Engine.KEY_FEATURE_NOT_INSTALLED))) {
                    if (speech.setVoice(voice) == TextToSpeech.SUCCESS) {
                        offlineVoice = true; selected = voice.getName(); break;
                    }
                }
            }
        }
        try {
            JSONObject report = new JSONObject().put("offline_voice_available", offlineVoice)
                .put("selected_voice", selected).put("network_voice_allowed", false);
            try (FileOutputStream out = activity.openFileOutput("voice-status.json", 0)) {
                out.write(report.toString().getBytes(StandardCharsets.UTF_8));
            }
        } catch (Exception ignored) {}
        if (!offlineVoice) listener.state("Offline voice data is not installed; text chat remains available", false);
    }
    boolean isRecording() { return recording; }
    void speak(String text) {
        if (active && offlineVoice && speech != null) speech.speak(text, TextToSpeech.QUEUE_FLUSH, null, "local-answer");
    }
    void setActive(boolean value) {
        active = value;
        if (!value) {
            recording = false;
            if (recognizer != null) recognizer.destroy();
            if (speech != null) speech.stop();
        }
    }
    void toggle() {
        if (recording) { recording = false; return; }
        File model = new File(activity.getExternalFilesDir(null), "ggml-tiny-q5_1.bin");
        File executable = new File(activity.getApplicationInfo().nativeLibraryDir, "libaiphone_whisper.so");
        if (!model.isFile() || !executable.isFile()) {
            listener.error("Offline speech model/runtime missing. Install the completed voice build first."); return;
        }
        if (speech != null) speech.stop();
        recording = true;
        listener.state("Listening locally · tap Stop (15 seconds maximum)", true);
        worker.execute(() -> recordAndTranscribe(model, executable));
    }
    private void recordAndTranscribe(File model, File executable) {
        File wav = new File(activity.getCacheDir(), "voice-input.wav");
        File prefix = new File(activity.getCacheDir(), "voice-transcript");
        File transcript = new File(prefix.getAbsolutePath() + ".txt");
        AudioRecord audio = null;
        try {
            int minimum = AudioRecord.getMinBufferSize(16000, AudioFormat.CHANNEL_IN_MONO, AudioFormat.ENCODING_PCM_16BIT);
            if (minimum <= 0) throw new IOException("Microphone format unavailable.");
            audio = new AudioRecord(MediaRecorder.AudioSource.MIC, 16000, AudioFormat.CHANNEL_IN_MONO,
                                    AudioFormat.ENCODING_PCM_16BIT, Math.max(minimum, 8192));
            if (audio.getState() != AudioRecord.STATE_INITIALIZED) throw new IOException("Microphone could not start.");
            int count = 0;
            try (FileOutputStream out = new FileOutputStream(wav)) {
                out.write(new byte[44]); byte[] buffer = new byte[4096]; audio.startRecording();
                while (recording && active && count < 16000 * 2 * 15) {
                    int n = audio.read(buffer, 0, Math.min(buffer.length, 16000 * 2 * 15 - count));
                    if (n < 0) throw new IOException("Microphone read failed: " + n);
                    out.write(buffer, 0, n); count += n;
                }
            }
            audio.stop(); audio.release(); audio = null; recording = false;
            if (!active) return;
            if (count < 3200) throw new IOException("Recording was too short. Hold a brief sentence, then tap Stop.");
            writeHeader(wav, count);
            activity.runOnUiThread(() -> listener.state("Transcribing on your phone…", true));
            beforeRecognition.run();
            ProcessBuilder p = new ProcessBuilder(executable.getAbsolutePath(), "-m", model.getAbsolutePath(),
                "-f", wav.getAbsolutePath(), "-t", "4", "-l", "auto", "-nt", "-otxt", "-of", prefix.getAbsolutePath());
            p.redirectErrorStream(true); p.redirectOutput(new File(activity.getFilesDir(), "speech-engine.log"));
            recognizer = p.start();
            if (!recognizer.waitFor(90, TimeUnit.SECONDS)) { recognizer.destroy(); throw new IOException("Speech recognition timed out."); }
            if (!active) return;
            if (recognizer.exitValue() != 0 || !transcript.isFile()) throw new IOException("Speech engine failed; see its app-private log.");
            String text;
            try (FileInputStream in = new FileInputStream(transcript); ByteArrayOutputStream out = new ByteArrayOutputStream()) {
                byte[] buffer = new byte[4096]; int n;
                while ((n = in.read(buffer)) != -1) { if (out.size() + n > 8192) throw new IOException("Transcript too long."); out.write(buffer, 0, n); }
                text = out.toString("UTF-8").trim();
            }
            if (text.isEmpty()) throw new IOException("No speech detected. Try a clearer, slightly longer sentence.");
            final String result = text;
            activity.runOnUiThread(() -> { listener.state("Speech recognized locally", false); listener.transcript(result); });
        } catch (Exception error) {
            if (active) activity.runOnUiThread(() -> { listener.state("Voice request stopped", false); listener.error(error.getMessage()); });
        } finally {
            recording = false;
            if (audio != null) { try { audio.stop(); } catch (Exception ignored) {} audio.release(); }
            if (recognizer != null) { recognizer.destroy(); recognizer = null; }
            wav.delete(); transcript.delete();
        }
    }
    private static void writeHeader(File file, int bytes) throws IOException {
        ByteBuffer b = ByteBuffer.allocate(44).order(ByteOrder.LITTLE_ENDIAN);
        b.put("RIFF".getBytes(StandardCharsets.US_ASCII)).putInt(bytes + 36);
        b.put("WAVEfmt ".getBytes(StandardCharsets.US_ASCII)).putInt(16).putShort((short)1).putShort((short)1);
        b.putInt(16000).putInt(32000).putShort((short)2).putShort((short)16);
        b.put("data".getBytes(StandardCharsets.US_ASCII)).putInt(bytes);
        try (RandomAccessFile f = new RandomAccessFile(file, "rw")) { f.write(b.array()); }
    }
    void close() {
        setActive(false); worker.shutdownNow();
        if (speech != null) { speech.shutdown(); speech = null; }
    }
}

package dev.local.aiphone;

import android.app.Activity;
import android.app.AlertDialog;
import android.os.StatFs;
import android.widget.Button;
import org.json.JSONArray;
import org.json.JSONObject;
import java.io.*;
import java.net.HttpURLConnection;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** Explicit, resumable downloads of pinned public weights; no inference service account. */
final class ModelSetup implements AutoCloseable {
    private final Activity activity;
    private final Button button;
    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    private volatile boolean cancelled, busy, ready;
    private volatile HttpURLConnection active;
    private final JSONArray models;

    ModelSetup(Activity activity, Button button) throws Exception {
        this.activity = activity; this.button = button;
        try (InputStream in = activity.getAssets().open("models.json"); ByteArrayOutputStream out = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[4096]; int n;
            while ((n = in.read(buffer)) != -1) out.write(buffer, 0, n);
            models = new JSONObject(out.toString("UTF-8")).getJSONArray("models");
        }
        button.setOnClickListener(v -> {
            if (busy) { cancel(); return; }
            if (ready) return;
            new AlertDialog.Builder(activity).setTitle("Set up Cully")
                .setMessage("Download 499 MiB of language and speech models? Use Wi-Fi if possible. Allow 650 MiB of free space. Models stay on this phone; inference has no cloud fee. The engines and Cully connection SDK are already included.")
                .setNegativeButton("Later", null).setPositiveButton("Download", (d,w) -> start()).show();
        });
        // Inspect existing files without downloading anything, including developer-installed weights.
        busy = true; label("Checking local models…");
        worker.execute(() -> {
            try { ready = allVerified(); } catch (Exception ignored) { ready = false; }
            finally { busy = false; label(ready ? "Local models ready" : "Set up Cully · 499 MiB"); }
        });
    }
    private void label(String value) { activity.runOnUiThread(() -> { button.setText(value); button.setEnabled(!ready); }); }
    private File file(JSONObject model, String suffix) { return new File(activity.getExternalFilesDir(null), model.optString("file") + suffix); }
    private boolean verified(JSONObject model, File file) throws Exception {
        if (!file.isFile() || file.length() != model.getLong("bytes")) return false;
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        try (InputStream in = new FileInputStream(file)) {
            byte[] block = new byte[65536]; int n;
            while ((n = in.read(block)) != -1) { if (cancelled) throw new InterruptedIOException(); digest.update(block, 0, n); }
        }
        StringBuilder hash = new StringBuilder(); for (byte b : digest.digest()) hash.append(String.format("%02x", b & 255));
        return hash.toString().equals(model.getString("sha256"));
    }
    private boolean allVerified() throws Exception {
        for (int i = 0; i < models.length(); i++) if (!verified(models.getJSONObject(i), file(models.getJSONObject(i), ""))) return false;
        return true;
    }
    private void start() {
        if (busy) return;
        cancelled = false; busy = true;
        worker.execute(() -> {
            try {
                long needed = 100L * 1024 * 1024;
                for (int i = 0; i < models.length(); i++) {
                    JSONObject m = models.getJSONObject(i);
                    if (!verified(m, file(m, ""))) needed += Math.max(0, m.getLong("bytes") - file(m, ".part").length());
                }
                if (new StatFs(activity.getExternalFilesDir(null).getPath()).getAvailableBytes() < needed) throw new IOException("Not enough free space. Free 650 MiB and try again.");
                for (int i = 0; i < models.length(); i++) {
                    JSONObject m = models.getJSONObject(i);
                    if (verified(m, file(m, ""))) continue;
                    download(m);
                }
                ready = allVerified(); label("Local models ready");
            } catch (Exception e) {
                label(cancelled ? "Resume setup" : "Retry setup");
                if (!cancelled) activity.runOnUiThread(() -> new AlertDialog.Builder(activity).setTitle("Setup paused")
                    .setMessage("The download could not finish or verify. Check your internet and free space, then retry. Completed bytes are kept for resume.")
                    .setPositiveButton("OK", null).show());
            } finally { busy = false; active = null; }
        });
    }
    private void download(JSONObject m) throws Exception {
        File part = file(m, ".part"); long expected = m.getLong("bytes"), offset = part.length();
        if (offset > expected) { if (!part.delete()) throw new IOException("Invalid partial file"); offset = 0; }
        if (offset < expected) {
            HttpURLConnection c = (HttpURLConnection)new URL(m.getString("url")).openConnection(); active = c;
            c.setConnectTimeout(15000); c.setReadTimeout(20000); c.setRequestProperty("Accept-Encoding", "identity");
            if (offset > 0) c.setRequestProperty("Range", "bytes=" + offset + "-");
            try {
                int code = c.getResponseCode();
                if (!"https".equals(c.getURL().getProtocol())) throw new IOException("HTTPS required");
                if (code == 200) offset = 0;
                else if (code != 206 || !String.valueOf(c.getHeaderField("Content-Range")).startsWith("bytes " + offset + "-")) throw new IOException("Unexpected download response");
                try (InputStream in = c.getInputStream(); FileOutputStream out = new FileOutputStream(part, offset > 0)) {
                    byte[] block = new byte[65536]; int n; long total = offset, last = 0;
                    while ((n = in.read(block)) != -1) {
                        if (cancelled) throw new InterruptedIOException();
                        total += n; if (total > expected) throw new IOException("Download exceeded expected size");
                        out.write(block, 0, n);
                        long now = android.os.SystemClock.elapsedRealtime();
                        if (now - last > 500) { label(m.getString("name") + " · " + (total * 100 / expected) + "% · tap to pause"); last = now; }
                    }
                }
            } finally { c.disconnect(); active = null; }
        }
        if (cancelled) throw new InterruptedIOException();
        label("Verifying " + m.getString("name") + "…");
        if (!verified(m, part)) { part.delete(); throw new IOException("Checksum mismatch"); }
        File destination = file(m, "");
        if (!part.renameTo(destination)) throw new IOException("Could not install model");
    }
    void cancel() { cancelled = true; HttpURLConnection c = active; if (c != null) c.disconnect(); }
    public void close() { cancel(); worker.shutdownNow(); }
}

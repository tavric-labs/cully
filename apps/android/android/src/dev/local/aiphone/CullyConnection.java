package dev.local.aiphone;

import android.app.Activity;
import android.app.AlertDialog;
import android.content.pm.ApplicationInfo;
import android.net.Uri;
import android.widget.Button;
import android.widget.EditText;
import android.widget.TextView;
import org.json.JSONArray;
import org.json.JSONObject;
import org.mcpruntime.mobile.McpCallback;
import org.mcpruntime.mobile.McpMobileJavaClient;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

/** Cully owns server configuration and sign-in. The reusable SDK contains no Cully policy. */
final class CullyConnection implements AutoCloseable {
    private static final String DEFAULT_ENDPOINT = "https://mcp.mcpruntime.org/cully/mcp";
    private final Activity activity;
    private final TextView status;
    private final Button button;
    private final CullySignIn signIn;
    private String endpoint, token;
    private long expiresAt;
    private volatile boolean active;
    private boolean debugReadTest;
    private volatile McpMobileJavaClient client;
    private volatile String modelTools = "[]";

    String modelTools() { return modelTools; }

    /** The model selects a discovered tool. This host enforces its read-only Self-mode policy. */
    String invokeModelTool(String name, String rawArguments) throws Exception {
        McpMobileJavaClient session = client;
        if (!active || session == null || session.getState() != org.mcpruntime.mobile.ConnectionState.CONNECTED) throw new IOException("Cully is not connected.");
        if (!name.equals("cully_context")) throw new IOException("This tool is not enabled in Self mode.");
        if (rawArguments.length() > 4096) throw new IOException("Tool arguments exceeded the local limit.");
        JSONObject arguments = new JSONObject(rawArguments);
        java.util.Set<String> allowed = new java.util.HashSet<>(java.util.Arrays.asList("query", "project_url", "session_ref", "section", "mode", "limit"));
        java.util.Iterator<String> keys = arguments.keys();
        while(keys.hasNext()) if(!allowed.contains(keys.next())) throw new IOException("Unsupported tool argument.");
        if (!arguments.optString("section", "personal").equals("personal")) throw new IOException("Only private personal context is enabled.");
        String mode = arguments.optString("mode", arguments.optString("query").isEmpty()?"recent":"text");
        if (!mode.equals("recent") && !mode.equals("text")) throw new IOException("Only recent and text lookups are enabled.");
        if (arguments.optString("query").length() > 180) throw new IOException("Keep the context query short.");
        int limit = arguments.optInt("limit", 3);
        if(limit<1 || limit>3) throw new IOException("Request one to three notes.");
        arguments.put("section", "personal").put("limit", limit).put("mode", mode);
        CountDownLatch done = new CountDownLatch(1);
        AtomicReference<String> result = new AtomicReference<>(), failure = new AtomicReference<>();
        org.mcpruntime.mobile.McpRequest request = session.callTool(name, arguments.toString(), new McpCallback() {
            public void onSuccess(String value) { result.set(value); done.countDown(); }
            public void onError(String code, String message) { failure.set(message); done.countDown(); }
        });
        try {
            if(!done.await(15,TimeUnit.SECONDS)) throw new IOException("Cully did not respond in time. No action was repeated.");
            if(failure.get()!=null)throw new IOException(failure.get());
            JSONObject value = new JSONObject(result.get());
            if(value.optBoolean("isError",false))return "{\"error\":\"The context lookup failed; do not invent a result.\"}";
            JSONObject data=value.optJSONObject("structuredContent");
            if(data==null){JSONArray content=value.optJSONArray("content");if(content!=null)for(int i=0;i<content.length();i++){JSONObject part=content.getJSONObject(i);if(part.optString("type").equals("text")){data=new JSONObject(part.getString("text"));break;}}}
            if(data==null)throw new IOException("Cully returned no usable context.");
            JSONArray notes=data.optJSONArray("notes"), compact=new JSONArray();
            if(notes!=null)for(int i=0;i<Math.min(notes.length(),3);i++){
                JSONObject note=notes.getJSONObject(i), selected=new JSONObject();
                for(String key:new String[]{"date","summary","outcome","issue","next_steps"}){String text=note.optString(key);selected.put(key,text.substring(0,Math.min(text.length(),180)));}
                compact.put(selected);
            }
            return new JSONObject().put("source",data.optString("source")).put("notes",compact).toString();
        } finally { if(done.getCount()!=0)request.cancel(); }
    }

    CullyConnection(Activity activity, TextView status, Button button) {
        this.activity = activity; this.status = status; this.button = button;
        endpoint = activity.getPreferences(0).getString("cully_endpoint", DEFAULT_ENDPOINT);
        signIn = new CullySignIn(activity, new CullySignIn.Listener() {
            public void signedIn(String address, String credential, long expiry) {
                endpoint = address; token = credential; expiresAt = expiry;
                activity.getPreferences(0).edit().putString("cully_endpoint", endpoint).apply();
                if (active) connect();
            }
            public void failed() { status.setText("Could not open Cully sign-in · check internet and phone date, then try again"); button.setEnabled(true); }
        });
    }

    void configure() {
        if (token != null) { disconnect(); token = null; status.setText("Self mode · signed out of Cully"); button.setText("Connect Cully"); return; }
        EditText address = new EditText(activity); address.setSingleLine(true); address.setInputType(17); address.setText(endpoint);
        new AlertDialog.Builder(activity).setTitle("Connect your Cully").setMessage("Sign in to your Cully server. This preview checks the connection; chat and voice stay local.")
            .setView(address).setNegativeButton("Cancel", null).setPositiveButton("Sign in", (d, which) -> {
                String value = address.getText().toString().trim();
                try { org.mcpruntime.mobile.McpMobileClient.validateEndpoint(value, false); }
                catch (Exception invalid) { status.setText("Enter a valid HTTPS Cully MCP URL"); return; }
                button.setEnabled(false); status.setText("Opening Cully sign-in…"); signIn.begin(value);
            }).show();
    }

    void handleRedirect(Uri uri) { signIn.handleRedirect(uri); }

    void refreshContext(java.util.function.Consumer<String> success, java.util.function.Consumer<String> failure) {
        McpMobileJavaClient session = client;
        if (!active || session == null || session.getState() != org.mcpruntime.mobile.ConnectionState.CONNECTED) {
            failure.accept("Connect Cully to refresh your private context."); return;
        }
        session.callTool("cully_context", "{\"mode\":\"recent\",\"limit\":3,\"section\":\"personal\"}", new McpCallback() {
            public void onSuccess(String result) { activity.runOnUiThread(() -> { if(current(session))success.accept(result); }); }
            public void onError(String code, String message) { activity.runOnUiThread(() -> { if(current(session))failure.accept(message); }); }
        });
    }

    void onStart() {
        active = true;
        if (token == null) button.setEnabled(true);
        // Debug-only test credential handoff. Consume once from app-private storage; never an intent extra.
        File handoff = new File(activity.getFilesDir(), "cully-release-session.json");
        if ((activity.getApplicationInfo().flags & ApplicationInfo.FLAG_DEBUGGABLE) != 0 && handoff.isFile()) {
            try (FileInputStream input = new FileInputStream(handoff)) {
                if (handoff.length() > 16384) throw new IOException("Credential handoff limit");
                ByteArrayOutputStream bytes = new ByteArrayOutputStream(); byte[] b = new byte[1024]; int n;
                while ((n = input.read(b)) != -1) bytes.write(b, 0, n);
                JSONObject value = new JSONObject(bytes.toString("UTF-8"));
                endpoint = org.mcpruntime.mobile.McpMobileClient.validateEndpoint(value.getString("endpoint"), false);
                token = value.getString("access_token"); expiresAt = (long)(value.getDouble("expires_at") * 1000);
                debugReadTest = true;
            } catch (Exception ignored) { token = null; status.setText("Test sign-in expired or invalid"); }
            finally { handoff.delete(); }
        }
        if (token != null && client == null) connect();
    }

    private void connect() {
        if (!active) return;
        if (expiresAt <= System.currentTimeMillis() + 5000) { token = null; status.setText("Cully sign-in expired · connect again"); button.setEnabled(true); button.setText("Connect Cully"); return; }
        disconnect();
        final String credential = token;
        final McpMobileJavaClient session = new McpMobileJavaClient(endpoint, () -> credential);
        client = session; status.setText("Connecting your Cully…"); button.setEnabled(false);
        session.connect(new McpCallback() {
            public void onSuccess(String ignored) {
                if (!current(session)) return;
                session.listTools(null, new McpCallback() {
                    public void onSuccess(String result) {
                        try {
                            JSONArray tools = new JSONObject(result).getJSONArray("tools");
                            boolean hasContext = false; int selfTools = 0;
                            for (int i = 0; i < tools.length(); i++) {
                                String name = tools.getJSONObject(i).getString("name");
                                if (name.equals("cully_context")) hasContext = true;
                                if (name.startsWith("cully_") && !name.startsWith("cully_workspace")) selfTools++;
                            }
                            if (!hasContext) { fail(session, "This server does not expose Cully context"); return; }
                            JSONArray functions = new JSONArray();
                            for(int i=0;i<tools.length();i++){
                                JSONObject tool=tools.getJSONObject(i);
                                if(tool.optString("name").equals("cully_context")) {
                                    String description="Read saved private work notes. Use mode text for a topic or recent for latest work. Always set section personal and limit 1, 2 or 3.";
                                    JSONObject schema=new JSONObject(tool.getJSONObject("inputSchema").toString());
                                    JSONObject properties=schema.getJSONObject("properties");
                                    properties.put("limit",new JSONObject().put("type","integer").put("minimum",1).put("maximum",3).put("default",3));
                                    properties.put("section",new JSONObject().put("type","string").put("enum",new JSONArray().put("personal")));
                                    properties.put("mode",new JSONObject().put("type","string").put("enum",new JSONArray().put("text").put("recent")));
                                    functions.put(new JSONObject().put("type","function").put("function",new JSONObject()
                                        .put("name",tool.getString("name")).put("description",description.substring(0,Math.min(description.length(),360)))
                                        .put("parameters",schema)));
                                }
                            }
                            if(current(session))modelTools=functions.toString();
                            final int count = selfTools;
                            activity.runOnUiThread(() -> { if (current(session)) { status.setText("Self mode · Cully connected · " + count + " personal tools"); button.setText("Disconnect Cully"); button.setEnabled(true); } });
                            report(true, false, count);
                            if (debugReadTest) testRead(session, count);
                        } catch (Exception invalid) { fail(session, "Could not read the Cully tool catalog"); }
                    }
                    public void onError(String code, String message) { fail(session, message); }
                });
            }
            public void onError(String code, String message) { fail(session, message); }
        });
    }

    private void testRead(McpMobileJavaClient session, int count) {
        session.callTool("cully_context", "{\"query\":\"mcp-mobile-release-smoke-never-match-6f4b0c2d\",\"limit\":1,\"mode\":\"text\",\"project_url\":\"https://github.com/tavric-labs/mcp-mobile\"}", new McpCallback() {
            public void onSuccess(String json) { try { report(true, !new JSONObject(json).optBoolean("isError", false), count); } catch (Exception ignored) {} }
            public void onError(String code, String message) { report(false, false, count); }
        });
    }

    private boolean current(McpMobileJavaClient session) { return active && client == session; }
    private void fail(McpMobileJavaClient session, String message) {
        activity.runOnUiThread(() -> { if (current(session)) { report(false, false, 0); if (debugReadTest) debugFailure(session); disconnect(); token = null; status.setText(message); button.setEnabled(true); button.setText("Connect Cully"); } });
    }
    private void debugFailure(McpMobileJavaClient session) {
        try (FileOutputStream out = activity.openFileOutput("mcp-diagnostic.json", 0)) {
            out.write(new JSONObject().put("exception_types", new JSONArray(session.getLastFailureTypes())).toString().getBytes(StandardCharsets.UTF_8));
        } catch (Exception ignored) {}
    }
    private void report(boolean connected, boolean readPassed, int count) {
        try (FileOutputStream out = activity.openFileOutput("mcp-status.json", 0)) {
            out.write(new JSONObject().put("sdk", "mcp-mobile 0.1.0").put("connected", connected)
                .put("personal_tool_count", count).put("read_test_passed", readPassed).put("writes_performed", false)
                .toString().getBytes(StandardCharsets.UTF_8));
        } catch (Exception ignored) {}
    }
    private void disconnect() { modelTools="[]"; if (client != null) { client.close(); client = null; } }
    void onStop() { active = false; disconnect(); status.setText("Self mode · Cully connection paused"); }
    @Override public void close() { onStop(); token = null; signIn.close(); }
}

package dev.local.aiphone;

import android.app.Activity;
import android.content.Intent;
import android.net.Uri;
import android.util.Base64;
import org.json.JSONObject;
import java.io.*;
import java.net.HttpURLConnection;
import java.net.URI;
import java.net.URL;
import java.net.URLEncoder;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.SecureRandom;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

/** Public-client browser PKCE flow for this preview. Tokens/verifier remain in memory only. */
final class CullySignIn implements AutoCloseable {
    interface Listener { void signedIn(String endpoint, String token, long expiresAt); void failed(); }
    private static final String REDIRECT = "dev.cully.mobile:/oauth2redirect";
    private final Activity activity;
    private final Listener listener;
    private final ExecutorService worker = Executors.newSingleThreadExecutor();
    private String state, verifier, issuer, tokenEndpoint, clientId, resource;
    private volatile boolean closed;

    CullySignIn(Activity activity, Listener listener) { this.activity = activity; this.listener = listener; }

    void begin(String endpoint) {
        worker.execute(() -> {
            try {
                resource = org.mcpruntime.mobile.McpMobileClient.validateEndpoint(endpoint, false);
                URI address = URI.create(resource);
                String origin = address.getScheme() + "://" + address.getRawAuthority();
                JSONObject protectedResource = json(origin + "/.well-known/oauth-protected-resource" + address.getRawPath(), null, false);
                if (!resource.equals(protectedResource.getString("resource"))) throw new IOException("Resource mismatch");
                issuer = https(protectedResource.getJSONArray("authorization_servers").getString(0));
                URI authority = URI.create(issuer);
                JSONObject metadata = json(authority.getScheme() + "://" + authority.getRawAuthority()
                    + "/.well-known/oauth-authorization-server" + authority.getRawPath(), null, false);
                if (!issuer.equals(metadata.getString("issuer"))) throw new IOException("Issuer mismatch");
                if (!metadata.getJSONArray("code_challenge_methods_supported").toString().contains("\"S256\"")) throw new IOException("PKCE unavailable");
                tokenEndpoint = https(metadata.getString("token_endpoint"));
                JSONObject registration = json(https(metadata.getString("registration_endpoint")), new JSONObject()
                    .put("client_name", "Cully Android Self preview")
                    .put("redirect_uris", new org.json.JSONArray().put(REDIRECT))
                    .put("token_endpoint_auth_method", "none")
                    .put("grant_types", new org.json.JSONArray().put("authorization_code"))
                    .put("response_types", new org.json.JSONArray().put("code")), false);
                clientId = registration.getString("client_id"); state = random(); verifier = random();
                String challenge = Base64.encodeToString(MessageDigest.getInstance("SHA-256").digest(verifier.getBytes(StandardCharsets.US_ASCII)), Base64.URL_SAFE | Base64.NO_WRAP | Base64.NO_PADDING);
                Uri authorize = Uri.parse(https(metadata.getString("authorization_endpoint"))).buildUpon()
                    .appendQueryParameter("response_type", "code").appendQueryParameter("client_id", clientId)
                    .appendQueryParameter("redirect_uri", REDIRECT).appendQueryParameter("scope", "tools:read")
                    .appendQueryParameter("resource", resource).appendQueryParameter("state", state)
                    .appendQueryParameter("code_challenge", challenge).appendQueryParameter("code_challenge_method", "S256").build();
                activity.runOnUiThread(() -> {
                    if (!closed) {
                        try { activity.startActivity(new Intent(Intent.ACTION_VIEW, authorize).addCategory(Intent.CATEGORY_BROWSABLE)); }
                        catch (Exception unavailable) { fail(); }
                    }
                });
            } catch (Exception ignored) { fail(); }
        });
    }

    void handleRedirect(Uri uri) {
        if (uri == null || !"dev.cully.mobile".equals(uri.getScheme()) || !"/oauth2redirect".equals(uri.getPath())) return;
        final String code = uri.getQueryParameter("code");
        String incomingState = uri.getQueryParameter("state"), incomingIssuer = uri.getQueryParameter("iss");
        if (state == null || incomingState == null || !MessageDigest.isEqual(state.getBytes(StandardCharsets.UTF_8), incomingState.getBytes(StandardCharsets.UTF_8))
            || code == null || (incomingIssuer != null && !issuer.equals(incomingIssuer))) { fail(); return; }
        state = null; // Only accept one callback for this authorization attempt.
        worker.execute(() -> {
            try {
                JSONObject token = json(tokenEndpoint, new JSONObject().put("grant_type", "authorization_code")
                    .put("client_id", clientId).put("code", code).put("redirect_uri", REDIRECT)
                    .put("code_verifier", verifier).put("resource", resource), true);
                verifier = null;
                if (!"Bearer".equalsIgnoreCase(token.getString("token_type"))) throw new IOException("Unexpected token type");
                String accessToken = token.getString("access_token");
                long expiresAt = System.currentTimeMillis() + token.optLong("expires_in", 300) * 1000;
                activity.runOnUiThread(() -> { if (!closed) listener.signedIn(resource, accessToken, expiresAt); });
            } catch (Exception ignored) { fail(); }
        });
    }

    private void fail() { verifier = null; state = null; activity.runOnUiThread(() -> { if (!closed) listener.failed(); }); }
    private static String random() { byte[] b = new byte[48]; new SecureRandom().nextBytes(b); return Base64.encodeToString(b, Base64.URL_SAFE | Base64.NO_WRAP | Base64.NO_PADDING); }
    private static String https(String value) throws IOException {
        URI uri = URI.create(value);
        if (!"https".equals(uri.getScheme()) || uri.getHost() == null || uri.getUserInfo() != null || uri.getFragment() != null) throw new IOException("HTTPS required");
        return value;
    }
    private static JSONObject json(String address, JSONObject body, boolean form) throws Exception {
        HttpURLConnection connection = (HttpURLConnection) new URL(https(address)).openConnection();
        connection.setInstanceFollowRedirects(false); connection.setConnectTimeout(15000); connection.setReadTimeout(20000);
        try {
            if (body != null) {
                String content = body.toString();
                if (form) {
                    StringBuilder encoded = new StringBuilder();
                    java.util.Iterator<String> names = body.keys();
                    while (names.hasNext()) { String name = names.next(); if (encoded.length() > 0) encoded.append('&'); encoded.append(URLEncoder.encode(name, "UTF-8")).append('=').append(URLEncoder.encode(body.getString(name), "UTF-8")); }
                    content = encoded.toString();
                }
                connection.setRequestMethod("POST"); connection.setDoOutput(true);
                connection.setRequestProperty("Content-Type", form ? "application/x-www-form-urlencoded" : "application/json");
                try (OutputStream out = connection.getOutputStream()) { out.write(content.getBytes(StandardCharsets.UTF_8)); }
            }
            int status = connection.getResponseCode();
            if (status < 200 || status > 299) throw new IOException("Sign-in request failed");
            try (InputStream in = connection.getInputStream(); ByteArrayOutputStream out = new ByteArrayOutputStream()) {
                byte[] b = new byte[4096]; int n;
                while ((n = in.read(b)) != -1) { if (out.size()+n > 65536) throw new IOException("Response limit"); out.write(b, 0, n); }
                return new JSONObject(out.toString("UTF-8"));
            }
        } finally { connection.disconnect(); }
    }
    @Override public void close() { closed = true; verifier = null; state = null; worker.shutdownNow(); }
}

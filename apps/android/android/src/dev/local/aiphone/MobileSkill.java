package dev.local.aiphone;

import android.content.Context;
import org.json.JSONObject;
import java.io.*;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;

/** Uses the canonical Cully skill's mobile entrypoint. No untrusted import or script execution. */
final class MobileSkill {
    static String instructions(Context context) throws Exception {
        JSONObject catalog = new JSONObject(asset(context, "catalog.json"));
        String profile = catalog.getString("profile");
        if (!profile.equals("references/mobile-self.md")) throw new IOException("Unsupported mobile skill profile");
        String body = asset(context, profile);
        byte[] hash = MessageDigest.getInstance("SHA-256").digest(body.getBytes(StandardCharsets.UTF_8));
        StringBuilder hex = new StringBuilder(); for (byte b : hash) hex.append(String.format("%02x", b & 255));
        if (!hex.toString().equals(catalog.getJSONObject("sha256").getString(profile))) throw new IOException("Skill integrity check failed");
        return body;
    }
    private static String asset(Context context, String path) throws Exception {
        try (InputStream in = context.getAssets().open("skills/cully/" + path); ByteArrayOutputStream out = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[1024]; int n;
            while ((n=in.read(buffer))!=-1) { if(out.size()+n>16384)throw new IOException("Skill size limit");out.write(buffer,0,n); }
            return out.toString("UTF-8");
        }
    }
}

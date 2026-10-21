---
title: Add sign-in to Cully
description: Set up OAuth when several people use a Cully server over HTTPS.
---

# Add sign-in to Cully

OAuth is optional for a team deployment. You do not need it for `cully setup --agent codex --all` on your laptop. When an ops team deploys one Cully server for several people over HTTPS, OAuth lets each person sign in and access their own records. [MCP Auth](https://github.com/tavric-labs/mcp-auth) provides configurable login methods: LDAP, Google, GitHub and generic OIDC or OAuth 2.0 providers such as Keycloak, Okta and Entra ID.

MCP Auth checks identity through the configured login method and issues a token for Cully's exact public MCP URL. Cully verifies that token and uses its subject as the record owner. The coding agent uses OAuth for every method, including LDAP; directory passwords are entered in the browser and never put in the agent's MCP configuration. Read tools require `tools:read` and writes require `tools:write`. The [team deployment diagram](/team-deployment) shows the service flow.

## Choose login methods

Pick the methods your deployment needs; Cully has no required provider. One
MCP Auth server can offer several methods on the same login page.

| Available service | Configure in MCP Auth |
| --- | --- |
| An LDAP directory | A native `ldap` method. MCP Auth searches the directory and binds as the user over LDAPS or StartTLS. An upstream OIDC server is not required. |
| Google accounts | A `google` method with an OAuth client ID and secret; optionally restrict Google Workspace domains. |
| GitHub accounts | A `github` method with an OAuth client ID and secret; optionally restrict organization membership. |
| Keycloak, Okta, Entra ID or another OIDC/OAuth provider | An `oidc` method referencing that provider's connector. |

The authoritative instructions for writing these configurations are in
[MCP Auth's login-method guide](https://github.com/tavric-labs/mcp-auth/blob/main/docs/login-methods.md).
It documents each method's fields, complete examples, callback registration,
secret environment variables, directory searches and provider restrictions.
Use its [connector guide](https://github.com/tavric-labs/mcp-auth/blob/main/docs/auth-server.md#oidc-or-plain-oauth-20)
for generic OIDC/OAuth provider settings. Additional providers can use that
connector interface; a new native protocol still needs an MCP Auth implementation.

## Configure a deployment with login methods

Login methods require an MCP Auth release containing
[PR #32](https://github.com/tavric-labs/mcp-auth/pull/32). Select that released
auth-server image explicitly. The
[Compose example](https://github.com/tavric-labs/cully/tree/main/deploy/login-methods)
provides Cully's service wiring, independent storage, LDAP and Google examples,
and an environment file passed only to MCP Auth.

1. Write `login-methods.json` using the MCP Auth guide and set
   `MCP_AUTH_LOGIN_METHODS_FILE` to its mounted container path. For an `oidc`
   method, also provide `MCP_AUTH_CONNECTORS_FILE`. Leave `MCP_AUTH_CONNECTOR`
   unset in login-method mode.
2. Supply only the environment variables referenced by your selected methods.
   Google and GitHub need their app credentials; LDAP needs directory settings
   and, when using a search account, that account's secret. Anonymous directory
   searches must be enabled explicitly. Keep secret values out of JSON and Git.
3. Set MCP Auth's issuer and resource to the public authorization-server URL and
   exact public Cully MCP URL. Allow `tools:read` and `tools:write` for that resource.
4. Configure Cully to trust the same issuer and resource, plus the issuer's JWKS
   URL. For the example's dedicated authorization hostname:

   ```dotenv
   CULLY_MCP_AUTH_MODE=oauth
   CULLY_AUTH_ISSUER=https://auth.example.com
   CULLY_AUTH_RESOURCE=https://mcp.example.com/mcp
   CULLY_JWKS_URL=https://auth.example.com/.well-known/jwks.json
   ```

Users connect their agent once and choose an offered method in the browser.
Provider selection lives in MCP Auth's configuration; Cully's token validation
does not change per provider. Accounts from different methods are separate
identities by default. Review MCP Auth's subject-prefix guidance before changing
the method on a deployment that already has user records.

## Bundled Docker setup with one connector

The bundled `cully setup --all --oauth` path below uses one OIDC/OAuth connector.
For the login-method registry, use the configurable deployment above. Prepare
two public hostnames pointing to your machine: one for Cully MCP and one for MCP
Auth. Ports 80 and 443 must be reachable so Caddy can serve HTTPS. This connector
path needs a provider where you can register a client, the [Cully CLI](/installation)
and Docker Compose.

## Self-hosted Docker with MCP Auth

1. Create the editable files without starting containers:

   ```sh
   cully setup --all --prepare
   ```

2. Register a client with your identity provider. Set its redirect URI to your MCP Auth callback, such as `https://auth.example.com/mcp-auth/identity/callback`. Follow the [MCP Auth connector guide](https://github.com/tavric-labs/mcp-auth/blob/main/docs/auth-server.md#connect-an-organizations-identity-provider) for provider settings and identity claims.

3. Edit `~/.cully/self-hosted/config/.env` with your hostnames and the client secret:

   ```dotenv
   CULLY_MCP_HOST=mcp.example.com
   CULLY_AUTH_HOST=auth.example.com
   MCP_AUTH_UPSTREAM_CLIENT_SECRET=your-private-client-secret
   ```

   Use hostnames without `https://` or a path. Setup derives the public MCP URL, issuer and signing-key URL from them. Keep this file private.

4. Create `~/.cully/self-hosted/config/connectors.json` for your provider. For Keycloak, copy `connectors.keycloak.example.json` from the same directory and replace its example realm, client and claim values. Keep the client secret in `.env`, not in the JSON file. If you define more than one connector, set `CULLY_MCP_AUTH_CONNECTOR` in `.env`. See the [MCP Auth connector settings](https://github.com/tavric-labs/mcp-auth/blob/main/docs/auth-server.md#oidc-or-plain-oauth-20) for other providers.

5. Create the signing key:

   ```sh
   openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out ~/.cully/self-hosted/config/.secrets/signing-key.pem
   ```

   Make it readable by the MCP Auth container. Keep it private and back it up with the Compose `auth-state` volume.

6. Start the stack and connect your agent:

   <InstallCommand template="cully setup --agent {agent} --all --oauth" />

   Setup starts Cully, MCP Auth and Caddy, then registers the public MCP URL with your agent. Restart the agent. For Codex, run `codex mcp login cully`; in Claude Code, use `/mcp`; in Cursor, sign in from MCP settings.

Check the public routes after setup, replacing the example hostnames:

```sh
curl -fsS https://auth.example.com/mcp-auth/.well-known/jwks.json
curl -fsS https://mcp.example.com/.well-known/oauth-protected-resource/mcp
```

## Connect to a server that already uses OAuth

Run `cully setup --agent codex --mcp-url https://mcp.example.com/mcp --oauth`, restart Codex and run `codex mcp login cully`. See [connect an agent](/agents) for Claude Code and Cursor.

Cully checks the RS256 token signature, issuer, exact MCP resource URL, expiration, subject and tool scopes. The data API and Mem0 use separate private service credentials. For another container platform or authorization server, see [team deployment](/team-deployment) and the [configuration reference](/configuration#manual-mcp-service-settings).

Keep the signing key and MCP Auth state across upgrades so existing registrations and sessions remain usable. Setup checks the OAuth values, connector JSON and signing key before starting the stack. For a manual deployment, configure issuer, audience and JWKS URL explicitly on Cully MCP; OAuth does not apply to the private data API or Mem0 links.

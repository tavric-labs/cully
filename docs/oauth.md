---
title: Add sign-in to Cully
description: Set up OAuth when several people use a Cully server over HTTPS.
---

# Add sign-in to Cully

OAuth is optional for a team deployment. You do not need it for `cully setup --agent codex --all` on your laptop. When an ops team deploys one Cully server for several people over HTTPS, OAuth lets each person sign in and access their own records. [MCP Auth](https://github.com/tavric-labs/mcp-auth) provides configurable login methods: LDAP, Google, GitHub and generic OIDC or OAuth 2.0 providers such as Keycloak, Okta and Entra ID.

MCP Auth checks identity through the configured login method and issues a token for Cully's exact public MCP URL. Cully verifies that token and uses its subject as the record owner. The coding agent uses OAuth for every method, including LDAP; directory passwords are entered in the browser and never put in the agent's MCP configuration. Read tools require `tools:read` and writes require `tools:write`. The [team deployment diagram](/team-deployment) shows the service flow.

## Choose login methods

Pick the methods your deployment needs; Cully has no required provider. One
MCP Auth server can offer several methods on the same login page. For Google-only login, include only the `google` entry in the `methods` array. MCP Auth redirects directly to Google after consent. Add `ldap` only when directory login is also wanted.

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

## Plug in your mcp-auth configuration

Use `cully setup --all --oauth` with a directory containing your mcp-auth
configuration files and an environment file. Setup automatically resolves the
latest stable mcp-auth release and uses its published versioned image.
Cully mounts the directory
read-only at `/etc/mcp-auth` and passes the environment file only to mcp-auth.
The selected mcp-auth image reads and validates its own provider configuration;
Cully does not require a particular JSON filename or provider schema.

| Input | What you provide |
| --- | --- |
| `--mcp-auth-config-dir DIR` | Your mcp-auth files: login methods, OIDC connectors, directory CA bundles or other files the selected image needs. |
| `--mcp-auth-env FILE` | Container environment variables selecting those files, plus the provider secrets referenced by them. |

For example, create a directory containing `login-methods.json` and, when an
OIDC method needs it, `connectors.json`. Write the files using
[mcp-auth's login-method configuration](https://github.com/tavric-labs/mcp-auth/blob/main/docs/login-methods.md#turn-it-on)
and [OIDC/OAuth connector configuration](https://github.com/tavric-labs/mcp-auth/blob/main/docs/auth-server.md#oidc-or-plain-oauth-20).
Provider-specific fields, restrictions and examples are documented for
[LDAP](https://github.com/tavric-labs/mcp-auth/blob/main/docs/login-methods.md#ldap),
[Google](https://github.com/tavric-labs/mcp-auth/blob/main/docs/login-methods.md#google),
[GitHub](https://github.com/tavric-labs/mcp-auth/blob/main/docs/login-methods.md#github)
and [generic OIDC](https://github.com/tavric-labs/mcp-auth/blob/main/docs/login-methods.md#oidc).
You can offer several methods together in the same file.
The released mcp-auth version must support the methods and fields you supply;
the login-method registry requires a release containing
[PR #32](https://github.com/tavric-labs/mcp-auth/pull/32).

Your private `provider.env` selects the files by their **container paths**:

```dotenv
MCP_AUTH_LOGIN_METHODS_FILE=/etc/mcp-auth/login-methods.json
# Include this only when a method uses a named connector:
MCP_AUTH_CONNECTORS_FILE=/etc/mcp-auth/connectors.json
# Add the provider variables referenced by your config's *_env fields.
```

Use your actual file paths to prepare the local deployment:

```sh
cully setup --all --oauth --prepare \
  --mcp-auth-config-dir /absolute/path/to/auth-config \
  --mcp-auth-env /absolute/path/to/provider.env
```

Set `CULLY_MCP_HOST` and `CULLY_AUTH_HOST` in
`~/.cully/self-hosted/config/.env`, and create
`~/.cully/self-hosted/config/.secrets/signing-key.pem` as described below.
This mode needs no legacy `MCP_AUTH_UPSTREAM_CLIENT_SECRET` or Cully-managed
`connectors.json`. Keep the provider env file private and make the mounted
config files and signing key readable by the chosen auth image's user.

Start or rerun with `cully setup --all --oauth`. Setup remembers the image and
absolute input paths in `~/.cully/self-hosted/config/mcp-auth.json`; it leaves
your provider files in place. Repeat the two config/env options to change
those input paths. Each production setup resolves the latest stable release
again; a failed release lookup stops setup rather than selecting an unverified
image. `--mcp-auth-image` is an invocation-only override for testing an
unreleased image; it is not required for production and is not reused
implicitly on the next run. The config directory can hold any filenames; choose
them through the selected image's environment variables. For an older image
using a single connector, set `MCP_AUTH_CONNECTOR` and
`MCP_AUTH_CONNECTORS_FILE` in `provider.env` instead of
`MCP_AUTH_LOGIN_METHODS_FILE`.

Cully supplies mcp-auth's issuer, resource/scopes, signing-key path, local auth
state and HTTPS/proxy settings so the tokens match Cully's trust configuration.
All provider-specific settings come from your files. Auth state ownership
defaults to UID/GID `65532:65532`; if your image uses another user, set
`CULLY_MCP_AUTH_STATE_OWNER` in the deployment `.env` accordingly. When changing
an existing bundled deployment, stop its `mcp-auth` service first; setup refuses
to start both auth services on the same route. Preserve its signing key and
state, and review the subject-prefix guidance below before changing methods.

## Configure a deployment with login methods

Login methods require an MCP Auth release containing
[PR #32](https://github.com/tavric-labs/mcp-auth/pull/32). Select that released
auth-server image explicitly. The
[Compose example](https://github.com/tavric-labs/cully/tree/main/deploy/login-methods)
provides Cully's service wiring, independent storage, LDAP and Google examples,
and an environment file passed only to MCP Auth.

For deployments managed with your own Compose tooling, the auth service wiring
looks like this excerpt (use the full Compose example for URLs, signing keys
and storage):

```yaml
services:
  mcp-auth:
    image: ${MCP_AUTH_IMAGE:?select a compatible image and version}
    volumes:
      - ./config:/etc/mcp-auth:ro
    env_file:
      - ./provider.env
    environment:
      MCP_AUTH_LOGIN_METHODS_FILE: /etc/mcp-auth/login-methods.json
      # Include when your methods reference an OIDC/OAuth connector:
      MCP_AUTH_CONNECTORS_FILE: /etc/mcp-auth/connectors.json
```

Set `MCP_AUTH_IMAGE` in the Compose `.env`, put your login methods in
`config/login-methods.json`, and add `config/connectors.json` when needed.
Put the secrets referenced by those files in `provider.env`. The mounted
filenames must match the container paths in the environment variables.
See [mcp-auth's complete multi-provider example](https://github.com/tavric-labs/mcp-auth/blob/main/examples/login-methods.example.json)
and [login-method reference](https://github.com/tavric-labs/mcp-auth/blob/main/docs/login-methods.md)
for the JSON format.

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

Without custom mcp-auth options, the bundled `cully setup --all --oauth` path
below uses its pinned auth image and one OIDC/OAuth connector. To supply your
own provider files with the latest released mcp-auth image, use [the setup options above](#plug-in-your-mcp-auth-configuration). Prepare
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

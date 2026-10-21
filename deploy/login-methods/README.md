# Cully with configurable login methods

Choose LDAP, Google, GitHub, generic OIDC/OAuth or several methods together
through MCP Auth configuration. Cully's OAuth token validation is the same for
every method. The authoritative provider fields and examples live in
[MCP Auth's login-method guide](https://github.com/tavric-labs/mcp-auth/blob/main/docs/login-methods.md)
and [OIDC connector guide](https://github.com/tavric-labs/mcp-auth/blob/main/docs/auth-server.md#oidc-or-plain-oauth-20).

This example starts a separate deployment of Cully and MCP Auth, with its own
Compose project, databases, signing key and authorization state. An existing
deployment keeps its configuration and data.

**Release prerequisite:** merge [mcp-auth PR #32](https://github.com/tavric-labs/mcp-auth/pull/32),
publish its auth-server image, then set `MCP_AUTH_IMAGE` to that exact released
image. At preparation time, the latest mcp-auth release is v0.5.1 and the PR is
still open. Do not assume v0.5.1 or Cully's bundled 0.4.4 image supports login
methods. This example deliberately has no default auth image. Updating or
deploying the existing Keycloak service is not part of these instructions.

The agent still uses OAuth with Cully. With LDAP, the browser shows a login form
hosted by MCP Auth; MCP Auth checks the password by binding to the directory and
issues a Cully access token. LDAP credentials never go into the agent's MCP
configuration. No Keycloak or other upstream OIDC server is needed for LDAP.

## Prepare the separate host

Use a checkout or release archive containing this directory and the adjacent
`deploy/self-hosted/mem0-init-db.sql`. Run the following on the deployment host,
not a shared development VM:

```sh
cd deploy/login-methods
cp .env.example .env
cp provider.env.example provider.env
# LDAP is one example; choose the Google example or write your selected methods.
cp config/login-methods.ldap.example.json config/login-methods.json
mkdir -p .secrets
chmod 700 .secrets
chmod 600 .env provider.env
openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 \
  -out .secrets/signing-key.pem
chmod 600 .secrets/signing-key.pem
sudo chown 65532:65532 .secrets/signing-key.pem
```

Edit `.env` with independent service secrets, the matching published Cully
image tag, the released mcp-auth image and the new public HTTPS URLs. The issuer
is an HTTPS origin without a trailing slash, for example
`https://cully-auth.example.com`. The resource is the exact public MCP endpoint,
for example `https://cully.example.com/mcp`. The database password in
`CULLY_DATABASE_URL` must match `CULLY_DB_PASSWORD` and be URL-encoded.

Edit `config/login-methods.json` for the methods you selected. For LDAP, the example uses
a read-only search account; put its password in `provider.env`, under the name
referenced by `bind_password_env`. End users enter their own passwords only in
the browser. The filter must locate exactly one eligible account. Use a stable
directory identifier such as `entryUUID` or Active Directory's `objectGUID`
for `subject_attribute`.

For a private directory CA, put its public certificate bundle in `config/` and
set `ca_file` to `/etc/mcp-auth/<bundle-name>.pem`. The config directory is
mounted read-only. Use LDAPS or `ldap://` with `start_tls: true`; certificate
verification stays enabled. This example needs directory read-account access;
it does not assume anonymous searches are enabled.

Keep only provider client IDs/secrets and the directory bind-account password
in `provider.env`. That file goes only to MCP Auth. Keep database and Mem0
credentials in `.env`. Never copy either file into an agent configuration.

## Publish HTTPS routes

Use the new host's existing reverse proxy and two new hostnames:

| Public hostname | Loopback upstream |
| --- | --- |
| Cully MCP (`cully.example.com`) | `127.0.0.1:3393` |
| MCP Auth (`cully-auth.example.com`) | `127.0.0.1:8081` |

Proxy all paths on each hostname without stripping prefixes. Preserve the
client `Authorization` header on MCP requests and set `X-Forwarded-Proto` to
`https` at the trusted TLS proxy. MCP discovery lives under
`/.well-known/oauth-protected-resource`; the auth hostname also serves discovery,
authorization, registration, token, callback and login routes. Login requires
browser cookies. Keep the loopback upstreams private because MCP Auth trusts
the TLS proxy. Choose different loopback ports in `.env` if occupied.

The private data API, PostgreSQL and Mem0 have no published ports. This
deployment sends the client token directly to Cully for owner identification.

## Start after the mcp-auth release

```sh
docker compose --env-file .env config --quiet
docker compose --env-file .env pull
docker compose --env-file .env up -d --wait db mem0-db
docker compose --env-file .env --profile ops run --rm migrate
docker compose --env-file .env up -d --wait mem0 data-api mcp mcp-auth
```

Back up the volumes and signing key. Apply schema migrations before serving
traffic after upgrades. Never use `down --volumes` on a deployment with records
you need to keep.

Connect each coding agent to the new HTTPS MCP endpoint using its normal OAuth
sign-in. See [Connect an agent](../../docs/agents.md). Check a correct and wrong
LDAP password, empty-password rejection, directory unavailability, read/write
scope enforcement and isolation between two users before opening access.
PR #32's LDAP tests use a fake directory; real-directory validation remains a
deployment acceptance step. Test only with approved accounts on the target
directory.

## Google, GitHub and generic OIDC

Use `config/login-methods.google.example.json` instead of the LDAP example for
Google. Register `<CULLY_AUTH_ISSUER>/identity/callback` with Google and put the
client ID and secret in `provider.env`. Set `allowed_domains` to your Google
Workspace domain; omit it deliberately only if any Google account may sign in.

The same `methods` array can offer Google, GitHub, LDAP and generic OIDC
together. For an `oidc` entry, add `config/connectors.json` and reference the
connector by name. Its `*_env` variables also belong in `provider.env`.
Use the MCP Auth guides linked above for method fields and restrictions.

Different methods produce separate identities by default (`directory:…`,
`google:…`). Accounts are not linked automatically. This new deployment starts
with its own data; changing an existing deployment's prefixes would need an
explicit identity migration so users retain access to their records.

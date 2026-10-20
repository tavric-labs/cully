# Development

Use Go 1.26 or newer. All Cully production entry points build from the root module; upstream Mem0 is deployed separately.

```sh
go fmt ./...
go vet ./...
go build ./...
go test ./... -race -count=1
```

Unit and transport tests run without a database. PostgreSQL tests require `CULLY_TEST_DATABASE_URL` pointing to a disposable plain PostgreSQL database. They create and remove generated schemas; use a separate test database, never production.

```sh
CULLY_TEST_DATABASE_URL=postgresql://test:test@localhost:5432/cully_test \
  go test ./internal/store/postgres -race -count=1 -v
```

Database coverage includes every memory operation, owner isolation, full-text search, personal records, fresh schema creation, Mem0 retries/deletion and authenticated MCP-to-data-API-to-PostgreSQL E2E.

Separate workflows provide independent checks and badge targets:

| Workflow | Check |
| --- | --- |
| Lint | Go formatting and vet, shell syntax, Compose configuration |
| Unit Test | Go and deployment-controller unit tests |
| Integration Test | PostgreSQL-backed owner isolation and OAuth MCP tests |
| E2E Test — Solo | Install/setup/uninstall, live private memory and Mem0 recall, session task linking; local connections cannot create a Team workspace |
| E2E Test — Team | Production CLI/MCP/data binaries, signed OAuth via JWKS, disposable PostgreSQL, cross-agent handoff, review gates, lesson sharing/withdrawal, concurrent claims and revocation |
| Trivy | MCP, data, website and docs container vulnerability reports |
| CodeQL | Go, Python and JavaScript/TypeScript analysis |

These run on pushes and pull requests. The website and docs retain separate build/deploy workflows for their own paths. Release tags publish CLI, MCP and data archives plus matching service images. Personal VM deployment is triggered manually with a published tag through the `Personal Cully deployment` workflow; it checks the published images before updating services.

Build server images with `docker build -t cully-mcp .` and `docker build -f Dockerfile.data-api -t cully-data .`. GoReleaser builds independent CLI, MCP and data archives from a Cully release tag.

Run Team E2E locally against a disposable PostgreSQL database:

```sh
CULLY_E2E_TEAM_DATABASE_URL=postgresql://test:test@localhost:5432/cully_test \
  go test ./tests/e2e -run '^TestTeam$' -race -count=1 -v
```

The test builds the current CLI, MCP and data binaries with the race detector, applies migrations in an isolated schema, starts loopback services and a temporary JWKS provider, and removes its schema and processes on completion. Solo's complete installation flow runs with `bash tests/e2e/setup.sh` on an isolated CI runner; `TestSolo` targets the local stack through `CULLY_E2E_MCP_URL`. Ordinary Go test runs skip each mode unless its environment variable is set. The published-CLI workflow option applies to Solo; Team always exercises the current checkout.

After a release's archives and service images are published, verify the real distribution path from the matching checkout:

```sh
gh workflow run e2e-test.yml --ref main -f published_cli=true
```

Published mode keeps the downloaded CLI; candidate mode replaces it with the checkout's binary and stack scripts. Both run `cully setup --all` and `cully setup --all --prepare` in a disposable home. The published CLI's version must match the checkout's newest stable tag.

Keep private-memory validation in `internal/memory`, Team policies in `internal/workspace`, SQL in `internal/store/postgres`, protocol handlers in `internal/transport`, and initialization in `internal/app`. Workspace access, projections, task lifecycle, evidence/review and learning/playbook handlers have separate responsibilities; a shared operation table defines routing, authorization scope and read/write status. Follow the [architecture](architecture.md) and [roadmap](roadmap.md) when extending session capture or synchronization.

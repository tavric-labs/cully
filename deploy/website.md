# Website and documentation hosting

Cully's website is **https://cully.net** and its docs are **https://docs.cully.net**. They have separate CI, images, containers, deployment workflows and rollback histories. Changes under `site/` deploy the website; changes under `docs/` deploy the docs.

## Build

Use Node.js 20 or newer:

```sh
npm ci
npm run site:build
```

The combined build produces `dist/site` and `dist/docs`. Run `npm run website:build` or `npm run docs:build` to build one site. To preview the docs locally, run `npm run site:preview`. The public website uses the Go `cully-web` server so testimonial submissions and uploads work alongside the static assets. For a local preview:

```sh
npm run website:build
CULLY_WEB_SITE_DIR=dist/site CULLY_WEB_DATA_DIR=/tmp/cully-web-preview go run ./cmd/cully-web
```

Open `http://localhost:8080`. Do not serve the repository root: it includes source files and deployment notes.

The docs source is Markdown in `docs/`. `docs/.vitepress/config.mts` defines the public navigation. Every Markdown guide under `docs/` is built; these deployment notes stay outside the public docs site. Product and documentation links assume the two production domains. Local previews can follow the corresponding links in the sidebar.

## Publish on the existing VM

`Dockerfile.website` packages the website and its Go submission server as `ghcr.io/tavric-labs/cully-web:<commit>`. `Dockerfile.docs` packages only the docs as `ghcr.io/tavric-labs/cully-docs:<commit>`. Their Compose files run `cully-web` and `cully-docs` separately on the existing `workspace_workspace` network. The VM's Docker Caddy routes `cully.net` to `cully-web:8080` and `docs.cully.net` to `cully-docs:8080`. Nginx resolves clean docs URLs such as `/quickstart` to generated HTML. No public container ports are required.

`website-deploy.yml` runs for website inputs; `docs-deploy.yml` runs for docs and package inputs. Each workflow builds and smoke-tests its own image, publishes it to GHCR, deploys only its own Compose project, and verifies its own public revision marker. Pull requests run the build checks without publishing. The restricted SSH controller keeps independent state and restores the previous image if startup or public verification fails. Its temporary registry token is not retained on the VM.

### GitHub configuration

| Setting | Type | Purpose |
| --- | --- | --- |
| `CULLY_WEB_SSH_KEY` | Secret | Dedicated website deployment private key |
| `CULLY_WEB_KNOWN_HOSTS` | Secret | Pinned VM SSH host key |
| `CULLY_WEB_HOST` | Variable | VM IP, currently `103.181.176.61` |
| `CULLY_WEB_SSH_USER` | Variable | `root`, restricted to the deployment controller |
| `CULLY_WEB_SSH_PORT` | Variable | `22` |
| `CULLY_WEB_DEPLOY_ENABLED` | Variable | Enables the production deploy jobs |
| `CULLY_SITE_DEPLOY_V2_ENABLED` | Variable | Enables the separate website and docs controller |
| `CULLY_DOCS_ROUTE_V2_ENABLED` | Variable | Enables public docs route verification |

The `website-production` environment records both site deployments, while separate concurrency groups let either site deploy independently. `GITHUB_TOKEN` is provided by Actions for GHCR publishing/pulling; no personal registry token is required.

### VM controller

The current VM has separate `cully-web` and `cully-docs` Compose projects. Its Caddy configuration routes `cully.net` to `cully-web:8080` and `docs.cully.net` to `cully-docs:8080`. The controller is installed by `deploy/bootstrap-website.sh` and accepts only the website and docs deployment actions through its forced SSH command. The website state is in `/opt/cully-web/state.json`; docs state is in `/opt/cully-web/docs-state.json`.

To rebuild the controller on a replacement VM, copy `deploy/` and the dedicated deployment public key there, then run `sh deploy/bootstrap-website.sh /path/to/deployment-key.pub` as root. Install the two Caddy routes from `deploy/Caddyfile.website.fragment`, validate the Caddyfile and reload Caddy. Set the three deployment variables to `true` after the routes and controller are ready. Changes to either site then deploy that site from `main`.

The website domains do not change an operator's MCP resource URL. See the [team deployment guide](../docs/team-deployment.md) for the MCP and authorization server relationship.

## Community testimonials

The homepage shows a horizontal carousel with floating cards and left/right controls when approved testimonials are available. A single “Share your experience” button opens a compact submission dialog; optional photo, LinkedIn, and role fields are grouped under an expandable row. Visitors submit directly to `POST /api/testimonials`; no GitHub account is needed. Publication consent is required. Photos are limited to 5 MB and JPG, PNG, or WebP; PNG and JPG dimensions are checked against a 4096-pixel limit.

The Go website service stores pending records and photos in `/var/lib/cully-web`, mounted from the named `cully-web_testimonials` Docker volume. The read-only container can be replaced without losing submissions. Back up this volume separately from images and Git. Submissions contain personal information and must never be committed to the repository.

Review from the VM with the current `CULLY_WEB_REVISION` set:

```sh
docker compose --project-name cully-web --file /opt/cully-web/compose.yaml exec cully-web cully-web list
docker compose --project-name cully-web --file /opt/cully-web/compose.yaml exec cully-web cully-web approve SUBMISSION_ID
docker compose --project-name cully-web --file /opt/cully-web/compose.yaml exec cully-web cully-web reject SUBMISSION_ID
```

Approval publishes the record and photo on the next page load, with no rebuild needed. Rejection deletes the pending record and photo. There is no public moderation endpoint. `/testimonials.json` and `/testimonial-photos/ID` expose only approved submissions. The page renders text safely. Until real testimonials are approved, it shows a compact invitation without placeholder quote cards. Previously curated entries in `site/testimonials.json` remain supported.

The form's LinkedIn import calls `POST /api/linkedin-profile` to read metadata a profile exposes publicly. It can fill available name, role, workplace, and photo fields; visitors review and edit those values before submitting. Fetches are restricted to LinkedIn profile URLs and photos from `media.licdn.com`, with bounded responses and timeouts. Restricted profiles, sign-in pages, and blocked requests fall back to manual entry. It does not bypass LinkedIn sign-in or use the restricted Profile API. LinkedIn's [Profile API](https://learn.microsoft.com/en-us/linkedin/shared/integrations/people/profile-api) requires approved access and member authorization for richer imports.

Submission routes require same-origin requests, validate fields and image content, limit simultaneous uploads, and apply a request budget. The pending inbox holds up to 100 submissions plus in-flight uploads; review it regularly. Set `CULLY_WEB_DATA_DIR`, `CULLY_WEB_SITE_DIR`, and `CULLY_WEB_LISTEN` to change runtime paths or the listen address.

## Installer download

The website serves the repository's installer at `https://cully.net/install.sh`. The website build copies `install.sh` into its static image, and an installer change triggers only the website deployment. Keep the displayed `curl -fsSL https://cully.net/install.sh | sh` command aligned with [installation](../docs/installation.md).

### Tavric Labs registry namespace migration

The GitHub organization is now `tavric-labs`; source publishing and Compose references use `ghcr.io/tavric-labs`. The installed restricted SSH controllers contain their own image prefixes, so updating the workflow alone is insufficient. Before merging the namespace change into a branch that automatically deploys, snapshot the existing controller files, Compose files and deployment-state JSON; update the website/docs and personal-data controllers and their Compose files together using the reviewed bootstrap procedures and the existing deployment public keys. Preserve state, credentials, certificates and Caddy routes. Verify the matching current/rollback image tags are accessible in the new namespace before switching the installed Compose references. Rehearse the controller's fake-runner regressions first. A source change or successful image build is not evidence that the VM controller has been migrated.

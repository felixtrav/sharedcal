# SharedCal

**One shared calendar for a group: sign in with PocketID (or any OIDC
provider), see each other's changes live, and work on events together with
comments, attachments and suggested edits.**

SharedCal is self-hosted and ships as one small container image (a static Go
binary with the React app embedded). It runs on SQLite by default, and
PostgreSQL or MySQL/MariaDB work as drop-in alternatives. MIT licensed.

- **One calendar per deployment.** Everyone who signs in sees the same calendar.
  Tags cover "sub-calendars".
- **OIDC only.** No local passwords. Groups from the IdP map to three roles:
  Viewer, Writer, Admin.
- **Live.** A change shows up in every open browser within about a second
  (Server-Sent Events).
- **Collaborative.** Anyone can comment. Writers can *suggest* edits to events
  they don't own, and owners accept them field by field.
- **Attachments, treated as hostile.** The server checks the real file type,
  re-encodes images, strips metadata and serves files so they can't run in the
  browser. ClamAV scanning is optional.
- **Calm UI.** Month/week/day/agenda views, drag to create, move and resize,
  light and dark themes, keyboard shortcuts, WCAG 2.2 AA.
- **Read-only ICS feed** per user, for Apple/Google/Thunderbird calendars.

## Screenshots

> _Screenshots will be added with the 1.0 release._
>
> | Month view | Event panel with comments | Suggestion review | Dark mode |
> | --- | --- | --- | --- |
> | `docs/img/month.png` | `docs/img/panel.png` | `docs/img/suggestion.png` | `docs/img/dark.png` |

## Contents

- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Setting up PocketID](#setting-up-pocketid)
- [Roles and group mapping](#roles-and-group-mapping)
- [Other identity providers](#other-identity-providers)
- [Reverse proxy](#reverse-proxy)
- [Databases](#databases)
- [Backups, export and import](#backups-export-and-import)
- [Upgrading](#upgrading)
- [Security model](#security-model)
- [Development](#development)
- [License](#license)

## Quick start

You need a domain with HTTPS (via a [reverse proxy](#reverse-proxy)) and an
OIDC client in your identity provider ([PocketID steps](#setting-up-pocketid)).

The container runs as uid **65532**, so the data directory must be writable by
that user:

```sh
mkdir -p data && sudo chown 65532:65532 data
```

### docker run

```sh
docker run -d --name sharedcal --restart unless-stopped \
  -p 8080:8080 -v "$PWD/data:/data" \
  -e APP_URL=https://cal.example.com \
  -e OIDC_ISSUER=https://id.example.com \
  -e OIDC_CLIENT_ID=sharedcal \
  -e OIDC_CLIENT_SECRET=change-me \
  -e OIDC_ADMIN_GROUPS=calendar-admins \
  -e OIDC_WRITER_GROUPS=calendar-writers \
  ghcr.io/felixtrav/sharedcal:1
```

### Docker Compose

[`deploy/compose.yaml`](deploy/compose.yaml):

```yaml
services:
  sharedcal:
    image: ghcr.io/felixtrav/sharedcal:1
    restart: unless-stopped
    ports: ["8080:8080"]
    volumes: ["./data:/data"]
    environment:
      APP_URL: https://cal.example.com
      OIDC_ISSUER: https://id.example.com
      OIDC_CLIENT_ID: ${OIDC_CLIENT_ID}
      OIDC_CLIENT_SECRET: ${OIDC_CLIENT_SECRET}
      OIDC_ADMIN_GROUPS: calendar-admins
      OIDC_WRITER_GROUPS: calendar-writers
```

Put `OIDC_CLIENT_ID=…` and `OIDC_CLIENT_SECRET=…` in a `.env` file next to it,
then run `docker compose up -d`. More examples:
[`compose.postgres.yaml`](deploy/compose.postgres.yaml) (PostgreSQL) and
[`compose.clamav.yaml`](deploy/compose.clamav.yaml) (virus scanning).

Open `https://cal.example.com` and sign in. To make sure the first person can
administer the instance even before groups are set up, set
`BOOTSTRAP_ADMIN_EMAIL` (or `BOOTSTRAP_ADMIN_SUB`) to their identity.

## Configuration

All configuration is environment variables. Anything an admin changes at
runtime (calendar name, default time zone, branding, themes, attachment
limits) lives on the in-app **Settings** page instead. Invalid values stop the
server at startup with a message listing every problem.

**Secrets from files:** `APP_URL`, `OIDC_ISSUER`, `OIDC_CLIENT_ID`,
`OIDC_CLIENT_SECRET`, `DB_DSN` and `SESSION_SECRET` also accept a `_FILE`
suffix (e.g. `OIDC_CLIENT_SECRET_FILE=/run/secrets/oidc`) for Docker or
Kubernetes secrets. Setting both forms is an error.

| Variable | Default | Purpose |
| --- | --- | --- |
| `APP_URL` | **required** | Public base URL, e.g. `https://cal.example.com`. Used for redirect URIs, cookies (`Secure` when https) and ICS links. |
| `LISTEN_ADDR` | `:8080` | HTTP bind address. |
| `DATA_DIR` | `/data` | SQLite database, attachments, uploaded themes, logo, generated session secret. |
| `DB_DRIVER` | `sqlite` | `sqlite`, `postgres` or `mysql` (also for MariaDB). |
| `DB_DSN` | `$DATA_DIR/sharedcal.db` | Connection string. Required for `postgres`/`mysql`. See [Databases](#databases). |
| `OIDC_ISSUER` | **required** | Provider issuer URL (discovery at `/.well-known/openid-configuration`). |
| `OIDC_CLIENT_ID` | **required** | Client ID. |
| `OIDC_CLIENT_SECRET` | **required** | Client secret. |
| `OIDC_SCOPES` | `openid profile email groups` | Space-separated scopes. Must include `openid`. |
| `OIDC_ROLE_CLAIM` | `groups` | ID-token claim holding an array of group names. |
| `OIDC_ADMIN_GROUPS` | empty | Comma-separated. Membership in any one makes the user **admin**. |
| `OIDC_WRITER_GROUPS` | empty | Comma-separated. Makes the user **writer**. |
| `OIDC_VIEWER_GROUPS` | empty | Comma-separated. Makes the user **viewer**. |
| `DEFAULT_ROLE` | `viewer` | Role when no group matches: `viewer`, `writer` or `admin`. |
| `ALLOW_UNMAPPED_USERS` | `true` | `false` refuses sign-in when no group matches. |
| `BOOTSTRAP_ADMIN_EMAIL` | empty | This email gets admin on login, so a fresh install is never locked out. |
| `BOOTSTRAP_ADMIN_SUB` | empty | Same, by OIDC `sub` (safer if emails can be changed at the IdP). |
| `SESSION_SECRET` | generated into `$DATA_DIR/session_secret` | At least 32 characters. Signs short-lived auth cookies. Set it explicitly when running several replicas. |
| `SESSION_IDLE_TTL` | `168h` | Session ends after this much inactivity. |
| `SESSION_MAX_TTL` | `720h` | Absolute session lifetime. Must be ≥ `SESSION_IDLE_TTL`. |
| `TRUSTED_PROXIES` | empty | Comma-separated IPs/CIDRs whose `X-Forwarded-*` headers are trusted (your reverse proxy). |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `LOG_FORMAT` | `json` | `json` or `text`. |
| `METRICS_ADDR` | empty (off) | `host:port` for Prometheus `/metrics`, e.g. `127.0.0.1:9090`. Keep it private. |
| `ATTACHMENT_MAX_SIZE` | `25MB` | Per-file upload limit (`KB`/`MB`/`GB`, binary units). |
| `ATTACHMENT_MAX_PER_EVENT` | `10` | Files per event. |
| `ATTACHMENT_MAX_PER_COMMENT` | `5` | Files per comment. |
| `ATTACHMENT_QUOTA` | `2GB` | Total attachment storage. Uploads beyond it are refused. |
| `CLAMAV_ADDR` | empty (off) | `host:port` of clamd, e.g. `clamav:3310`. Uploads stay pending until they scan clean. |
| `BACKUP_SCHEDULE` | empty (off) | Cron schedule for in-container backups, e.g. `0 3 * * *` or `@daily`. |
| `BACKUP_KEEP` | `7` | Number of scheduled backups to keep (`0` keeps all). |

The settings admins can change on the Settings page (`calendarName`,
`defaultTimeZone`, `defaultRole`, accent colour, enabled themes, attachment
limits) start from these defaults and are then stored in the database.

## Setting up PocketID

1. In PocketID, create an **OIDC client** named e.g. `sharedcal`:
   - Callback URL: `https://cal.example.com/auth/callback`
   - Logout callback URL: `https://cal.example.com/`
   - Enable **PKCE**.
2. Create **user groups** such as `calendar-admins`, `calendar-writers` and
   `calendar-viewers`, and add people to them. PocketID puts group membership
   in the `groups` claim when the client requests the `groups` scope, which
   SharedCal does by default.
3. Optionally restrict the client to those groups under **Allowed user
   groups**, so only calendar members can sign in at all.
4. Copy the **issuer URL** (your PocketID URL, e.g. `https://id.example.com`),
   the **client ID** and the **client secret** into `OIDC_ISSUER`,
   `OIDC_CLIENT_ID` and `OIDC_CLIENT_SECRET`, and set the group variables:

   ```sh
   OIDC_ADMIN_GROUPS=calendar-admins
   OIDC_WRITER_GROUPS=calendar-writers
   OIDC_VIEWER_GROUPS=calendar-viewers
   ```

SharedCal fetches the discovery document at startup and retries with backoff,
so it can start at the same time as PocketID.

## Roles and group mapping

| | Viewer | Writer | Admin |
| --- | --- | --- | --- |
| See the calendar, comment, subscribe to ICS | ✓ | ✓ | ✓ |
| Create events, edit/delete **own** events | | ✓ | ✓ |
| Suggest edits to others' events, attach files to comments | | ✓ | ✓ |
| Edit/delete anyone's events, review any suggestion | | | ✓ |
| Manage users, roles, tags, themes, settings, see the full audit log | | | ✓ |

- On each sign-in the role is computed from the `OIDC_ROLE_CLAIM` claim. The
  **highest** matching role wins, and group names match case-insensitively.
- No match: the user gets `DEFAULT_ROLE`, or is refused if
  `ALLOW_UNMAPPED_USERS=false`.
- **Role source:** a user's role is either `idp` (recomputed at every login) or
  `manual`. An admin who changes someone's role on the People page pins it
  (`manual`) until it is reset to `idp`. Small installs can manage roles
  entirely in the app and never touch the IdP's groups.
- The server refuses to demote or deactivate the **last active admin**.
- Role changes take effect on the user's next request, because sessions are
  server-side.

## Other identity providers

SharedCal uses only standard OIDC discovery (Authorization Code + PKCE, ID
token verification, RP-initiated logout when advertised), so any compliant
provider works:

| Provider | Notes |
| --- | --- |
| **Authelia** | Add a client with `redirect_uris: [https://cal.example.com/auth/callback]`, `scopes: [openid, profile, email, groups]`, `require_pkce: true`, `pkce_challenge_method: S256`. Groups are in `groups`. |
| **Authentik** | OAuth2/OpenID provider, confidential client. The default `profile` scope mapping already includes `groups`. If `groups` isn't a valid scope there, set `OIDC_SCOPES="openid profile email"`. |
| **Keycloak** | Add a "Group Membership" mapper (claim `groups`, *Full group path* off, *Add to ID token* on) to a client scope named `groups`, or set `OIDC_ROLE_CLAIM` to a claim you already emit. |
| **Zitadel** | Roles arrive in `urn:zitadel:iam:org:project:roles`, which is an object rather than an array. Add an Action that copies them into a `groups` array claim. |

Users are identified by `(issuer, sub)`, never by email, so changing
`OIDC_ISSUER` later makes everyone a new user.

## Reverse proxy

TLS terminates at your proxy. Ready-made snippets:
[Caddy](deploy/proxy/Caddyfile), [Traefik](deploy/proxy/traefik.yml),
[nginx](deploy/proxy/nginx.conf). Each one:

- forwards `X-Forwarded-For`, `X-Forwarded-Proto` and `X-Forwarded-Host`. Set
  `TRUSTED_PROXIES` to the proxy's address so SharedCal believes them
  (client IPs for rate limits and the session list, and the https scheme);
- raises the request body limit to at least `ATTACHMENT_MAX_SIZE` (the
  snippets use 30 MB for the 25 MB default plus multipart overhead);
- **disables response buffering for `/api/stream`.** This is the one setting
  that matters. Live updates are a long-lived Server-Sent Events response. A
  proxy that buffers it delivers changes in bursts, or not at all. In nginx
  that means `proxy_buffering off;` plus a long `proxy_read_timeout` on
  `location = /api/stream`. Caddy and Traefik stream `text/event-stream`
  correctly by default. The server also sends a heartbeat every 25 seconds so
  idle connections are not closed.

Serve SharedCal on its own (sub)domain, not under a path prefix.

## Databases

| | `DB_DRIVER` | Example `DB_DSN` |
| --- | --- | --- |
| SQLite (default) | `sqlite` | `/data/sharedcal.db` |
| PostgreSQL 14+ | `postgres` | `postgres://sharedcal:secret@db:5432/sharedcal?sslmode=disable` |
| MySQL 8+ / MariaDB 10.11+ | `mysql` | `sharedcal:secret@tcp(db:3306)/sharedcal` |

- **SQLite** runs in WAL mode with one writer connection and a read pool. It
  is comfortable for dozens of concurrent users, which is what SharedCal is
  built for.
- For MySQL, SharedCal adds `parseTime=true`, UTC and `utf8mb4` to the DSN
  itself.
- Migrations run automatically at startup under a lock, so concurrent starts
  are safe. `sharedcal migrate` applies them without starting the server.
- **Attachments always live on the `/data` volume**, whichever database you
  use, so mount it in every setup.
- **Switching databases** is two commands, see
  [export and import](#backups-export-and-import).

## Backups, export and import

The `sharedcal` binary has these subcommands. In Docker, run them with
`docker exec sharedcal /sharedcal <command>`, or as a one-off container with
the same volume and environment.

| Command | What it does |
| --- | --- |
| `sharedcal serve` | Run the server (the image's default command). |
| `sharedcal migrate` | Apply pending database migrations and exit. |
| `sharedcal backup --out DIR` | Write a backup directory `DIR/sharedcal-<UTC timestamp>/`. |
| `sharedcal export --out FILE` | Write a portable archive (zip) of the database and files. |
| `sharedcal import --in FILE` | Restore an archive into an **empty** database. |
| `sharedcal healthcheck` | Exit 0 if `/healthz` answers (used by the image's `HEALTHCHECK`). |
| `sharedcal openapi` | Print the OpenAPI 3.1 document. |

### Backups

```sh
docker exec sharedcal /sharedcal backup --out /data/backups
```

- **SQLite:** the database is copied with `VACUUM INTO`, a consistent online
  snapshot taken while the app keeps running.
- **PostgreSQL / MySQL:** the command backs up files only. Dump the database
  with its native tool, which handles consistency and permissions better than
  the app could:
  `pg_dump -Fc -d "$DB_DSN" > sharedcal.dump`, or
  `mysqldump --single-transaction --routines sharedcal > sharedcal.sql`.
- Attachments, uploaded themes and the logo are always included. Blobs that
  were already in the previous backup are **hard-linked**, not copied, so
  daily backups cost little space, and each backup directory is still complete
  by itself.
- **Scheduled:** set `BACKUP_SCHEDULE="0 3 * * *"`, and `BACKUP_KEEP=7` keeps
  the newest seven. Scheduled backups go to `$DATA_DIR/backups`. Copy them
  off the host too, because a backup on the same disk doesn't protect against
  losing the disk.

To restore a SQLite backup, stop the container, then copy `sharedcal.db` and
the `attachments/`, `themes/` and `branding/` directories back into the data
directory, and remove any stale `sharedcal.db-wal` and `sharedcal.db-shm`. See
[docs/operations.md](docs/operations.md) for the full runbook.

### Export and import (switching databases)

```sh
# 1. On the old instance (can stay running; the export is a consistent snapshot)
docker exec sharedcal /sharedcal export --out /data/export.zip

# 2. Point the instance at the new database and create the schema
#    (DB_DRIVER/DB_DSN set to the new database)
docker compose run --rm sharedcal migrate

# 3. Import into the new, empty database
docker compose run --rm sharedcal import --in /data/export.zip
```

The archive holds one JSON-lines file per table, the attachment blobs, themes
and logo, and a manifest with row counts and SHA-256 checksums. It does
**not** include `sessions` (everyone signs in again, which is the safe thing
after a move) or `change_log` (the 24-hour live-update replay buffer; its
sequence number carries over so open browsers resync cleanly). Import refuses
a non-empty database, checks every file's checksum and rejects unsafe paths,
and inserts all rows in one transaction, so a failed import leaves nothing
half-done.

## Upgrading

1. Read the release notes, especially for major versions.
2. Take a backup (`sharedcal backup`, plus `pg_dump`/`mysqldump` if you use
   them).
3. Pull the new image and restart: `docker compose pull && docker compose up -d`.
   Migrations apply automatically at startup, and `/readyz` turns green when
   they are done.

The `:1` tag follows the latest 1.x release, and `:1.2` follows patch releases
of 1.2. Downgrades are not supported once a newer migration has run, so restore
the backup instead. Release images are signed. See [SECURITY.md](SECURITY.md)
for how to verify them.

## Security model

- **Authentication** is delegated to your OIDC provider (Authorization Code +
  PKCE, signature, issuer, audience, expiry and nonce checks). There are no
  local passwords.
- **Sessions** are server-side, with a random 256-bit ID stored only as a
  SHA-256 hash, sent in an `HttpOnly`, `Secure`, `SameSite=Lax` cookie. Admins
  can revoke them instantly.
- **Authorization** lives in one package (`internal/authz`) as pure functions,
  called by every handler and tested over every role × ownership × action
  combination on every supported database. The UI only hides what the
  server's `permissions` object denies.
- **CSRF:** a double-submit token on every non-GET request, plus an `Origin`
  check.
- **Headers:** a strict CSP (`script-src 'self'`, `frame-ancestors 'none'`),
  HSTS on https, `nosniff`, `Referrer-Policy: same-origin`.
- **Attachments:** size capped while streaming, type detected from magic bytes
  and checked against an allow-list, images re-encoded with metadata stripped,
  and everything served with `Content-Security-Policy: sandbox` and as a
  download, except re-encoded images. Optional ClamAV scanning.
- **ICS feed tokens** are 32 random bytes, stored hashed, rotatable and
  read-only. The feed leaves out comments, suggestions and attachments.
- **Rate limits** apply to writes, uploads and `/auth/*`.
- **Supply chain:** pinned dependencies and actions, `govulncheck` and
  `npm audit` in CI, Renovate, and signed multi-arch images with an SBOM and
  provenance.
- **Privacy:** no telemetry. SharedCal stores only what the IdP sends and what
  users enter.

Please report vulnerabilities privately. See [SECURITY.md](SECURITY.md).

## Development

Requirements: Go (version in `go.mod`), Node.js 22+, and Docker for the
PostgreSQL/MySQL tests.

```sh
make dev            # mock OIDC provider + Go server (:8080) + Vite (:5173), hot reload
                    # open http://localhost:5173 and sign in as a test user
make test           # Go tests on SQLite with -race, plus web unit tests
make test-all-dbs   # Go tests on SQLite, PostgreSQL and MySQL (testcontainers)
make lint           # gofmt, go vet, staticcheck, eslint, tsc
make vuln           # govulncheck + npm audit
make build          # web app, then bin/sharedcal with the app embedded
make image          # multi-arch container image via docker buildx
```

The Playwright end-to-end tests (`cd web && npm run test:e2e`) start the mock
OIDC provider and the server themselves. API details for frontend work are in
[docs/api.md](docs/api.md), operations in
[docs/operations.md](docs/operations.md), and design decisions in
[docs/decisions.md](docs/decisions.md). To contribute, start with
[CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE) © 2026 SharedCal contributors.

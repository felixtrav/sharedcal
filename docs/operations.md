# Operations guide

How to run SharedCal in production: health checks, metrics, the reverse
proxy, and the backup/restore runbook. For configuration see the
[README](../README.md#configuration).

## Process and container

- One process, `sharedcal serve`, listens on `LISTEN_ADDR` (`:8080`) and keeps
  all state in `DATA_DIR` (`/data`) plus the database.
- The image is `gcr.io/distroless/static` with no shell. It runs as uid/gid
  **65532** and `/data` must be writable by that user. A named Docker volume
  is set up correctly automatically. For a bind mount, run
  `chown -R 65532:65532 ./data` first. On Kubernetes set
  `securityContext: { runAsUser: 65532, runAsGroup: 65532, fsGroup: 65532 }`.
- The root filesystem can be read-only (`read_only: true` in Compose), because
  everything is written under `/data`. With PostgreSQL/MySQL, `BLOB_STORE=s3`
  and `SESSION_SECRET` set, `/data` holds only upload temp files and can be an
  `emptyDir` or `tmpfs` (see the README's "Running without a data volume").
- Shutdown: on `SIGTERM` the server stops accepting connections, closes SSE
  streams (browsers reconnect and replay from `Last-Event-ID`) and finishes
  in-flight requests.

### Directory layout of `DATA_DIR`

```
/data
├── sharedcal.db          SQLite database (+ -wal, -shm while running)
├── session_secret        generated if SESSION_SECRET is not set; keep it with backups
├── attachments/<ab>/<sha256>   content-addressed blobs: attachments, thumbnails, logo
│                         (BLOB_STORE=fs only; with s3 they live in the bucket)
├── tmp/                  upload temp files (BLOB_STORE=s3 only)
├── themes/*.json         optional drop-in themes, copied into the database at startup
└── backups/              default target of scheduled backups
```

## Health endpoints

| Endpoint | Meaning | Use for |
| --- | --- | --- |
| `GET /healthz` | Process is up and serving HTTP. Never touches the database. | Liveness probes, the image `HEALTHCHECK` (`/sharedcal healthcheck`) |
| `GET /readyz` | Database reachable and all migrations applied. | Readiness probes, load balancer checks |

Both return `{"status": "ok"}` with 200, or 503 with a reason.

```yaml
# Kubernetes
livenessProbe:  { httpGet: { path: /healthz, port: 8080 }, periodSeconds: 30 }
readinessProbe: { httpGet: { path: /readyz,  port: 8080 }, periodSeconds: 10 }
```

`sharedcal healthcheck` calls `http://127.0.0.1:<port of LISTEN_ADDR>/healthz`
and exits 0 or 1. Docker uses it because there is no `curl` in the image.

## Metrics

Set `METRICS_ADDR=127.0.0.1:9090` (or a private interface) to serve Prometheus
metrics at `http://METRICS_ADDR/metrics`. They are served on a separate
listener, never on the public port. With Docker, publish it only to a
monitoring network.

| Metric | Type | Labels | Meaning |
| --- | --- | --- | --- |
| `sharedcal_http_request_duration_seconds` | histogram | `method`, `route`, `code` | Request latency by route pattern (not raw path) |
| `sharedcal_http_requests_in_flight` | gauge | | Requests being served |
| `sharedcal_sse_clients` | gauge | | Open `/api/stream` connections |
| `sharedcal_sse_messages_total` | counter | `type` | Change messages fanned out |
| `sharedcal_db_open_connections` | gauge | `pool` (`read`/`write`) | `database/sql` pool stats (also `_in_use`, `_idle`, `_wait_count_total`, `_wait_duration_seconds_total`) |
| `sharedcal_attachment_storage_bytes` | gauge | | Attachment bytes stored (against `ATTACHMENT_QUOTA`) |
| `sharedcal_attachment_uploads_total` | counter | `result` (`ok`, `refused`, `infected`, `error`) | Upload outcomes |
| `sharedcal_backup_last_success_timestamp_seconds` | gauge | | Last successful scheduled backup (alert if older than ~26 h) |
| `go_*`, `process_*` | | | Standard Go runtime and process metrics |

> The metric names above are the planned contract. `/metrics` itself is
> authoritative, so check it if a dashboard query finds nothing.

Suggested alerts: `/readyz` failing for 5 minutes; p95 latency of
`GET /api/events` above 250 ms; `sharedcal_attachment_storage_bytes` above
90 % of the quota; no successful backup in 26 hours.

## Logs

Structured JSON on stdout (`LOG_FORMAT=json`), one line per request with a
request ID (`X-Request-ID` is honoured from a trusted proxy and echoed back).
Session IDs, tokens and secrets are never logged. Use `LOG_LEVEL=debug` only
temporarily.

## Reverse proxy

Snippets: [`deploy/proxy/`](../deploy/proxy). Checklist:

1. **TLS** terminates at the proxy, and `APP_URL` is the public `https://` URL.
2. **Forwarded headers:** the proxy sets `X-Forwarded-For`/`-Proto`/`-Host`,
   and `TRUSTED_PROXIES` lists the proxy's address or CIDR (e.g. the Docker
   network `172.16.0.0/12`). Headers from anyone else are ignored.
3. **Body size** of at least `ATTACHMENT_MAX_SIZE` plus about 10 %
   (nginx `client_max_body_size 30m`, Traefik `buffering.maxRequestBodyBytes`,
   Caddy `request_body max_size`). Otherwise uploads fail with 413 from the
   proxy.
4. **SSE:** no response buffering and a long read timeout for `/api/stream`
   (nginx `proxy_buffering off; proxy_read_timeout 1h;`). The server sends a
   heartbeat every 25 seconds. Symptom when this is wrong: other people's
   changes only appear after a refresh, or arrive in bursts.
5. Don't cache `/api/*`. The app sends `Cache-Control` headers, so leave them
   alone.
6. **HTTP/2** is fine and recommended. Browsers limit HTTP/1.1 to six
   connections per host, and one is held by the SSE stream.

## Backups

### What to back up

| Data | Where | How |
| --- | --- | --- |
| Database (SQLite) | `/data/sharedcal.db` | `sharedcal backup` (online `VACUUM INTO` snapshot) |
| Database (PostgreSQL/MySQL) | the DB server | `pg_dump` / `mysqldump --single-transaction` |
| Attachments, logo | `/data/attachments` | `sharedcal backup` (incremental, hard links) |
| Attachments, logo (`BLOB_STORE=s3`) | the bucket | bucket versioning, object lock or replication; `sharedcal backup` leaves them out |
| Themes | the database (`settings`); drop-in files in `/data/themes` | with the database; `sharedcal backup` also copies `/data/themes` |
| Session secret | `/data/session_secret` or `SESSION_SECRET` | your secrets store. Losing it only invalidates in-flight logins. |
| Configuration | your Compose file / env | version control (without secrets) |

**Never copy a live `sharedcal.db` with `cp` or a filesystem snapshot** that
leaves out the `-wal` file. Use `sharedcal backup`.

### Taking backups

```sh
# ad hoc
docker exec sharedcal /sharedcal backup --out /data/backups

# scheduled, inside the container
BACKUP_SCHEDULE="0 3 * * *"   # 5-field cron, local time of the container (UTC unless TZ is set)
BACKUP_KEEP=7                 # newest N backups are kept, older ones pruned
```

Each run creates `backups/sharedcal-YYYYMMDDTHHMMSSZ/`:

```
sharedcal-20261026T030000Z/
├── backup.json        manifest: time, dialect, file counts
├── sharedcal.db       SQLite only
├── attachments/…      unchanged blobs hard-linked to the previous backup
├── themes/…
└── branding/…
```

A backup is written to a hidden `.tmp-…` directory and renamed into place only
when it is complete, so a crash can't leave a half-written backup behind.
Because unchanged blobs are hard links, `du` on one backup shows its full
size, while the total across all backups grows only by the new files. Every
backup directory is self-contained, so you can delete any of them.

`backups/` must not be inside `attachments/`, `themes/` or `branding/`, and
the command refuses if it is. **Copy backups off the host** (restic, rclone,
borg…). Use `rsync -aH` or a tool that preserves hard links, otherwise the
copy multiplies in size.

For PostgreSQL:

```sh
docker compose exec db pg_dump -U sharedcal -Fc sharedcal > sharedcal-$(date -u +%F).dump
```

For MySQL/MariaDB:

```sh
docker compose exec db mysqldump --single-transaction --routines -uroot -p sharedcal > sharedcal-$(date -u +%F).sql
```

### Restoring (SQLite)

```sh
docker compose stop sharedcal
B=data/backups/sharedcal-20261026T030000Z
mv data/sharedcal.db data/sharedcal.db.broken 2>/dev/null || true
rm -f data/sharedcal.db-wal data/sharedcal.db-shm
cp "$B/sharedcal.db" data/sharedcal.db
rsync -a --delete "$B/attachments/" data/attachments/
[ -d "$B/themes" ]   && rsync -a --delete "$B/themes/"   data/themes/
[ -d "$B/branding" ] && rsync -a --delete "$B/branding/" data/branding/
chown -R 65532:65532 data/sharedcal.db data/attachments data/themes data/branding
docker compose start sharedcal
```

Migrations from a newer version run on start. Restoring a backup taken by a
*newer* SharedCal into an older one is not supported.

### Restoring (PostgreSQL/MySQL)

Restore the database dump with `pg_restore --clean --if-exists -d sharedcal`
(or `mysql sharedcal < dump.sql`), then restore `attachments/`, `themes/` and
`branding/` from the matching `sharedcal backup` as above. Take both at about
the same time. A blob that is newer than the database is harmless, but a row
whose blob is missing shows a broken attachment.

### Test your restores

Restore the latest backup into a scratch container with a different port and
`DATA_DIR` once a quarter, and sign in.

## Export and import

`sharedcal export --out FILE` / `sharedcal import --in FILE` move an instance
between databases or hosts. The archive is a zip:

| Entry | Content |
| --- | --- |
| `manifest.json` | format version, app version, source dialect, creation time, row counts, SHA-256 and size of every file, change_log high-water mark |
| `tables/<table>.jsonl` | one JSON object per row, keys are column names, ordered by primary key |
| `attachments/<ab>/<sha256>` | every blob the database references (attachments, thumbnails, logo), read through the configured blob store |
| `files/themes/…`, `files/branding/…` | only in archives from older versions; still restored into `DATA_DIR` |

- Tables are read page by page inside one read transaction, which gives a
  consistent snapshot and constant memory. The export can run while the
  server is up.
- **Not exported:** `sessions`, because they are credentials (everyone signs
  in again after an import), and `change_log`, the 24-hour live-update replay
  buffer. Its highest `seq` is recorded, and import continues the sequence
  after it, so open browsers resync instead of missing changes.
- **Import** requires a freshly migrated, **empty** database (run
  `sharedcal migrate` first). It validates the whole archive before writing
  anything. It rejects absolute paths, `..`, symlinks, unknown entries and
  oversized entries (zip-bomb limits: 4 GiB per entry, 256 GiB in total,
  32 MiB per row). It restores blobs first, into the configured blob store
  (local or S3), each verified against its SHA-256 before it is stored, then inserts
  all rows in dependency order, in batches, in **one transaction**. IDs are
  preserved. If anything fails, no rows are committed, and re-running is safe.
- Blobs that already exist are skipped, so importing onto the same `/data`
  volume or bucket (the usual SQLite → PostgreSQL move) is fast. Export and
  import also work across stores, e.g. to move from local files to S3.

```sh
# SQLite → PostgreSQL, same host and volume
docker exec sharedcal /sharedcal export --out /data/export.zip
docker compose down
#   edit compose: DB_DRIVER=postgres, DB_DSN=postgres://…  (see deploy/compose.postgres.yaml)
docker compose up -d db
docker compose run --rm sharedcal migrate
docker compose run --rm sharedcal import --in /data/export.zip
docker compose up -d
```

## Upgrades

1. Back up.
2. `docker compose pull && docker compose up -d`.
3. Watch the logs for `database migrated` and wait for `/readyz`.

Running several replicas: migrations take a database lock, so simultaneous
starts are safe. Set `SESSION_SECRET` explicitly so all replicas share it.
Live updates currently need a single replica, or sticky sessions plus the
upcoming PostgreSQL `LISTEN/NOTIFY` broker.

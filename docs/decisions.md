# Decisions

Choices made where the spec is silent, newest at the bottom of each section.

## Operations and packaging

- **SQLite backups use `VACUUM INTO`**, not the C `sqlite3_backup_*` API. The pure-Go driver only exposes the latter through driver-specific raw connections. `VACUUM INTO` is plain SQL, takes a consistent snapshot inside one read transaction, and produces a compact standalone file. It occupies the single writer connection while it runs, which is negligible at SharedCal's database sizes.
- **Backups are directories, not tarballs.** `sharedcal-<UTC timestamp>/` with blobs hard-linked to the previous backup, so every backup is self-contained and prunable while unchanged blobs cost no space. They are written to `.tmp-…` and renamed when complete. `BACKUP_KEEP=0` keeps everything.
- **`backup.Schedule` returns an error** for an invalid `BACKUP_SCHEDULE` (so startup fails fast) and runs in the background. Runs never overlap, and missed slots are skipped. Schedules use the container's local time (UTC unless `TZ` is set) or a `CRON_TZ=` prefix.
- **Export format is a zip** (random access lets import validate every entry before writing anything) with JSON-lines per table keyed by column name, times in RFC 3339 UTC. `sessions` and `change_log` are excluded. Only the change_log high-water mark is kept, and import advances the sequence past it so reconnecting browsers resync instead of missing events.
- **Import restores files before rows.** A failed row import then leaves only harmless content-addressed blobs and can be retried. The reverse order could commit rows pointing at missing blobs. All rows go in one transaction, except MySQL's `AUTO_INCREMENT` bump, which runs after commit because DDL commits implicitly there.
- **Import refuses unknown tables/columns** (archives from a newer schema) and tolerates missing columns (older archives get zero values). `archive.FormatVersion` is bumped only for incompatible layout changes.
- **The image has no QEMU step.** Builder stages run on `$BUILDPLATFORM` and cross-compile, and the distroless runtime stage has no `RUN`, so multi-arch builds are fast.
- **Atlas lives outside the main module** (`tools/atlasloader` with its own `go.mod`, config in `deploy/atlas/atlas.hcl`) so the app's dependency graph and `govulncheck` surface don't include Atlas. Because the GORM models don't declare FKs or CHECKs, Atlas output must be reviewed so it doesn't drop them.

## Deviations from the stack table

- **SQLite migrations run through a small in-repo golang-migrate driver** (`internal/store/migrate_sqlite.go`). golang-migrate's bundled `sqlite` driver imports `modernc.org/sqlite`, which registers the same `database/sql` name as `glebarez/go-sqlite` (used by the GORM dialector), so the two cannot be linked into one binary. PostgreSQL and MySQL use golang-migrate's own `pgx/v5` and `mysql` drivers, and their advisory/`GET_LOCK` migration locks.
- **The initial migration was generated from one table description** (`tools/schema-gen.py`) rather than by Atlas, so that FKs and CHECK constraints are identical in all three dialects. Later migrations can be generated with Atlas (see CONTRIBUTING.md) and must be reviewed. `TestModelsMatchMigrations` fails if a model and the schema drift apart.
- **Repositories are not a separate layer.** `internal/store` holds the models and connection handling; the domain services query GORM directly inside their transactions. A repository layer would only forward calls at this size.

## Data and schema

- **Three columns beyond the spec's table:** `events.series_end_at` (end of the last occurrence, so range queries can skip finished series), `events.deleted_by_id` (for the owner's undo window), `notifications.event_id` and `activity.event_id` (so the inbox and an event's History tab need no joins through polymorphic subjects), and `change_log.audience_user_id` (so per-user messages are filtered on replay too). `sessions.last_seen_at` throttles idle-extension writes.
- **`activity` has only `created_at`**: it is append-only, so `updated_at` would always equal it.
- **MySQL tables use `utf8mb4_bin`.** Case-insensitive collation would make OIDC subjects that differ only in case collide (and match each other in lookups). Tag-name uniqueness is case-insensitive and is enforced by the application instead.
- **Mentions are stored as notification rows** (`kind = comment.mention`), not a separate column; the comment DTO's `mentions` list is read from them.
- **Times read back from the database are always in `time.UTC`.** MySQL gets `loc=UTC` in its DSN; PostgreSQL connections set the session time zone to UTC and register a `timestamptz` codec that scans into UTC, because pgx otherwise returns `time.Local` and API output would carry the server's offset.
- **Event times are truncated to whole seconds** (calendar granularity; keeps text-encoded SQLite timestamps comparable).

## Behavior

- **Bootstrap admin is pinned** (`role_source = manual`), so a later IdP group recompute cannot demote the only way into a fresh install. `BOOTSTRAP_ADMIN_EMAIL` only matches when the IdP says `email_verified = true`.
- **Last-admin protection also applies to IdP recomputes**: if a login would demote the last active admin, the admin role is kept and a warning is logged.
- **Settings' default role** (Settings page) overrides `DEFAULT_ROLE` for new users whose groups match nothing; `DEFAULT_ROLE` is the initial value.
- **The settings attachment size limit cannot exceed `ATTACHMENT_MAX_SIZE`**, because proxies and the HTTP layer are sized for the environment value.
- **Recurring-event editing.** A single occurrence ("this event") can change title, description, location, status, start and end. Changing the time zone, all-day flag, rule or tags needs "this and following" or "all". Deleting one occurrence adds an `EXDATE` (and drops its override); `event_overrides.cancelled` is kept for CalDAV import. Moving a whole series shifts its EXDATEs and override `RECURRENCE-ID`s by the same amount so exceptions stay attached. "This and following" splits the series: the head gets `UNTIL`, and the tail is a new event (new UID) with the remaining `COUNT`, the later overrides and the later EXDATEs.
- **Allowed RRULEs:** DAILY/WEEKLY/MONTHLY/YEARLY only, no BYHOUR/BYMINUTE/BYSECOND/BYEASTER, `COUNT ≤ 5000`, `INTERVAL ≤ 1000`, `UNTIL` within 200 years, and the rule must occur at least once within 8 years. These bound expansion cost against hostile input. A range query returns at most 2,000 occurrences per event and spans at most 400 days.
- **Changing only an event's time zone keeps its wall-clock times** (19:00 stays 19:00 in the new zone).
- **Owners can undo a delete for 60 seconds** on the server; the UI shows the toast for 10. After that, only admins can restore, for 30 days. An hourly maintenance job then purges events and attachments, and deletes blobs that nothing references any more.
- **Suggestions are always against the whole series** (`scope = all`). A suggestion whose `baseVersion` is older than the event is refused with 409 and the current event, so the author proposes against what is actually there. Conflict flags then cover edits made between proposing and reviewing. Accepting a suggestion is recorded as a normal edit by the reviewer plus a `suggestion.accepted` activity row that names the suggester and the accepted fields.
- **Writers can delete attachments they uploaded** while they are still writers, as the API table's "Uploader, event owner, Admin" says. A demoted uploader can no longer remove them.
- **Thumbnails are served with the normal session check**; the optional signed thumbnail URLs were not needed because thumbnails are same-origin `<img>` requests that carry the session cookie.
- **Session IDs shown on the People page are the first 16 hex characters of the stored hash**: enough to revoke one, useless as a credential.
- **CSRF** is a session-bound HMAC token delivered by `GET /api/me`. Non-GET requests must send it in `X-CSRF-Token`, and it must also match a `SameSite=Strict` cookie (double submit) and come with a same-origin `Origin` (or `Referer`) header. Cookies get the `__Host-` prefix and `Secure` only when `APP_URL` is https, so local http development works.
- **CSP allows `style-src 'unsafe-inline'`**: FullCalendar and Radix set inline `style` attributes for positioning. Scripts stay `'self'` only, and the no-flash theme bootstrap is an external file.
- **SSE payloads are personalized per recipient**: the broker fans one message out, and the stream handler recomputes `permissions` for each user (role cached for 10 s, dropped at once on `user.role_changed`). The `change_log` always keeps its newest row, so a reconnecting client can tell "up to date" from "missed pruned changes" (→ `resync`). Writes are serialized with publishing so messages leave in `seq` order.
- **Upload, download, stream and logo endpoints are plain handlers**, so uploads stream without buffering. They are added to the generated OpenAPI document by hand (`documentRawOperations`).
- **The ICS feed covers one-off events that ended in the last year and every series that hasn't ended.** Feed paths are redacted from request logs.
- **Erasing a user ("hard-delete their data") anonymizes the row instead of deleting it.** `DELETE /api/users/{id}` deletes the user's comments, uploads (blobs included), suggestions, inbox, preferences, feed token, sessions and personal change-log rows, and blanks the audit snapshots of that content and of the user record. The `users` row stays as "Deleted user" (email, avatar and groups cleared, `erased_at` set), because `activity`, `events.owner_id` and reviewers reference it. Its OIDC issuer and subject are cleared too, so signing in again creates a fresh account rather than reviving the old one. Live events must go to another writer or admin in the same request (`transferTo`); trashed ones stay attributed to "Deleted user" until the 30-day purge. A comment that others replied to keeps an empty, deleted row so their replies stay threaded. Change-log rows broadcast to everyone may still name the user until they are pruned (24 h). Admins cannot erase themselves.
- **Removed attachments are restorable only while their event is live and their comment (if any) still exists.** `GET /api/trash/attachments` lists exactly those, and restoring one whose comment was deleted meanwhile returns 409.
- **Presence messages leave out their recipient**, so a client never has to filter itself out.
- **Ownership changes always apply to the whole series**, whatever `scope` says, and the Details tab offers them to admins as a separate control rather than as part of the edit form.
- **`mockoidc` (dev/e2e only)** binds to 127.0.0.1, only redirects to loopback `/auth/callback` URLs and requires PKCE.

## Frontend

- **URL is the source of truth for calendar state**: `?view=&date=&event=&occurrence=&tab=&q=&tags=&owners=`. The view the user picks is also saved as `preferences.defaultView` (except the automatic Agenda default under 768 px).
- **Live cache patching is pure** (`web/src/realtime/applyMessage.ts`, unit-tested). Single events are patched into every cached range they overlap and removed from ranges they left. Recurring series and free-text searches are refetched. Stale versions are ignored. The 600 ms glow skips the user's own writes.
- **Optimistic drag and drop**: the cache is patched before `PATCH`. A failure rolls back; a 409 also stores `problem.current`.
- **The event side panel, forms (Zod, React Hook Form), Markdown, the lightbox and all secondary pages are lazy-loaded** to keep the first load under 300 KB of gzipped JS (`npm run check:size`).
- **Tag text colors are computed, not themed**: each `--tag-N-foreground` is white or near-black, whichever reaches 4.5:1 on that theme's tag color. CI checks this.
- **The rail expands on hover after 200 ms** (hover intent), on keyboard focus, or when pinned. Pinning pushes the content; hover overlays it.
- **Forms edit the version they were opened on.** Live updates keep re-rendering the side panel; the event form ignores them, so a save after someone else's change gets the 409 and the conflict dialog instead of silently overwriting it.
- **Preference changes are saved one at a time**, each sending the latest cached preferences, so quick successive changes (e.g. clock, then week start) cannot overwrite each other.
- **Admin routes render Not Found for non-admins** instead of a page whose data fails to load. The API refuses the data either way.
- **End-to-end tests sign each user in once per run** and reuse the session, because `/auth/*` is rate-limited per IP and the whole suite comes from one address. `E2E_PORT` and `E2E_OIDC_PORT` move the test servers off 8080/9999 so the suite can run next to `make dev`.
- More frontend assumptions and API gaps are in `docs/frontend-notes.md`.

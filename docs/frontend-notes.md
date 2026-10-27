# Frontend notes for the backend author

These are the points where the web app (`web/`) had to assume something, or found a gap
or ambiguity, in `docs/api.md`. Each item says what the client does now. Please reconcile
and tell me if anything should change.

## Wire format assumptions

1. **SSE framing.** The client listens for named events (`event: <type>`). The data is the
   envelope `{seq, type, id, version, payload}`, and the client reads `payload`. `resync`
   may have empty data. Message types it handles: everything in the api.md table, plus
   `resync`.
2. **Hard reconnects.** `EventSource` reconnects on its own and sends `Last-Event-ID`. If
   the connection is closed for good (an HTTP error on connect), the client calls `GET
   /api/me`. On 401 it sends the user to `/auth/login?return=…`. Otherwise it opens a new
   `EventSource` with exponential backoff. A fresh `EventSource` cannot replay missed
   messages, so the client then invalidates all cached queries.
3. **Drag and resize payloads.** Timed events send `start`/`end` as UTC instants (`…Z`).
   All-day events send plain dates (`YYYY-MM-DD`, end exclusive), as you asked. Dragging
   between the all-day row and the time grid also sends `allDay` (plus `timeZone` when it
   becomes timed). Both always send `If-Match: <version>`.
4. **Recurring occurrences.** Range items with a non-null `occurrence` are treated as
   instances. Edit, drag and delete send `scope` plus `occurrence=<that item's
   occurrence>`. "This event" is disabled when the change touches `timeZone`, `allDay`,
   `rrule` or `tags`, following your decisions.md rule. After any scoped write, all event
   ranges are refetched.
5. **Owner edits send a strict diff.** `PATCH` includes only the fields whose values
   changed.
6. **Suggestions send time fields together.** Edit suggestions (`changes`) include only
   changed fields, except that `start`, `end`, `timeZone` and `allDay` are all sent when
   any one of them changed, since the server validates them as a unit. Suggestions are
   always series-wide. Dragging a recurring event you can't edit therefore shows a hint
   instead of a ghost suggestion.
7. **The 409 conflict UI** reads `problem.current` (an `Event`) and compares it with the
   user's diff, field by field, by key name. `start`/`end` in `current` are expected as
   wall times in `current.timeZone`, as api.md says. Retrying sends `If-Match:
   current.version`.
8. **403 with `suggest: true`** on a drag turns into the "Send as suggestion?" ghost
   prompt.
9. **Filters.** Repeated `tag=` and `owner=` parameters are assumed to mean "any of"
   (OR). Live cache patching uses that rule to decide whether an updated event still
   belongs in a filtered range. A free-text `q` range is refetched instead of patched.
10. **Event detail for an occurrence.** `GET /api/events/{id}` returns the master, so the
    panel overlays the occurrence's times and fields from the cached range item. Deep
    links carry `?event=<id>&occurrence=<instant>`.
11. **Inbox pagination** uses `before=<last item id>&limit=50`, and assumes items come
    newest first.
12. **Activity filter values.** The `subjectType` filter offers `event`, `comment`,
    `suggestion`, `attachment`, `tag`, `user`, `settings` and `theme`. Action labels come
    from `activity.actions.<action with dots → underscores>` in `en.json`, falling back
    to the raw action string, so new actions show up without code changes.
13. **Uploaded theme `id`** is validated client-side as `^[a-z0-9][a-z0-9-]{0,63}$`, plus
    the token rules in api.md. The server is authoritative; its 422 `errors[]` messages are
    shown as they are.
14. **Settings units.** `attachmentMaxSize` and `attachmentQuota` are treated as bytes.
    The form shows MB and GB.
15. **`/api/themes?all=true`** is used only on the Settings page.

## Gaps in the API (worth adding)

- ~~**Attaching files to a new comment.**~~ Resolved: `me.permissions.attachToComments`
  says whether the paperclip is offered before the comment exists.
- ~~**Presence includes me.**~~ Resolved: the server leaves the recipient out of their own
  presence messages. The client-side filter stays as a harmless safety net.
- **`eventOwnerId`** on suggestion and attachment SSE payloads isn't needed by the
  client. It invalidates or patches by `eventId`.

## Security and CSP

- **No inline scripts.** The theme bootstrap is `/theme-init.js`, loaded from `<head>`.
  It reads `localStorage["sharedcal.theme.v1"]` (compiled CSS variables plus mode) and
  sets `.dark`, `data-theme` and the variables before React loads.
- **`style-src 'unsafe-inline'` is required.** Radix's scroll lock (used by
  dialogs, menus and selects) injects a `<style>` element, and FullCalendar and Radix set
  inline `style` attributes. Your decisions.md already allows this. I avoided `sonner`
  (it injects CSS at runtime) and used Radix Toast instead.
- **Markdown is sanitized in the client.** It goes through `marked` with raw HTML escaped
  and images rendered as alt text, then through DOMPurify with an allow-list. Only
  http, https and mailto links survive, and every link gets `target="_blank"
  rel="noopener noreferrer nofollow"`. `src/components/Markdown.tsx` is the only place
  that uses `dangerouslySetInnerHTML`, and an ESLint rule enforces that.
- **localStorage holds only conveniences**: the compiled theme, color mode and the rail
  pin. The CSRF token is kept in memory.

## Build and embedding

- `web/dist` is gitignored at the repo root. `//go:embed all:dist` therefore needs
  `npm run build` (or the Makefile's `web-placeholder`) before `go build`. Vite's
  `emptyOutDir` replaces any placeholder on a real build.
- Built-in themes are pure JSON in `web/src/themes/` (`default.json`, `nord.json`), as
  `embed.go` expects. They pass the same validation the server applies, and
  `contrast.test.ts` checks them for 4.5:1 contrast.
- Code splitting: admin pages, Inbox, Preferences, Subscribe, Activity, the event side
  panel, Markdown and the lightbox are lazy-loaded. `npm run check:size` measures the
  entry chunk plus its static imports from Vite's manifest: currently about 282 KB
  gzipped, against the 300 KB budget. FullCalendar and temporal-polyfill account for most
  of that.

## End-to-end environment

- `playwright.config.ts` starts `go run ./cmd/mockoidc` (it waits for
  `http://localhost:9999/.well-known/openid-configuration`) and `go run ./cmd/sharedcal
  serve` (it waits for `http://localhost:8080/readyz`). The server gets `APP_URL`,
  `OIDC_ISSUER`, `OIDC_CLIENT_ID=sharedcal`, `OIDC_CLIENT_SECRET=sharedcal-dev-secret`,
  `OIDC_{ADMIN,WRITER,VIEWER}_GROUPS=calendar-{admins,writers,viewers}`, and a fresh
  `DATA_DIR`. **Assumption:** mockoidc gives alice, bob and carol those groups. If it
  doesn't, change the env in `playwright.config.ts` or `USERS` in `e2e/helpers.ts`.
- `login()` in `e2e/helpers.ts` intercepts the browser's request to
  `http://localhost:9999/authorize` and adds `login_hint=<user>`.
- Set `E2E_BASE_URL` to run against an already running server; no web servers are
  started then. Reports go to `web/playwright-report`.
- Set `E2E_PORT` / `E2E_OIDC_PORT` to start the test servers on other ports (e.g. while
  `make dev` holds 8080 and 9999).
- `signedInContext()` signs each user in once per run and reuses the session cookie, since
  `/auth/*` is rate-limited per IP.

## Deviations from the stack table

- **FullCalendar's shadcn theme** isn't an npm package. It is a shadcn registry block
  (`https://shadcn-registry.fullcalendar.io/monarch-event-calendar.json`). I vendored the
  Monarch flavor's `event-calendar-views.tsx` into
  `src/components/calendar/` with small, commented changes: a `ref` prop, and `--primary`
  for "today" markers to keep AA contrast. The toolbar is our own.
- **Named time zones** are built into FullCalendar v7 through its required peer
  `temporal-polyfill`. There is no luxon or temporal plugin in v7, so none is installed.
- **TypeScript 5.9.3, not 6 or 7.** `typescript-eslint` 8.x supports `<6.1` and
  `openapi-typescript` 7.x needs `^5`.
- **ESLint 9.39.5, not 10.** `eslint-plugin-jsx-a11y` 6.10.2 (the latest) only supports
  ESLint ≤ 9. npm prints a deprecation notice for ESLint 9; it is still the maintenance
  line and `npm audit` reports no vulnerabilities.
- **Releases from the last 14 days were avoided**, so FullCalendar is 7.1.2 (7.1.3 is 10
  days old) and temporal-polyfill is 1.0.5.
- **Toasts** use Radix Toast instead of sonner (CSP, see above).
- **Activity in the navigation.** Admins see "Activity" in the admin group. Non-admins
  get "My activity" in the main group, because the API returns their own activity.

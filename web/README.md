# SharedCal web app

The React single-page app for SharedCal. It is built into `web/dist`, which the Go
server embeds (`web/embed.go`) and serves. There is no Node.js at runtime.

Stack: React 19, TypeScript (strict), Vite, FullCalendar v7 (MIT standard build, shadcn
"Monarch" flavor vendored in `src/components/calendar/event-calendar-views.tsx`),
Radix primitives with shadcn-style components (`src/components/ui`), Tailwind CSS v4,
TanStack Query and Router, React Hook Form with Zod, i18next, marked and DOMPurify.

## Develop

```sh
cd web
npm ci
npm run dev            # http://localhost:5173
```

The dev server proxies `/api` (the SSE stream is not buffered), `/auth`, `/ics` and
`/branding` to the Go server on `http://localhost:8080`. You can point it elsewhere with
`SHAREDCAL_BACKEND=http://host:port npm run dev`. Start the backend from the repo root,
for example with the mock OIDC provider:

```sh
go run ./cmd/mockoidc &
APP_URL=http://localhost:5173 OIDC_ISSUER=http://localhost:9999 \
OIDC_CLIENT_ID=sharedcal OIDC_CLIENT_SECRET=sharedcal-dev-secret \
go run ./cmd/sharedcal serve
```

## Scripts

| Command | What it does |
| --- | --- |
| `npm run dev` | Vite dev server with the API proxy |
| `npm run build` | Type-check, then build to `dist/` |
| `npm run typecheck` | `tsc -b` (app, tests, tooling) |
| `npm run lint` | ESLint (typescript-eslint, react-hooks, jsx-a11y), zero warnings allowed |
| `npm test` | Vitest unit tests (API client, SSE cache patching, recurrence, Markdown sanitizer, themes and contrast, i18n keys) |
| `npm run check:size` | Fails if the JS for the first page load is over 300 KB gzipped (run after `build`) |
| `npm run test:e2e` | Playwright against the real server and mock OIDC (see below) |
| `npm run gen:api` | Regenerate `src/api/schema.d.ts` from `openapi.json` |

CI runs `npm ci && npm run lint && npm run typecheck && npm test && npm run build && npm run check:size`.

## Layout

```
src/
  api/          typed fetch client, endpoints, query keys, hand-written types (types.ts)
  realtime/     EventSource hook, pure cache-patching logic (applyMessage.ts), small UI stores
  components/
    ui/         shadcn-style primitives (Button, Dialog, Select, Toast…)
    layout/     app shell: icon rail, mobile bottom bar, reconnect banner
    calendar/   calendar page, toolbar, quick-create, FullCalendar views
    event/      side panel: details/form, recurrence, attachments, comments, suggestions, history
  pages/        Inbox, Preferences, Subscribe, Activity, admin/*
  themes/       theme JSON files (also embedded by the server), compiler, contrast checks
  i18n/         i18next setup and en.json (every user-visible string)
  lib/          time zones, recurrence (RRULE), Markdown, hotkeys, helpers
public/theme-init.js   applies the saved theme before first paint (CSP forbids inline scripts)
e2e/                   Playwright specs and helpers
```

## Themes

A theme is a JSON file of design tokens (`src/themes/*.json`, schema
`{id, name, author, modes: {light, dark}}`, keys listed in `docs/api.md`). `compile.ts`
turns tokens into CSS custom properties on `:root`. It fills optional tokens from the
required ones and computes readable text colors for the eight tag slots. shadcn
components and FullCalendar read the same variables. `contrast.test.ts` checks every
shipped theme in both modes for 4.5:1 contrast (text on background, on primary, on
muted, and on each tag color). To add a theme, drop a JSON file in `src/themes/` and add
it to `BUILTIN_THEMES` in `compile.ts`.

## API types

`src/api/types.ts` is written by hand to match `docs/api.md`. When the backend's OpenAPI
document is available:

```sh
go run ./cmd/sharedcal openapi > web/openapi.json
cd web && npm run gen:api   # writes src/api/schema.d.ts
```

Then reconcile `types.ts` against the generated schema.

## End-to-end tests

```sh
npx playwright install --with-deps chromium   # once (system libraries need sudo)
npm run build                                 # the server embeds web/dist
npm run test:e2e
```

`playwright.config.ts` starts `go run ./cmd/mockoidc` (port 9999) and
`go run ./cmd/sharedcal serve` (port 8080, fresh data dir under `test-results/`). If
`make dev` is running, move them with `E2E_PORT=18080 E2E_OIDC_PORT=19999 npm run test:e2e`.
Set `E2E_BASE_URL` to test an already running instance instead. Users: `alice` is the admin,
`bob` and `dave` writers and `carol` a viewer; `admin.spec.ts` erases `dave`'s data, so a
later login creates a fresh `dave`. Sign-in lives in `e2e/helpers.ts`: `login()` picks the user
on the mock provider's page, and `signedInContext()` reuses one session per user per run
because `/auth/*` is rate-limited per IP. Reports go to `playwright-report/`.

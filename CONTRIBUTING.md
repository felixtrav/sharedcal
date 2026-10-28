# Contributing to SharedCal

Thanks for helping! Bug reports, docs fixes, themes and code are all welcome.
Before you start on a larger change, please open an issue so we can agree on
the approach. The spec ([spec.md](spec.md)) and
[docs/decisions.md](docs/decisions.md) describe what is in and out of scope.

## Ground rules

- **License and DCO.** SharedCal is MIT licensed, and contributions are
  accepted under the same license. Every commit must carry a
  [Developer Certificate of Origin](https://developercertificate.org/)
  sign-off, which certifies that you wrote the change or have the right to
  submit it:

  ```sh
  git commit -s -m "events: reject zero-length recurrences"
  # adds: Signed-off-by: Your Name <you@example.com>
  ```

  Forgot it? `git commit --amend -s`, or for a whole branch
  `git rebase --signoff main`.
- **Authorization is the core invariant.** Every permission decision goes
  through `internal/authz`. A new action needs rows in the role × ownership ×
  action test table before the handler exists.
- **Definition of done** for a feature: server tests (including permission
  cases), a Playwright test for the user-facing flow, no new `govulncheck` or
  `npm audit` findings, works in light and dark mode, and is keyboard
  accessible.
- New dependencies need a reason in the PR description. The stack is fixed
  (see the spec), so swapping a framework or library is a design discussion.
- When the spec doesn't cover something, pick the simplest option consistent
  with it and add a line to `docs/decisions.md`.

## Setup

```sh
git clone https://github.com/felixtrav/sharedcal && cd sharedcal
make dev        # mock OIDC + Go server + Vite; open http://localhost:5173
```

You need Go (the version in `go.mod`) and Node.js 22+. Docker is only needed
for `make test-all-dbs` and `make image`.

Install the commit hooks once per clone (they need Python for `pipx`):

```sh
pipx install pre-commit
pre-commit install
```

`.pre-commit-config.yaml` scans staged changes for secrets (gitleaks, private
keys, files over 1 MB), formats Go, web (Prettier), Python (ruff) and
whitespace, and lints Go (`go vet`, staticcheck), web (ESLint, `tsc`), the
Dockerfile (hadolint) and the CI workflow (actionlint). Run every hook over
the whole tree with `pre-commit run --all-files`.

## Tests

| Command | Runs |
| --- | --- |
| `make test` | `go test -race ./...` on SQLite, plus Vitest |
| `make test-all-dbs` | the Go suite on SQLite, PostgreSQL 16 and MySQL 8.4 |
| `make lint` | gofmt, `go vet`, staticcheck, eslint, `tsc` |
| `pre-commit run --all-files` | every commit hook, including the secret scan |
| `make vuln` | govulncheck, `npm audit` |
| `cd web && npm run test:e2e` | Playwright against the mock OIDC provider |

Database tests take their database from `storetest.Open(t)`, which gives each
test a fresh, migrated database:

- `SHAREDCAL_TEST_DB` = `sqlite` (default), `postgres` or `mysql`.
- `SHAREDCAL_TEST_DSN` points at a running server, e.g.
  `postgres://sharedcal:sharedcal@localhost:5432/sharedcal?sslmode=disable`
  or `root:sharedcal@tcp(localhost:3306)/sharedcal`. A database is created
  per test, so the user needs `CREATE DATABASE` rights. Without a DSN,
  testcontainers starts a container.

CI runs the Go suite on **SQLite (with `-race`), PostgreSQL 16, MySQL 8.4 and
MariaDB 10.11**, as well as the web checks (audit, lint, typecheck, Vitest
including the theme contrast check, build, bundle size budget), every
pre-commit hook plus a gitleaks scan of the full history, Playwright and a
multi-arch image build. All of it must be green before merging.

## Adding a migration

Migrations are versioned SQL files, one set per dialect, in
`migrations/{sqlite,postgres,mysql}/NNNN_name.{up,down}.sql`. They are
embedded in the binary and applied at startup by golang-migrate. Production
never calls `AutoMigrate`. `0001_init` was generated once from a single table
description by `tools/schema-gen.py`. Every later migration is hand-written
or generated with Atlas, and always reviewed.

1. **Change the GORM model** in `internal/store/models.go`.
2. **Write or generate the SQL for all three dialects**, using the same number
   `NNNN` everywhere.
   - *With Atlas* ([install](https://atlasgo.io/getting-started)). The config
     is `deploy/atlas/atlas.hcl`. The desired schema comes from
     `tools/atlasloader`, a separate Go module so the app never depends on
     Atlas, through `atlas-provider-gorm`. From the repository root:

     ```sh
     atlas migrate diff add_event_color --config file://deploy/atlas/atlas.hcl --env sqlite
     atlas migrate diff add_event_color --config file://deploy/atlas/atlas.hcl --env postgres   # needs Docker
     atlas migrate diff add_event_color --config file://deploy/atlas/atlas.hcl --env mysql      # needs Docker
     ```

     **Review the output carefully.** The GORM models carry no foreign keys or
     CHECK constraints, which live only in the SQL, so Atlas will propose
     dropping them. Delete those statements. Rename the files to the next
     `NNNN_` number if Atlas used a timestamp, and remove any `atlas.sum` it
     created (golang-migrate doesn't use one).
   - *By hand*: copy the style of `0001_init` for each dialect. Use `TEXT` and
     `DATETIME` on SQLite, `VARCHAR(n)`/`TEXT`/`TIMESTAMPTZ` on PostgreSQL, and
     `VARCHAR(n)`/`MEDIUMTEXT`/`DATETIME(6)` with `utf8mb4_bin` on MySQL.
     Enumerations are short strings with a named CHECK constraint.
3. **Write the `down` migration** too.
4. **Keep portability rules:** no arrays or native JSON types (JSON is stored
   as text), no database-specific defaults the models don't know about, UTC
   timestamps, UUIDv7 string keys.
5. **SQLite caveat:** `ALTER TABLE` is limited. To change a column, create a
   new table, copy, drop and rename inside the migration.
6. **MySQL caveat:** DDL commits implicitly, so keep each migration small and
   idempotent where you can.
7. Run `make test-all-dbs`. `TestModelsMatchMigrations` fails if the models
   and the migrated schema disagree.
8. If the change affects export/import (a new table or column), `store.AllModels()`
   must list the new model **in dependency order** (parents before children).
   Archives from older versions must still import: missing columns get their
   zero value. Bump `archive.FormatVersion` only for incompatible changes.

## Code style

- Go: `gofmt`, staticcheck-clean, errors wrapped with context, `log/slog` for
  logging, no global state outside `main`.
- TypeScript: eslint and `tsc` clean. Strings go through i18n, and components
  use theme tokens rather than hard-coded colours.
- Commits: short imperative subject with a package prefix
  (`attachments: …`, `web: …`), and a body explaining *why*.

## Adding a theme

Themes are JSON token files in `web/src/themes/` (built-in) and use the keys
listed in [docs/api.md](docs/api.md#theme-tokens). Foreground/background
contrast must be at least 4.5:1 in both modes. CI checks this. A new theme
makes a good first contribution.

## Reporting security issues

Not in public issues, please. See [SECURITY.md](SECURITY.md).

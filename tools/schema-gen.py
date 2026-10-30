# One-off generator used to produce 0001_init for the three dialects from a
# single table description, so the initial schema stays consistent. Later
# migrations are written (or atlas-generated) per dialect and reviewed.

T = {"pg": {}, "my": {}, "lite": {}}


def ty(kind, d):
    k = kind.split(":")
    base = k[0]
    if base == "id":
        return {"pg": "VARCHAR(36)", "my": "VARCHAR(36)", "lite": "TEXT"}[d]
    if base == "str":
        return {"pg": f"VARCHAR({k[1]})", "my": f"VARCHAR({k[1]})", "lite": "TEXT"}[d]
    if base == "text":
        return {"pg": "TEXT", "my": "MEDIUMTEXT", "lite": "TEXT"}[d]
    if base == "ts":
        return {"pg": "TIMESTAMPTZ", "my": "DATETIME(6)", "lite": "DATETIME"}[d]
    if base == "bool":
        return {"pg": "BOOLEAN", "my": "BOOLEAN", "lite": "NUMERIC"}[d]
    if base == "int":
        return {"pg": "INTEGER", "my": "INT", "lite": "INTEGER"}[d]
    if base == "big":
        return {"pg": "BIGINT", "my": "BIGINT", "lite": "INTEGER"}[d]
    raise Exception(kind)


STD = [("created_at", "ts", True), ("updated_at", "ts", True)]


def q(name, d):
    return f"`{name}`" if d == "my" else f'"{name}"'


tables = []


def table(name, cols, pk, uniques=(), indexes=(), checks=(), fks=(), ts=True, autoinc=None):
    tables.append(
        dict(
            name=name,
            cols=cols + (STD if ts else []),
            pk=pk,
            uniques=uniques,
            indexes=indexes,
            checks=checks,
            fks=fks,
            autoinc=autoinc,
        )
    )


def inlist(col, vals):
    return f"{col} IN (" + ", ".join(f"'{v}'" for v in vals) + ")"


ROLES = ["viewer", "writer", "admin"]
table(
    "users",
    [
        ("id", "id", True),
        ("oidc_issuer", "str:255", True),
        ("oidc_subject", "str:255", True),
        ("email", "str:320", True, "''"),
        ("display_name", "str:200", True, "''"),
        ("avatar_url", "str:2048", True, "''"),
        ("role", "str:16", True),
        ("role_source", "str:16", True),
        ("groups_json", "text", True),
        ("active", "bool", True),
        ("last_login_at", "ts", False),
    ],
    ["id"],
    uniques=[("ux_users_oidc", ["oidc_issuer", "oidc_subject"])],
    checks=[inlist("role", ROLES), inlist("role_source", ["idp", "manual"])],
)
table(
    "user_preferences",
    [
        ("user_id", "id", True),
        ("theme_id", "str:64", True),
        ("color_mode", "str:16", True),
        ("time_zone", "str:64", True, "''"),
        ("week_start", "int", True),
        ("clock_24h", "bool", True),
        ("default_view", "str:16", True),
    ],
    ["user_id"],
    checks=[
        inlist("color_mode", ["light", "dark", "system"]),
        "week_start BETWEEN 0 AND 6",
        inlist("default_view", ["month", "week", "day", "agenda"]),
    ],
    fks=[("user_id", "users", "id", "CASCADE")],
)
table(
    "sessions",
    [
        ("id", "str:64", True),
        ("user_id", "id", True),
        ("expires_at", "ts", True),
        ("idle_expires_at", "ts", True),
        ("last_seen_at", "ts", True),
        ("ip", "str:64", True, "''"),
        ("user_agent", "str:512", True, "''"),
    ],
    ["id"],
    indexes=[("ix_sessions_user", ["user_id"])],
    fks=[("user_id", "users", "id", "CASCADE")],
)
table(
    "events",
    [
        ("id", "id", True),
        ("owner_id", "id", True),
        ("title", "str:200", True),
        ("description", "text", True),
        ("location", "str:1000", True, "''"),
        ("start_at", "ts", True),
        ("end_at", "ts", True),
        ("all_day", "bool", True),
        ("time_zone", "str:64", True),
        ("rrule", "str:1000", True, "''"),
        ("series_end_at", "ts", False),
        ("exdates_json", "text", True),
        ("status", "str:16", True),
        ("version", "big", True),
        ("deleted_at", "ts", False),
        ("deleted_by_id", "id", False),
    ],
    ["id"],
    indexes=[
        ("ix_events_owner", ["owner_id"]),
        ("ix_events_range", ["start_at", "end_at"]),
        ("ix_events_deleted", ["deleted_at"]),
    ],
    checks=[inlist("status", ["confirmed", "tentative"]), "end_at >= start_at", "version >= 1"],
    fks=[("owner_id", "users", "id", None)],
)
table(
    "event_overrides",
    [
        ("id", "id", True),
        ("event_id", "id", True),
        ("recurrence_id", "ts", True),
        ("title", "str:200", False),
        ("description", "text", False),
        ("location", "str:1000", False),
        ("start_at", "ts", False),
        ("end_at", "ts", False),
        ("all_day", "bool", False),
        ("status", "str:16", False),
        ("cancelled", "bool", True),
    ],
    ["id"],
    uniques=[("ux_overrides_occurrence", ["event_id", "recurrence_id"])],
    checks=["status IS NULL OR " + inlist("status", ["confirmed", "tentative"])],
    fks=[("event_id", "events", "id", "CASCADE")],
)
table(
    "tags",
    [("id", "id", True), ("name", "str:64", True), ("color_slot", "str:16", True), ("sort_order", "int", True)],
    ["id"],
    uniques=[("ux_tags_name", ["name"])],
    checks=[inlist("color_slot", [f"tag-{i}" for i in range(1, 9)])],
)
table(
    "event_tags",
    [("event_id", "id", True), ("tag_id", "id", True), ("position", "int", True)],
    ["event_id", "tag_id"],
    indexes=[("ix_event_tags_tag", ["tag_id"])],
    fks=[("event_id", "events", "id", "CASCADE"), ("tag_id", "tags", "id", "CASCADE")],
    ts=False,
)
table(
    "comments",
    [
        ("id", "id", True),
        ("event_id", "id", True),
        ("author_id", "id", True),
        ("parent_id", "id", False),
        ("body", "text", True),
        ("edited_at", "ts", False),
        ("deleted_at", "ts", False),
    ],
    ["id"],
    indexes=[("ix_comments_event", ["event_id"])],
    fks=[("event_id", "events", "id", "CASCADE"), ("author_id", "users", "id", None)],
)
table(
    "attachments",
    [
        ("id", "id", True),
        ("event_id", "id", True),
        ("comment_id", "id", False),
        ("uploader_id", "id", True),
        ("filename", "str:255", True),
        ("content_type", "str:128", True),
        ("size_bytes", "big", True),
        ("sha256", "str:64", True),
        ("storage_key", "str:200", True),
        ("width", "int", False),
        ("height", "int", False),
        ("thumb_key", "str:200", False),
        ("scan_status", "str:16", True),
        ("deleted_at", "ts", False),
    ],
    ["id"],
    indexes=[
        ("ix_attachments_event", ["event_id"]),
        ("ix_attachments_comment", ["comment_id"]),
        ("ix_attachments_sha", ["sha256"]),
        ("ix_attachments_deleted", ["deleted_at"]),
    ],
    checks=[inlist("scan_status", ["pending", "clean", "infected", "skipped"]), "size_bytes >= 0"],
    fks=[("event_id", "events", "id", "CASCADE"), ("uploader_id", "users", "id", None)],
)
table(
    "suggestions",
    [
        ("id", "id", True),
        ("event_id", "id", True),
        ("author_id", "id", True),
        ("kind", "str:16", True),
        ("patch_json", "text", True),
        ("base_version", "big", True),
        ("status", "str:24", True),
        ("reviewer_id", "id", False),
        ("review_note", "text", True),
        ("resolved_at", "ts", False),
    ],
    ["id"],
    indexes=[("ix_suggestions_event", ["event_id"]), ("ix_suggestions_status", ["status"])],
    checks=[
        inlist("kind", ["edit", "delete"]),
        inlist("status", ["open", "accepted", "partially_accepted", "rejected", "withdrawn", "closed"]),
    ],
    fks=[("event_id", "events", "id", "CASCADE"), ("author_id", "users", "id", None)],
)
table(
    "notifications",
    [
        ("id", "id", True),
        ("user_id", "id", True),
        ("kind", "str:32", True),
        ("subject_type", "str:32", True),
        ("subject_id", "id", True),
        ("event_id", "id", False),
        ("actor_id", "id", False),
        ("read_at", "ts", False),
    ],
    ["id"],
    indexes=[("ix_notifications_user", ["user_id", "created_at"])],
    fks=[("user_id", "users", "id", "CASCADE")],
)
table(
    "activity",
    [
        ("id", "id", True),
        ("actor_id", "id", False),
        ("action", "str:64", True),
        ("subject_type", "str:32", True),
        ("subject_id", "id", True),
        ("event_id", "id", False),
        ("before_json", "text", True),
        ("after_json", "text", True),
        ("created_at", "ts", True),
    ],
    ["id"],
    indexes=[
        ("ix_activity_actor", ["actor_id"]),
        ("ix_activity_event", ["event_id"]),
        ("ix_activity_created", ["created_at"]),
    ],
    ts=False,
)
table(
    "change_log",
    [
        ("seq", "big", True),
        ("type", "str:32", True),
        ("subject_id", "id", True),
        ("audience_user_id", "id", False),
        ("payload_json", "text", True),
        ("created_at", "ts", True),
    ],
    ["seq"],
    indexes=[("ix_change_log_created", ["created_at"])],
    ts=False,
    autoinc="seq",
)
table(
    "feed_tokens",
    [("id", "id", True), ("user_id", "id", True), ("token_hash", "str:64", True), ("last_used_at", "ts", False)],
    ["id"],
    uniques=[("ux_feed_tokens_user", ["user_id"]), ("ux_feed_tokens_hash", ["token_hash"])],
    fks=[("user_id", "users", "id", "CASCADE")],
)
table("settings", [("key", "str:64", True), ("value_json", "text", True)], ["key"])


def render(d):
    out = []
    for t in tables:
        lines = []
        for c in t["cols"]:
            name, kind, nn = c[0], c[1], c[2]
            default = c[3] if len(c) > 3 else None
            if t["autoinc"] == name:
                lines.append(
                    {
                        "pg": f"{q(name, d)} BIGSERIAL PRIMARY KEY",
                        "my": f"{q(name, d)} BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY",
                        "lite": f"{q(name, d)} INTEGER PRIMARY KEY AUTOINCREMENT",
                    }[d]
                )
                continue
            s = f"{q(name, d)} {ty(kind, d)}"
            if nn:
                s += " NOT NULL"
            if default is not None:
                s += f" DEFAULT {default}"
            lines.append(s)
        if not t["autoinc"]:
            lines.append("PRIMARY KEY (" + ", ".join(q(c, d) for c in t["pk"]) + ")")
        for i, ch in enumerate(t["checks"]):
            lines.append(f"CONSTRAINT {q('ck_' + t['name'] + '_' + str(i + 1), d)} CHECK ({ch})")
        for col, rt, rc, ondel in t["fks"]:
            s = f"CONSTRAINT {q('fk_' + t['name'] + '_' + col, d)} FOREIGN KEY ({q(col, d)}) REFERENCES {q(rt, d)} ({q(rc, d)})"
            if ondel:
                s += f" ON DELETE {ondel}"
            lines.append(s)
        tail = " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_bin" if d == "my" else ""
        out.append(f"CREATE TABLE {q(t['name'], d)} (\n  " + ",\n  ".join(lines) + f"\n){tail};\n")
        for nm, cols in t["uniques"]:
            out.append(
                f"CREATE UNIQUE INDEX {q(nm, d)} ON {q(t['name'], d)} (" + ", ".join(q(c, d) for c in cols) + ");\n"
            )
        for nm, cols in t["indexes"]:
            out.append(f"CREATE INDEX {q(nm, d)} ON {q(t['name'], d)} (" + ", ".join(q(c, d) for c in cols) + ");\n")
    return "\n".join(out)


def down(d):
    return "".join(f"DROP TABLE IF EXISTS {q(t['name'], d)};\n" for t in reversed(tables))


for d, dirn in [("pg", "postgres"), ("my", "mysql"), ("lite", "sqlite")]:
    with open(f"migrations/{dirn}/0001_init.up.sql", "w") as f:
        f.write(f"-- SharedCal initial schema ({dirn}).\n\n" + render(d))
    with open(f"migrations/{dirn}/0001_init.down.sql", "w") as f:
        f.write(down(d))

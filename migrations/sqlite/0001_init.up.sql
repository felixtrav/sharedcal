-- SharedCal initial schema (sqlite).

CREATE TABLE "users" (
  "id" TEXT NOT NULL,
  "oidc_issuer" TEXT NOT NULL,
  "oidc_subject" TEXT NOT NULL,
  "email" TEXT NOT NULL DEFAULT '',
  "display_name" TEXT NOT NULL DEFAULT '',
  "avatar_url" TEXT NOT NULL DEFAULT '',
  "role" TEXT NOT NULL,
  "role_source" TEXT NOT NULL,
  "groups_json" TEXT NOT NULL,
  "active" NUMERIC NOT NULL,
  "last_login_at" DATETIME,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_users_1" CHECK (role IN ('viewer', 'writer', 'admin')),
  CONSTRAINT "ck_users_2" CHECK (role_source IN ('idp', 'manual'))
);

CREATE UNIQUE INDEX "ux_users_oidc" ON "users" ("oidc_issuer", "oidc_subject");

CREATE TABLE "user_preferences" (
  "user_id" TEXT NOT NULL,
  "theme_id" TEXT NOT NULL,
  "color_mode" TEXT NOT NULL,
  "time_zone" TEXT NOT NULL DEFAULT '',
  "week_start" INTEGER NOT NULL,
  "clock_24h" NUMERIC NOT NULL,
  "default_view" TEXT NOT NULL,
  "density" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("user_id"),
  CONSTRAINT "ck_user_preferences_1" CHECK (color_mode IN ('light', 'dark', 'system')),
  CONSTRAINT "ck_user_preferences_2" CHECK (week_start BETWEEN 0 AND 6),
  CONSTRAINT "ck_user_preferences_3" CHECK (default_view IN ('month', 'week', 'day', 'agenda')),
  CONSTRAINT "ck_user_preferences_4" CHECK (density IN ('comfortable', 'compact')),
  CONSTRAINT "fk_user_preferences_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE TABLE "sessions" (
  "id" TEXT NOT NULL,
  "user_id" TEXT NOT NULL,
  "expires_at" DATETIME NOT NULL,
  "idle_expires_at" DATETIME NOT NULL,
  "last_seen_at" DATETIME NOT NULL,
  "ip" TEXT NOT NULL DEFAULT '',
  "user_agent" TEXT NOT NULL DEFAULT '',
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_sessions_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE INDEX "ix_sessions_user" ON "sessions" ("user_id");

CREATE TABLE "events" (
  "id" TEXT NOT NULL,
  "owner_id" TEXT NOT NULL,
  "title" TEXT NOT NULL,
  "description" TEXT NOT NULL,
  "location" TEXT NOT NULL DEFAULT '',
  "start_at" DATETIME NOT NULL,
  "end_at" DATETIME NOT NULL,
  "all_day" NUMERIC NOT NULL,
  "time_zone" TEXT NOT NULL,
  "rrule" TEXT NOT NULL DEFAULT '',
  "series_end_at" DATETIME,
  "exdates_json" TEXT NOT NULL,
  "status" TEXT NOT NULL,
  "version" INTEGER NOT NULL,
  "deleted_at" DATETIME,
  "deleted_by_id" TEXT,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_events_1" CHECK (status IN ('confirmed', 'tentative')),
  CONSTRAINT "ck_events_2" CHECK (end_at >= start_at),
  CONSTRAINT "ck_events_3" CHECK (version >= 1),
  CONSTRAINT "fk_events_owner_id" FOREIGN KEY ("owner_id") REFERENCES "users" ("id")
);

CREATE INDEX "ix_events_owner" ON "events" ("owner_id");

CREATE INDEX "ix_events_range" ON "events" ("start_at", "end_at");

CREATE INDEX "ix_events_deleted" ON "events" ("deleted_at");

CREATE TABLE "event_overrides" (
  "id" TEXT NOT NULL,
  "event_id" TEXT NOT NULL,
  "recurrence_id" DATETIME NOT NULL,
  "title" TEXT,
  "description" TEXT,
  "location" TEXT,
  "start_at" DATETIME,
  "end_at" DATETIME,
  "all_day" NUMERIC,
  "status" TEXT,
  "cancelled" NUMERIC NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_event_overrides_1" CHECK (status IS NULL OR status IN ('confirmed', 'tentative')),
  CONSTRAINT "fk_event_overrides_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE
);

CREATE UNIQUE INDEX "ux_overrides_occurrence" ON "event_overrides" ("event_id", "recurrence_id");

CREATE TABLE "tags" (
  "id" TEXT NOT NULL,
  "name" TEXT NOT NULL,
  "color_slot" TEXT NOT NULL,
  "sort_order" INTEGER NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_tags_1" CHECK (color_slot IN ('tag-1', 'tag-2', 'tag-3', 'tag-4', 'tag-5', 'tag-6', 'tag-7', 'tag-8'))
);

CREATE UNIQUE INDEX "ux_tags_name" ON "tags" ("name");

CREATE TABLE "event_tags" (
  "event_id" TEXT NOT NULL,
  "tag_id" TEXT NOT NULL,
  "position" INTEGER NOT NULL,
  PRIMARY KEY ("event_id", "tag_id"),
  CONSTRAINT "fk_event_tags_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE,
  CONSTRAINT "fk_event_tags_tag_id" FOREIGN KEY ("tag_id") REFERENCES "tags" ("id") ON DELETE CASCADE
);

CREATE INDEX "ix_event_tags_tag" ON "event_tags" ("tag_id");

CREATE TABLE "comments" (
  "id" TEXT NOT NULL,
  "event_id" TEXT NOT NULL,
  "author_id" TEXT NOT NULL,
  "parent_id" TEXT,
  "body" TEXT NOT NULL,
  "edited_at" DATETIME,
  "deleted_at" DATETIME,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_comments_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE,
  CONSTRAINT "fk_comments_author_id" FOREIGN KEY ("author_id") REFERENCES "users" ("id")
);

CREATE INDEX "ix_comments_event" ON "comments" ("event_id");

CREATE TABLE "attachments" (
  "id" TEXT NOT NULL,
  "event_id" TEXT NOT NULL,
  "comment_id" TEXT,
  "uploader_id" TEXT NOT NULL,
  "filename" TEXT NOT NULL,
  "content_type" TEXT NOT NULL,
  "size_bytes" INTEGER NOT NULL,
  "sha256" TEXT NOT NULL,
  "storage_key" TEXT NOT NULL,
  "width" INTEGER,
  "height" INTEGER,
  "thumb_key" TEXT,
  "scan_status" TEXT NOT NULL,
  "deleted_at" DATETIME,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_attachments_1" CHECK (scan_status IN ('pending', 'clean', 'infected', 'skipped')),
  CONSTRAINT "ck_attachments_2" CHECK (size_bytes >= 0),
  CONSTRAINT "fk_attachments_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE,
  CONSTRAINT "fk_attachments_uploader_id" FOREIGN KEY ("uploader_id") REFERENCES "users" ("id")
);

CREATE INDEX "ix_attachments_event" ON "attachments" ("event_id");

CREATE INDEX "ix_attachments_comment" ON "attachments" ("comment_id");

CREATE INDEX "ix_attachments_sha" ON "attachments" ("sha256");

CREATE INDEX "ix_attachments_deleted" ON "attachments" ("deleted_at");

CREATE TABLE "suggestions" (
  "id" TEXT NOT NULL,
  "event_id" TEXT NOT NULL,
  "author_id" TEXT NOT NULL,
  "kind" TEXT NOT NULL,
  "patch_json" TEXT NOT NULL,
  "base_version" INTEGER NOT NULL,
  "status" TEXT NOT NULL,
  "reviewer_id" TEXT,
  "review_note" TEXT NOT NULL,
  "resolved_at" DATETIME,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_suggestions_1" CHECK (kind IN ('edit', 'delete')),
  CONSTRAINT "ck_suggestions_2" CHECK (status IN ('open', 'accepted', 'partially_accepted', 'rejected', 'withdrawn', 'closed')),
  CONSTRAINT "fk_suggestions_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE,
  CONSTRAINT "fk_suggestions_author_id" FOREIGN KEY ("author_id") REFERENCES "users" ("id")
);

CREATE INDEX "ix_suggestions_event" ON "suggestions" ("event_id");

CREATE INDEX "ix_suggestions_status" ON "suggestions" ("status");

CREATE TABLE "notifications" (
  "id" TEXT NOT NULL,
  "user_id" TEXT NOT NULL,
  "kind" TEXT NOT NULL,
  "subject_type" TEXT NOT NULL,
  "subject_id" TEXT NOT NULL,
  "event_id" TEXT,
  "actor_id" TEXT,
  "read_at" DATETIME,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_notifications_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE INDEX "ix_notifications_user" ON "notifications" ("user_id", "created_at");

CREATE TABLE "activity" (
  "id" TEXT NOT NULL,
  "actor_id" TEXT,
  "action" TEXT NOT NULL,
  "subject_type" TEXT NOT NULL,
  "subject_id" TEXT NOT NULL,
  "event_id" TEXT,
  "before_json" TEXT NOT NULL,
  "after_json" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  PRIMARY KEY ("id")
);

CREATE INDEX "ix_activity_actor" ON "activity" ("actor_id");

CREATE INDEX "ix_activity_event" ON "activity" ("event_id");

CREATE INDEX "ix_activity_created" ON "activity" ("created_at");

CREATE TABLE "change_log" (
  "seq" INTEGER PRIMARY KEY AUTOINCREMENT,
  "type" TEXT NOT NULL,
  "subject_id" TEXT NOT NULL,
  "audience_user_id" TEXT,
  "payload_json" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL
);

CREATE INDEX "ix_change_log_created" ON "change_log" ("created_at");

CREATE TABLE "feed_tokens" (
  "id" TEXT NOT NULL,
  "user_id" TEXT NOT NULL,
  "token_hash" TEXT NOT NULL,
  "last_used_at" DATETIME,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_feed_tokens_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE UNIQUE INDEX "ux_feed_tokens_user" ON "feed_tokens" ("user_id");

CREATE UNIQUE INDEX "ux_feed_tokens_hash" ON "feed_tokens" ("token_hash");

CREATE TABLE "settings" (
  "key" TEXT NOT NULL,
  "value_json" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("key")
);

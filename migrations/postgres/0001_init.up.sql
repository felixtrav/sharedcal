-- SharedCal initial schema (postgres).

CREATE TABLE "users" (
  "id" VARCHAR(36) NOT NULL,
  "oidc_issuer" VARCHAR(255) NOT NULL,
  "oidc_subject" VARCHAR(255) NOT NULL,
  "email" VARCHAR(320) NOT NULL DEFAULT '',
  "display_name" VARCHAR(200) NOT NULL DEFAULT '',
  "avatar_url" VARCHAR(2048) NOT NULL DEFAULT '',
  "role" VARCHAR(16) NOT NULL,
  "role_source" VARCHAR(16) NOT NULL,
  "groups_json" TEXT NOT NULL,
  "active" BOOLEAN NOT NULL,
  "last_login_at" TIMESTAMPTZ,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_users_1" CHECK (role IN ('viewer', 'writer', 'admin')),
  CONSTRAINT "ck_users_2" CHECK (role_source IN ('idp', 'manual'))
);

CREATE UNIQUE INDEX "ux_users_oidc" ON "users" ("oidc_issuer", "oidc_subject");

CREATE TABLE "user_preferences" (
  "user_id" VARCHAR(36) NOT NULL,
  "theme_id" VARCHAR(64) NOT NULL,
  "color_mode" VARCHAR(16) NOT NULL,
  "time_zone" VARCHAR(64) NOT NULL DEFAULT '',
  "week_start" INTEGER NOT NULL,
  "clock_24h" BOOLEAN NOT NULL,
  "default_view" VARCHAR(16) NOT NULL,
  "density" VARCHAR(16) NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("user_id"),
  CONSTRAINT "ck_user_preferences_1" CHECK (color_mode IN ('light', 'dark', 'system')),
  CONSTRAINT "ck_user_preferences_2" CHECK (week_start BETWEEN 0 AND 6),
  CONSTRAINT "ck_user_preferences_3" CHECK (default_view IN ('month', 'week', 'day', 'agenda')),
  CONSTRAINT "ck_user_preferences_4" CHECK (density IN ('comfortable', 'compact')),
  CONSTRAINT "fk_user_preferences_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE TABLE "sessions" (
  "id" VARCHAR(64) NOT NULL,
  "user_id" VARCHAR(36) NOT NULL,
  "expires_at" TIMESTAMPTZ NOT NULL,
  "idle_expires_at" TIMESTAMPTZ NOT NULL,
  "last_seen_at" TIMESTAMPTZ NOT NULL,
  "ip" VARCHAR(64) NOT NULL DEFAULT '',
  "user_agent" VARCHAR(512) NOT NULL DEFAULT '',
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_sessions_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE INDEX "ix_sessions_user" ON "sessions" ("user_id");

CREATE TABLE "events" (
  "id" VARCHAR(36) NOT NULL,
  "owner_id" VARCHAR(36) NOT NULL,
  "title" VARCHAR(200) NOT NULL,
  "description" TEXT NOT NULL,
  "location" VARCHAR(1000) NOT NULL DEFAULT '',
  "start_at" TIMESTAMPTZ NOT NULL,
  "end_at" TIMESTAMPTZ NOT NULL,
  "all_day" BOOLEAN NOT NULL,
  "time_zone" VARCHAR(64) NOT NULL,
  "rrule" VARCHAR(1000) NOT NULL DEFAULT '',
  "series_end_at" TIMESTAMPTZ,
  "exdates_json" TEXT NOT NULL,
  "status" VARCHAR(16) NOT NULL,
  "version" BIGINT NOT NULL,
  "deleted_at" TIMESTAMPTZ,
  "deleted_by_id" VARCHAR(36),
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
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
  "id" VARCHAR(36) NOT NULL,
  "event_id" VARCHAR(36) NOT NULL,
  "recurrence_id" TIMESTAMPTZ NOT NULL,
  "title" VARCHAR(200),
  "description" TEXT,
  "location" VARCHAR(1000),
  "start_at" TIMESTAMPTZ,
  "end_at" TIMESTAMPTZ,
  "all_day" BOOLEAN,
  "status" VARCHAR(16),
  "cancelled" BOOLEAN NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_event_overrides_1" CHECK (status IS NULL OR status IN ('confirmed', 'tentative')),
  CONSTRAINT "fk_event_overrides_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE
);

CREATE UNIQUE INDEX "ux_overrides_occurrence" ON "event_overrides" ("event_id", "recurrence_id");

CREATE TABLE "tags" (
  "id" VARCHAR(36) NOT NULL,
  "name" VARCHAR(64) NOT NULL,
  "color_slot" VARCHAR(16) NOT NULL,
  "sort_order" INTEGER NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_tags_1" CHECK (color_slot IN ('tag-1', 'tag-2', 'tag-3', 'tag-4', 'tag-5', 'tag-6', 'tag-7', 'tag-8'))
);

CREATE UNIQUE INDEX "ux_tags_name" ON "tags" ("name");

CREATE TABLE "event_tags" (
  "event_id" VARCHAR(36) NOT NULL,
  "tag_id" VARCHAR(36) NOT NULL,
  "position" INTEGER NOT NULL,
  PRIMARY KEY ("event_id", "tag_id"),
  CONSTRAINT "fk_event_tags_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE,
  CONSTRAINT "fk_event_tags_tag_id" FOREIGN KEY ("tag_id") REFERENCES "tags" ("id") ON DELETE CASCADE
);

CREATE INDEX "ix_event_tags_tag" ON "event_tags" ("tag_id");

CREATE TABLE "comments" (
  "id" VARCHAR(36) NOT NULL,
  "event_id" VARCHAR(36) NOT NULL,
  "author_id" VARCHAR(36) NOT NULL,
  "parent_id" VARCHAR(36),
  "body" TEXT NOT NULL,
  "edited_at" TIMESTAMPTZ,
  "deleted_at" TIMESTAMPTZ,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_comments_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE,
  CONSTRAINT "fk_comments_author_id" FOREIGN KEY ("author_id") REFERENCES "users" ("id")
);

CREATE INDEX "ix_comments_event" ON "comments" ("event_id");

CREATE TABLE "attachments" (
  "id" VARCHAR(36) NOT NULL,
  "event_id" VARCHAR(36) NOT NULL,
  "comment_id" VARCHAR(36),
  "uploader_id" VARCHAR(36) NOT NULL,
  "filename" VARCHAR(255) NOT NULL,
  "content_type" VARCHAR(128) NOT NULL,
  "size_bytes" BIGINT NOT NULL,
  "sha256" VARCHAR(64) NOT NULL,
  "storage_key" VARCHAR(200) NOT NULL,
  "width" INTEGER,
  "height" INTEGER,
  "thumb_key" VARCHAR(200),
  "scan_status" VARCHAR(16) NOT NULL,
  "deleted_at" TIMESTAMPTZ,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
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
  "id" VARCHAR(36) NOT NULL,
  "event_id" VARCHAR(36) NOT NULL,
  "author_id" VARCHAR(36) NOT NULL,
  "kind" VARCHAR(16) NOT NULL,
  "patch_json" TEXT NOT NULL,
  "base_version" BIGINT NOT NULL,
  "status" VARCHAR(24) NOT NULL,
  "reviewer_id" VARCHAR(36),
  "review_note" TEXT NOT NULL,
  "resolved_at" TIMESTAMPTZ,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_suggestions_1" CHECK (kind IN ('edit', 'delete')),
  CONSTRAINT "ck_suggestions_2" CHECK (status IN ('open', 'accepted', 'partially_accepted', 'rejected', 'withdrawn', 'closed')),
  CONSTRAINT "fk_suggestions_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE,
  CONSTRAINT "fk_suggestions_author_id" FOREIGN KEY ("author_id") REFERENCES "users" ("id")
);

CREATE INDEX "ix_suggestions_event" ON "suggestions" ("event_id");

CREATE INDEX "ix_suggestions_status" ON "suggestions" ("status");

CREATE TABLE "notifications" (
  "id" VARCHAR(36) NOT NULL,
  "user_id" VARCHAR(36) NOT NULL,
  "kind" VARCHAR(32) NOT NULL,
  "subject_type" VARCHAR(32) NOT NULL,
  "subject_id" VARCHAR(36) NOT NULL,
  "event_id" VARCHAR(36),
  "actor_id" VARCHAR(36),
  "read_at" TIMESTAMPTZ,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_notifications_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE INDEX "ix_notifications_user" ON "notifications" ("user_id", "created_at");

CREATE TABLE "activity" (
  "id" VARCHAR(36) NOT NULL,
  "actor_id" VARCHAR(36),
  "action" VARCHAR(64) NOT NULL,
  "subject_type" VARCHAR(32) NOT NULL,
  "subject_id" VARCHAR(36) NOT NULL,
  "event_id" VARCHAR(36),
  "before_json" TEXT NOT NULL,
  "after_json" TEXT NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id")
);

CREATE INDEX "ix_activity_actor" ON "activity" ("actor_id");

CREATE INDEX "ix_activity_event" ON "activity" ("event_id");

CREATE INDEX "ix_activity_created" ON "activity" ("created_at");

CREATE TABLE "change_log" (
  "seq" BIGSERIAL PRIMARY KEY,
  "type" VARCHAR(32) NOT NULL,
  "subject_id" VARCHAR(36) NOT NULL,
  "audience_user_id" VARCHAR(36),
  "payload_json" TEXT NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL
);

CREATE INDEX "ix_change_log_created" ON "change_log" ("created_at");

CREATE TABLE "feed_tokens" (
  "id" VARCHAR(36) NOT NULL,
  "user_id" VARCHAR(36) NOT NULL,
  "token_hash" VARCHAR(64) NOT NULL,
  "last_used_at" TIMESTAMPTZ,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "fk_feed_tokens_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE UNIQUE INDEX "ux_feed_tokens_user" ON "feed_tokens" ("user_id");

CREATE UNIQUE INDEX "ux_feed_tokens_hash" ON "feed_tokens" ("token_hash");

CREATE TABLE "settings" (
  "key" VARCHAR(64) NOT NULL,
  "value_json" TEXT NOT NULL,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("key")
);

-- Admin-defined event types (birthday, holiday, ...). A typed event stores
-- values for the type's predefined fields instead of a free-form description.

CREATE TABLE "event_types" (
  "id" TEXT NOT NULL,
  "name" TEXT NOT NULL,
  "icon" TEXT NOT NULL,
  "color_slot" TEXT NOT NULL,
  "default_all_day" NUMERIC NOT NULL,
  "style" TEXT NOT NULL DEFAULT 'default',
  "year_counter" NUMERIC NOT NULL,
  "default_duration_minutes" INTEGER NOT NULL DEFAULT 60,
  "default_rrule" TEXT NOT NULL DEFAULT '',
  "fields_json" TEXT NOT NULL,
  "sort_order" INTEGER NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_event_types_1" CHECK (color_slot IN ('tag-1', 'tag-2', 'tag-3', 'tag-4', 'tag-5', 'tag-6', 'tag-7', 'tag-8')),
  CONSTRAINT "ck_event_types_2" CHECK (style IN ('default', 'banner', 'outline', 'badge')),
  CONSTRAINT "ck_event_types_3" CHECK (default_duration_minutes BETWEEN 0 AND 1440)
);

CREATE UNIQUE INDEX "ux_event_types_name" ON "event_types" ("name");

ALTER TABLE "events" ADD COLUMN "event_type_id" TEXT REFERENCES "event_types" ("id");

ALTER TABLE "events" ADD COLUMN "fields_json" TEXT NOT NULL DEFAULT '{}';

CREATE INDEX "ix_events_type" ON "events" ("event_type_id");

-- An occurrence can have its own type and field values (a movie release in a
-- series of premieres). type_overridden says whether event_type_id (NULL =
-- generic) and fields_json replace the master's.
ALTER TABLE "event_overrides" ADD COLUMN "type_overridden" NUMERIC NOT NULL DEFAULT 0;

ALTER TABLE "event_overrides" ADD COLUMN "event_type_id" TEXT REFERENCES "event_types" ("id");

ALTER TABLE "event_overrides" ADD COLUMN "fields_json" TEXT NOT NULL DEFAULT '{}';

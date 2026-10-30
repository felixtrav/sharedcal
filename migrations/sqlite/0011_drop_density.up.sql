-- Drop the density preference, which only nudged calendar font size and time-grid row height.

-- SQLite cannot drop a column used by a CHECK constraint, so rebuild the table.
CREATE TABLE "user_preferences_new" (
  "user_id" TEXT NOT NULL,
  "theme_id" TEXT NOT NULL,
  "color_mode" TEXT NOT NULL,
  "time_zone" TEXT NOT NULL DEFAULT '',
  "week_start" INTEGER NOT NULL,
  "clock_24h" NUMERIC NOT NULL,
  "default_view" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("user_id"),
  CONSTRAINT "ck_user_preferences_1" CHECK (color_mode IN ('light', 'dark', 'system')),
  CONSTRAINT "ck_user_preferences_2" CHECK (week_start BETWEEN 0 AND 6),
  CONSTRAINT "ck_user_preferences_3" CHECK (default_view IN ('month', 'week', 'day', 'agenda')),
  CONSTRAINT "fk_user_preferences_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

INSERT INTO "user_preferences_new" ("user_id", "theme_id", "color_mode", "time_zone", "week_start", "clock_24h", "default_view", "created_at", "updated_at")
SELECT "user_id", "theme_id", "color_mode", "time_zone", "week_start", "clock_24h", "default_view", "created_at", "updated_at" FROM "user_preferences";

DROP TABLE "user_preferences";
ALTER TABLE "user_preferences_new" RENAME TO "user_preferences";

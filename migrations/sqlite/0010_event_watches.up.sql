-- Event watchers: users who get inbox items when an event changes, and a counter so repeated activity folds into one unread item.

CREATE TABLE "event_watches" (
  "user_id" TEXT NOT NULL,
  "event_id" TEXT NOT NULL,
  "created_at" DATETIME NOT NULL,
  PRIMARY KEY ("user_id", "event_id"),
  CONSTRAINT "fk_event_watches_user_id" FOREIGN KEY ("user_id") REFERENCES "users" ("id") ON DELETE CASCADE,
  CONSTRAINT "fk_event_watches_event_id" FOREIGN KEY ("event_id") REFERENCES "events" ("id") ON DELETE CASCADE
);

CREATE INDEX "ix_event_watches_event" ON "event_watches" ("event_id");

ALTER TABLE "notifications" ADD COLUMN "item_count" INTEGER NOT NULL DEFAULT 1;

-- Owners watch their own events.
INSERT INTO "event_watches" ("user_id", "event_id", "created_at")
SELECT "owner_id", "id", "created_at" FROM "events" WHERE "owner_id" <> '00000000-0000-0000-0000-000000000000';

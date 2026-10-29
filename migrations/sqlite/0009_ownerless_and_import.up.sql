-- Event types whose events have no personal owner, and the iCalendar UID imported events came from.

ALTER TABLE "event_types" ADD COLUMN "ownerless" NUMERIC NOT NULL DEFAULT 0;
ALTER TABLE "events" ADD COLUMN "source_uid" TEXT NOT NULL DEFAULT '';
CREATE INDEX "ix_events_source_uid" ON "events" ("source_uid");

-- Event types whose events have no personal owner, and the iCalendar UID imported events came from.

ALTER TABLE "event_types" ADD COLUMN "ownerless" BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE "events" ADD COLUMN "source_uid" VARCHAR(255) NOT NULL DEFAULT '';
CREATE INDEX "ix_events_source_uid" ON "events" ("source_uid");

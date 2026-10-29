DROP INDEX "ix_events_source_uid";
ALTER TABLE "events" DROP COLUMN "source_uid";
ALTER TABLE "event_types" DROP COLUMN "ownerless";

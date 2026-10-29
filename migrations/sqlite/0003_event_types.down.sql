ALTER TABLE "event_overrides" DROP COLUMN "fields_json";
ALTER TABLE "event_overrides" DROP COLUMN "event_type_id";
ALTER TABLE "event_overrides" DROP COLUMN "type_overridden";
DROP INDEX "ix_events_type";
ALTER TABLE "events" DROP COLUMN "fields_json";
ALTER TABLE "events" DROP COLUMN "event_type_id";
DROP TABLE "event_types";

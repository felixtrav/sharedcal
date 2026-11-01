-- Whether the event form leaves out the location field for an event type.

ALTER TABLE "event_types" ADD COLUMN "hide_location" BOOLEAN NOT NULL DEFAULT FALSE;

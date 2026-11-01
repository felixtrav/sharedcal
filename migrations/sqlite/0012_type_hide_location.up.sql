-- Whether the event form leaves out the location field for an event type.

ALTER TABLE "event_types" ADD COLUMN "hide_location" NUMERIC NOT NULL DEFAULT 0;

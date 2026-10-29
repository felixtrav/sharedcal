-- Whether an event type is reserved for admins to create events of.

ALTER TABLE "event_types" ADD COLUMN "admin_only" NUMERIC NOT NULL DEFAULT 0;

-- Whether an event type is reserved for admins to create events of.

ALTER TABLE "event_types" ADD COLUMN "admin_only" BOOLEAN NOT NULL DEFAULT FALSE;

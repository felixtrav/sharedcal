-- Event types can carry instructions shown when an event of the type is created.

ALTER TABLE "event_types" ADD COLUMN "instructions" VARCHAR(1000) NOT NULL DEFAULT '';

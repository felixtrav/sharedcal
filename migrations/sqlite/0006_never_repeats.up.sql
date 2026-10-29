-- Event types can forbid repetition (releases, one-off milestones).

ALTER TABLE "event_types" ADD COLUMN "never_repeats" NUMERIC NOT NULL DEFAULT 0;

-- Whether an always-all-day event type is limited to a single date. Types that were all-day with a fixed
-- repeat were already single-date, so keep them that way.

ALTER TABLE "event_types" ADD COLUMN "single_day" NUMERIC NOT NULL DEFAULT 0;
UPDATE "event_types" SET "single_day" = 1 WHERE "default_all_day" = 1 AND "default_rrule" <> '';

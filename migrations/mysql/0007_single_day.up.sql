-- Whether an always-all-day event type is limited to a single date. Types that were all-day with a fixed
-- repeat were already single-date, so keep them that way.

ALTER TABLE `event_types` ADD COLUMN `single_day` BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE `event_types` SET `single_day` = TRUE WHERE `default_all_day` = TRUE AND `default_rrule` <> '';

ALTER TABLE `event_overrides` DROP FOREIGN KEY `fk_event_overrides_event_type_id`;
ALTER TABLE `event_overrides` DROP COLUMN `fields_json`;
ALTER TABLE `event_overrides` DROP COLUMN `event_type_id`;
ALTER TABLE `event_overrides` DROP COLUMN `type_overridden`;
ALTER TABLE `events` DROP FOREIGN KEY `fk_events_event_type_id`;
DROP INDEX `ix_events_type` ON `events`;
ALTER TABLE `events` DROP COLUMN `fields_json`;
ALTER TABLE `events` DROP COLUMN `event_type_id`;
DROP TABLE `event_types`;

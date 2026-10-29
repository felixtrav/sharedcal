-- Admin-defined event types (birthday, holiday, ...). A typed event stores
-- values for the type's predefined fields instead of a free-form description.

CREATE TABLE `event_types` (
  `id` VARCHAR(36) NOT NULL,
  `name` VARCHAR(64) NOT NULL,
  `icon` VARCHAR(32) NOT NULL,
  `color_slot` VARCHAR(16) NOT NULL,
  `default_all_day` BOOLEAN NOT NULL,
  `style` VARCHAR(16) NOT NULL DEFAULT 'default',
  `year_counter` BOOLEAN NOT NULL,
  `default_duration_minutes` INT NOT NULL DEFAULT 60,
  `default_rrule` VARCHAR(500) NOT NULL DEFAULT '',
  `fields_json` MEDIUMTEXT NOT NULL,
  `sort_order` INT NOT NULL,
  `created_at` DATETIME(6) NOT NULL,
  `updated_at` DATETIME(6) NOT NULL,
  PRIMARY KEY (`id`),
  CONSTRAINT `ck_event_types_1` CHECK (color_slot IN ('tag-1', 'tag-2', 'tag-3', 'tag-4', 'tag-5', 'tag-6', 'tag-7', 'tag-8')),
  CONSTRAINT `ck_event_types_2` CHECK (style IN ('default', 'banner', 'outline', 'badge')),
  CONSTRAINT `ck_event_types_3` CHECK (default_duration_minutes BETWEEN 0 AND 1440)
);

CREATE UNIQUE INDEX `ux_event_types_name` ON `event_types` (`name`);

ALTER TABLE `events` ADD COLUMN `event_type_id` VARCHAR(36) NULL;

ALTER TABLE `events` ADD COLUMN `fields_json` MEDIUMTEXT NOT NULL DEFAULT ('{}');

ALTER TABLE `events` ADD CONSTRAINT `fk_events_event_type_id` FOREIGN KEY (`event_type_id`) REFERENCES `event_types` (`id`);

CREATE INDEX `ix_events_type` ON `events` (`event_type_id`);

-- An occurrence can have its own type and field values (a movie release in a
-- series of premieres). type_overridden says whether event_type_id (NULL =
-- generic) and fields_json replace the master's.
ALTER TABLE `event_overrides` ADD COLUMN `type_overridden` BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE `event_overrides` ADD COLUMN `event_type_id` VARCHAR(36) NULL;

ALTER TABLE `event_overrides` ADD COLUMN `fields_json` MEDIUMTEXT NOT NULL DEFAULT ('{}');

ALTER TABLE `event_overrides` ADD CONSTRAINT `fk_event_overrides_event_type_id` FOREIGN KEY (`event_type_id`) REFERENCES `event_types` (`id`);

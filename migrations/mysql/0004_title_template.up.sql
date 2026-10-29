-- Event types can generate event titles from their field values.

ALTER TABLE `event_types` ADD COLUMN `title_template` VARCHAR(200) NOT NULL DEFAULT '';

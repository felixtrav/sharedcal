-- People have a first and last name; `name` stays as the combined display name.

ALTER TABLE `people` MODIFY COLUMN `name` VARCHAR(128) NOT NULL;

ALTER TABLE `people` ADD COLUMN `first_name` VARCHAR(60) NOT NULL DEFAULT '';

ALTER TABLE `people` ADD COLUMN `last_name` VARCHAR(60) NOT NULL DEFAULT '';

UPDATE `people` SET `first_name` = `name`;

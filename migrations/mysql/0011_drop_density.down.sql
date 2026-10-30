ALTER TABLE `user_preferences` ADD COLUMN `density` VARCHAR(16) NOT NULL DEFAULT 'comfortable';
ALTER TABLE `user_preferences` ADD CONSTRAINT `ck_user_preferences_4` CHECK (density IN ('comfortable', 'compact'));

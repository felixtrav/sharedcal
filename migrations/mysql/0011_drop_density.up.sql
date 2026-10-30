-- Drop the density preference, which only nudged calendar font size and time-grid row height.

ALTER TABLE `user_preferences` DROP CONSTRAINT `ck_user_preferences_4`;
ALTER TABLE `user_preferences` DROP COLUMN `density`;

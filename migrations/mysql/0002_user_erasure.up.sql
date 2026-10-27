-- Marks users whose personal data an admin has erased.

ALTER TABLE `users` ADD COLUMN `erased_at` DATETIME(6);

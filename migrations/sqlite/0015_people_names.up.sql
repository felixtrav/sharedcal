-- People have a first and last name; "name" stays as the combined display name.

ALTER TABLE "people" ADD COLUMN "first_name" TEXT NOT NULL DEFAULT '';

ALTER TABLE "people" ADD COLUMN "last_name" TEXT NOT NULL DEFAULT '';

UPDATE "people" SET "first_name" = "name";

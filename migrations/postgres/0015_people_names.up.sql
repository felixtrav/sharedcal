-- People have a first and last name; "name" stays as the combined display name.

ALTER TABLE "people" ALTER COLUMN "name" TYPE VARCHAR(128);

ALTER TABLE "people" ADD COLUMN "first_name" VARCHAR(60) NOT NULL DEFAULT '';

ALTER TABLE "people" ADD COLUMN "last_name" VARCHAR(60) NOT NULL DEFAULT '';

UPDATE "people" SET "first_name" = "name";

-- People who can be mentioned in events without having an account. An admin binds a person to a user when they join.

CREATE TABLE "people" (
  "id" VARCHAR(36) NOT NULL,
  "name" VARCHAR(64) NOT NULL,
  "status" VARCHAR(16) NOT NULL,
  "created_by_id" VARCHAR(36),
  "bound_user_id" VARCHAR(36),
  "bound_at" TIMESTAMPTZ,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_people_1" CHECK (status IN ('pending', 'active')),
  CONSTRAINT "fk_people_created_by_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") ON DELETE SET NULL,
  CONSTRAINT "fk_people_bound_user_id" FOREIGN KEY ("bound_user_id") REFERENCES "users" ("id") ON DELETE SET NULL
);

CREATE INDEX "ix_people_status" ON "people" ("status");

CREATE INDEX "ix_people_bound_user" ON "people" ("bound_user_id");

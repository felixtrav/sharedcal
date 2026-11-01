-- People who can be mentioned in events without having an account. An admin binds a person to a user when they join.

CREATE TABLE "people" (
  "id" TEXT NOT NULL,
  "name" TEXT NOT NULL,
  "status" TEXT NOT NULL,
  "created_by_id" TEXT,
  "bound_user_id" TEXT,
  "bound_at" DATETIME,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_people_1" CHECK (status IN ('pending', 'active')),
  CONSTRAINT "fk_people_created_by_id" FOREIGN KEY ("created_by_id") REFERENCES "users" ("id") ON DELETE SET NULL,
  CONSTRAINT "fk_people_bound_user_id" FOREIGN KEY ("bound_user_id") REFERENCES "users" ("id") ON DELETE SET NULL
);

CREATE INDEX "ix_people_status" ON "people" ("status");

CREATE INDEX "ix_people_bound_user" ON "people" ("bound_user_id");

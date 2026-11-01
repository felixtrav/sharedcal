-- Requests from non-admins for a new tag or event type, kept until an admin approves or rejects them.

CREATE TABLE "catalog_requests" (
  "id" VARCHAR(36) NOT NULL,
  "kind" VARCHAR(16) NOT NULL,
  "name" VARCHAR(64) NOT NULL,
  "icon" VARCHAR(32) NOT NULL,
  "color_slot" VARCHAR(16) NOT NULL,
  "other_text" TEXT NOT NULL,
  "author_id" VARCHAR(36) NOT NULL,
  "status" VARCHAR(16) NOT NULL,
  "reviewer_id" VARCHAR(36),
  "review_note" TEXT NOT NULL,
  "created_item_id" VARCHAR(36),
  "resolved_at" TIMESTAMPTZ,
  "created_at" TIMESTAMPTZ NOT NULL,
  "updated_at" TIMESTAMPTZ NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_catalog_requests_1" CHECK (kind IN ('tag', 'event_type')),
  CONSTRAINT "ck_catalog_requests_2" CHECK (status IN ('open', 'approved', 'rejected', 'withdrawn')),
  CONSTRAINT "fk_catalog_requests_author_id" FOREIGN KEY ("author_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE INDEX "ix_catalog_requests_status" ON "catalog_requests" ("status");

CREATE INDEX "ix_catalog_requests_author" ON "catalog_requests" ("author_id");

-- Requests from non-admins for a new tag or event type, kept until an admin approves or rejects them.

CREATE TABLE "catalog_requests" (
  "id" TEXT NOT NULL,
  "kind" TEXT NOT NULL,
  "name" TEXT NOT NULL,
  "icon" TEXT NOT NULL,
  "color_slot" TEXT NOT NULL,
  "other_text" TEXT NOT NULL,
  "author_id" TEXT NOT NULL,
  "status" TEXT NOT NULL,
  "reviewer_id" TEXT,
  "review_note" TEXT NOT NULL,
  "created_item_id" TEXT,
  "resolved_at" DATETIME,
  "created_at" DATETIME NOT NULL,
  "updated_at" DATETIME NOT NULL,
  PRIMARY KEY ("id"),
  CONSTRAINT "ck_catalog_requests_1" CHECK (kind IN ('tag', 'event_type')),
  CONSTRAINT "ck_catalog_requests_2" CHECK (status IN ('open', 'approved', 'rejected', 'withdrawn')),
  CONSTRAINT "fk_catalog_requests_author_id" FOREIGN KEY ("author_id") REFERENCES "users" ("id") ON DELETE CASCADE
);

CREATE INDEX "ix_catalog_requests_status" ON "catalog_requests" ("status");

CREATE INDEX "ix_catalog_requests_author" ON "catalog_requests" ("author_id");

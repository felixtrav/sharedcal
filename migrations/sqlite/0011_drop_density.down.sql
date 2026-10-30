ALTER TABLE "user_preferences" ADD COLUMN "density" TEXT NOT NULL DEFAULT 'comfortable' CHECK (density IN ('comfortable', 'compact'));

-- 0002_covers down: drop the covers table and re-add the cover_path columns as the
-- original nullable TEXT columns (matching their 0001 definitions).
DROP TABLE IF EXISTS covers;
ALTER TABLE series ADD COLUMN cover_path TEXT;
ALTER TABLE issues ADD COLUMN cover_path TEXT;

-- 0003_indexers (down): drop the index then the table.
DROP INDEX IF EXISTS idx_indexers_enabled;
DROP TABLE IF EXISTS indexers;

-- 0003_drop_handrolled_jobs: the hand-owned jobs/job_history tables (and their
-- idx_jobs_state index) defined in 0001 are superseded by the River job engine
-- (ADR 0006), which owns its own schema via its own migrator — NOT golang-migrate.
-- This migration only DROPS the now-unused hand-rolled tables; it never creates
-- River's tables. job_history is dropped first because it carries an FK referencing
-- jobs(id). metadata_cache is untouched (it stays per CONTEXT D-02).
DROP INDEX idx_jobs_state;
DROP TABLE job_history;
DROP TABLE jobs;

-- 0001_init down: drop all tables in reverse dependency order.
DROP TABLE IF EXISTS issue_events;
DROP TABLE IF EXISTS user_config;
DROP TABLE IF EXISTS metadata_cache;
DROP TABLE IF EXISTS blacklists;
DROP TABLE IF EXISTS download_history;
DROP TABLE IF EXISTS downloads;
DROP TABLE IF EXISTS story_arc_issues;
DROP TABLE IF EXISTS story_arcs;
DROP TABLE IF EXISTS covers;
DROP TABLE IF EXISTS issues;
DROP TABLE IF EXISTS series;
DROP TABLE IF EXISTS publishers;

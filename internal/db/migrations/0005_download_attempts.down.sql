-- 0005_download_attempts (down): drop the download_attempts counter (D-13).
ALTER TABLE issues DROP COLUMN download_attempts;

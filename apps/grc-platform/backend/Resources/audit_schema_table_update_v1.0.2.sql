USE grc_platform;

-- User-facing audit name, set when an Active audit is renamed. `name` stays the
-- audit's evidence storage folder and is never changed by a rename; NULL here
-- means "show `name`", so existing audits are unaffected.
-- Deploy order: run this first, then the Compliance Entity, then the backend.
ALTER TABLE audit
  ADD COLUMN display_name VARCHAR(200) NULL AFTER name;

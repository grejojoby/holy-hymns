ALTER TABLE categories ADD COLUMN version integer NOT NULL DEFAULT 1;
ALTER TABLE app_config ADD COLUMN version integer NOT NULL DEFAULT 1;

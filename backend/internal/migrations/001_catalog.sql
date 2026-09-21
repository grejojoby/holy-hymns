CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE TABLE categories (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, name_malayalam text NOT NULL DEFAULT '',
 kind text NOT NULL CHECK(kind IN ('purpose','occasion','theme')), position integer NOT NULL DEFAULT 0,
 UNIQUE(name,kind)
);
CREATE TABLE songs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), draft jsonb NOT NULL,
 published jsonb, version integer NOT NULL DEFAULT 1, published_version integer,
 source_id text UNIQUE, source_hash text, source_html text, imported_version integer,
 search_text text NOT NULL DEFAULT '', search_title text NOT NULL DEFAULT '',
 search_document tsvector NOT NULL DEFAULT ''::tsvector,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), published_at timestamptz
);
CREATE INDEX song_search_document ON songs USING gin(search_document) WHERE published IS NOT NULL;
CREATE INDEX song_search_trigram ON songs USING gin(search_title gin_trgm_ops) WHERE published IS NOT NULL;
CREATE INDEX song_published_at ON songs(published_at DESC) WHERE published IS NOT NULL;
CREATE INDEX song_published_categories ON songs USING gin((published->'categoryIds')) WHERE published IS NOT NULL;
CREATE TABLE song_revisions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), song_id uuid NOT NULL REFERENCES songs(id) ON DELETE CASCADE,
 version integer NOT NULL, content jsonb NOT NULL, actor_id uuid, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(song_id,version)
);
CREATE TABLE app_config (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 content jsonb NOT NULL DEFAULT '{"appName":"Holy Hymns","announcement":"","aboutText":"Malayalam Christian lyrics, close at hand.","supportEmail":""}'
);
INSERT INTO app_config(singleton) VALUES(true);
CREATE TABLE content_state(singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton), revision bigint NOT NULL DEFAULT 0);
INSERT INTO content_state(singleton) VALUES(true);
CREATE TABLE content_changes(revision bigint PRIMARY KEY, kind text NOT NULL, song_id uuid, created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE favorites(user_id uuid NOT NULL,song_id uuid NOT NULL REFERENCES songs(id) ON DELETE CASCADE,created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(user_id,song_id));
CREATE TABLE admin_audit(id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,actor_id uuid,action text NOT NULL,target_id text NOT NULL,created_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE analytics_daily(day date NOT NULL DEFAULT CURRENT_DATE,event text NOT NULL,song_id text NOT NULL DEFAULT '',category_id text NOT NULL DEFAULT '',count bigint NOT NULL DEFAULT 0,PRIMARY KEY(day,event,song_id,category_id));
CREATE TABLE import_runs(id uuid PRIMARY KEY DEFAULT gen_random_uuid(),summary jsonb NOT NULL,created_at timestamptz NOT NULL DEFAULT now());

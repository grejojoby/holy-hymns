-- Keep original import metadata independently of reader-facing categories.
ALTER TABLE songs ADD COLUMN source_labels text[] NOT NULL DEFAULT '{}';

-- The first imported revision records exactly which categories came from labels.
-- Editorial categories added later are not part of this replacement.
CREATE TEMP TABLE legacy_labels ON COMMIT DROP AS
SELECT DISTINCT c.id, c.name
FROM categories c
JOIN song_revisions r ON (r.content->'categoryIds') ? c.id::text
JOIN songs s ON s.id=r.song_id
WHERE s.source_id IS NOT NULL AND r.version=1 AND c.kind='theme';

UPDATE songs s SET source_labels=ARRAY(
 SELECT DISTINCT l.name FROM song_revisions r
 JOIN legacy_labels l ON (r.content->'categoryIds') ? l.id::text
 WHERE r.song_id=s.id AND r.version=1 ORDER BY l.name
) WHERE s.source_id IS NOT NULL;

CREATE TABLE category_import_labels (
 label text PRIMARY KEY,
 category_id uuid NOT NULL REFERENCES categories(id) ON DELETE CASCADE
);

INSERT INTO categories(name,kind,position) VALUES
 ('Entrance Songs','purpose',10),
 ('Communion Songs','purpose',20),
 ('Adoration Songs','purpose',30),
 ('Recession Songs','purpose',40),
 ('Mother Mary Songs','theme',50),
 ('Liturgy','purpose',60),
 ('Offertory Songs','purpose',70),
 ('Thanksgiving Songs','purpose',80),
 ('Holy Spirit Songs','theme',90),
 ('Prayer Songs','purpose',100),
 ('Repentance Songs','purpose',110),
 ('Christmas Songs','occasion',120),
 ('Easter Songs','occasion',130),
 ('Holy Week Songs','occasion',140),
 ('Way of the Cross','purpose',150),
 ('Wedding Songs','occasion',160),
 ('Ordination Songs','occasion',170),
 ('First Holy Communion Songs','occasion',180)
ON CONFLICT(name,kind) DO UPDATE SET position=excluded.position,version=categories.version+1;

-- Canonical names are also valid import labels. Aliases are deliberately exact:
-- language, artist, album, and generic Christian labels imply no liturgical use.
INSERT INTO category_import_labels(label,category_id)
SELECT lower(name),id FROM categories WHERE (name,kind) IN (
 ('Entrance Songs','purpose'),('Communion Songs','purpose'),
 ('Adoration Songs','purpose'),('Recession Songs','purpose'),
 ('Mother Mary Songs','theme'),('Liturgy','purpose'),
 ('Offertory Songs','purpose'),('Thanksgiving Songs','purpose'),
 ('Holy Spirit Songs','theme'),('Prayer Songs','purpose'),
 ('Repentance Songs','purpose'),('Christmas Songs','occasion'),
 ('Easter Songs','occasion'),('Holy Week Songs','occasion'),
 ('Way of the Cross','purpose'),('Wedding Songs','occasion'),
 ('Ordination Songs','occasion'),('First Holy Communion Songs','occasion')
);
INSERT INTO category_import_labels(label,category_id)
SELECT a.label,c.id FROM (VALUES
 ('entrance','Entrance Songs'),('entrance (intro) songs','Entrance Songs'),
 ('communion','Communion Songs'),('holy communion','Communion Songs'),
 ('holy communion songs','Communion Songs'),
 ('adoration','Adoration Songs'),('aaradhana geethangal','Adoration Songs'),
 ('kurbana aaradhana geethangal','Adoration Songs'),
 ('recession','Recession Songs'),('recessional songs','Recession Songs'),('end songs','Recession Songs'),
 ('mother mary','Mother Mary Songs'),('marian','Mother Mary Songs'),
 ('holy qurbana songs','Liturgy'),
 ('offertory','Offertory Songs'),('thanksgiving','Thanksgiving Songs'),
 ('holy spirit','Holy Spirit Songs'),('prayer','Prayer Songs'),
 ('repentance','Repentance Songs'),
 ('christmas','Christmas Songs'),('carol','Christmas Songs'),('christmas carol songs','Christmas Songs'),
 ('easter','Easter Songs'),('easter sunday songs','Easter Songs'),
 ('holy week','Holy Week Songs'),('marriage','Wedding Songs'),
 ('wedding','Wedding Songs'),('ordination','Ordination Songs'),
 ('first holy communion','First Holy Communion Songs')
) AS a(label,name)
JOIN categories c ON c.name=a.name
JOIN category_import_labels canonical ON canonical.category_id=c.id AND canonical.label=lower(c.name);

CREATE TEMP TABLE category_replacements ON COMMIT DROP AS
SELECT l.id::text AS old_id, a.category_id::text AS new_id
FROM legacy_labels l LEFT JOIN category_import_labels a ON a.label=lower(trim(l.name));

-- Remap draft and published snapshots independently, preserving unpublished edits.
CREATE OR REPLACE FUNCTION pg_temp.worship_categories(content jsonb) RETURNS jsonb
LANGUAGE sql AS $$
 SELECT CASE WHEN content IS NULL THEN NULL ELSE jsonb_set(content,'{categoryIds}',
  COALESCE((SELECT jsonb_agg(id ORDER BY id) FROM (
   SELECT DISTINCT CASE WHEN r.old_id IS NULL THEN value ELSE r.new_id END AS id
   FROM jsonb_array_elements_text(COALESCE(content->'categoryIds','[]'::jsonb))
   LEFT JOIN category_replacements r ON r.old_id=value
  ) ids WHERE id IS NOT NULL),'[]'::jsonb)) END
$$;

WITH changed AS (
 UPDATE songs SET draft=pg_temp.worship_categories(draft),
  published=pg_temp.worship_categories(published),version=version+1,updated_at=now()
 WHERE draft IS DISTINCT FROM pg_temp.worship_categories(draft)
    OR published IS DISTINCT FROM pg_temp.worship_categories(published)
 RETURNING id,version,draft
)
INSERT INTO song_revisions(song_id,version,content) SELECT id,version,draft FROM changed;

-- Historical revisions retain the original label assignments for review.
DELETE FROM categories c USING legacy_labels l
WHERE c.id=l.id AND NOT EXISTS(SELECT 1 FROM category_import_labels a WHERE a.category_id=c.id);

WITH revision AS (UPDATE content_state SET revision=revision+1 RETURNING revision)
INSERT INTO content_changes(revision,kind) SELECT revision,'categories' FROM revision;

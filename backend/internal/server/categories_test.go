package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"holyhymns/internal/importer"
	"holyhymns/internal/migrations"
)

func TestWorshipCategoriesAndImportLabels(t *testing.T) {
	s, db, _ := integration(t)
	ctx := context.Background()
	rr := request(s, "GET", "/v1/categories", "", nil)
	for _, name := range []string{"Entrance Songs", "Communion Songs", "Adoration Songs", "Recession Songs", "Mother Mary Songs", "Liturgy"} {
		if rr.Code != 200 || !strings.Contains(rr.Body.String(), name) {
			t.Fatalf("missing %s: %s", name, rr.Body)
		}
	}
	entry := importer.Entry{SourceID: "category-test", Hash: "test", Title: "Test hymn", LyricsManglish: "Test lyrics", Labels: []string{"Malayalam Christian", "Kester", "Mother Mary", " Marian ", "Holy Communion"}}
	report, err := ImportEntries(ctx, db, []importer.Entry{entry}, false)
	if err != nil || report.Created != 1 {
		t.Fatal(report, err)
	}
	var raw []byte
	var labels []string
	if err = db.QueryRow(ctx, `SELECT draft,source_labels FROM songs WHERE source_id=$1`, entry.SourceID).Scan(&raw, &labels); err != nil {
		t.Fatal(err)
	}
	var song Song
	if err = json.Unmarshal(raw, &song); err != nil {
		t.Fatal(err)
	}
	if len(song.CategoryIDs) != 2 || len(labels) != 5 {
		t.Fatal("aliases not deduplicated or original labels lost", song.CategoryIDs, labels)
	}
	var count int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM categories WHERE name IN ('Kester','Malayalam Christian')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("source labels became categories", count, err)
	}
}

func TestWorshipCategoryMigrationPreservesDraftsAndMetadata(t *testing.T) {
	s, db, _ := integration(t)
	ctx := context.Background()
	// Recreate the pre-migration taxonomy in this disposable test schema.
	_, err := db.Exec(ctx, `DROP TABLE category_import_labels;
 ALTER TABLE songs DROP COLUMN source_labels;
 DELETE FROM categories;
 DELETE FROM schema_migrations WHERE name='005_worship_categories.sql';
 INSERT INTO categories(id,name,kind) VALUES
 ('00000000-0000-4000-8000-000000000001','Mother Mary','theme'),
 ('00000000-0000-4000-8000-000000000002','Marian','theme'),
 ('00000000-0000-4000-8000-000000000003','Kester','theme'),
 ('00000000-0000-4000-8000-000000000004','Custom editorial category','occasion');
 INSERT INTO songs(id,source_id,imported_version,draft,published,published_version,published_at) VALUES
 ('00000000-0000-4000-8000-000000000005','legacy-source',1,
 '{"title":"Unpublished edit","categoryIds":["00000000-0000-4000-8000-000000000001","00000000-0000-4000-8000-000000000002","00000000-0000-4000-8000-000000000003","00000000-0000-4000-8000-000000000004"]}',
 '{"title":"Published hymn","categoryIds":["00000000-0000-4000-8000-000000000001","00000000-0000-4000-8000-000000000003"]}',1,now());
 INSERT INTO song_revisions(song_id,version,content) SELECT id,1,draft FROM songs;`)
	if err != nil {
		t.Fatal(err)
	}
	if err = migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	var draft, published []byte
	var labels []string
	var version, publishedVersion int
	if err = db.QueryRow(ctx, `SELECT draft,published,source_labels,version,published_version FROM songs WHERE source_id='legacy-source'`).Scan(&draft, &published, &labels, &version, &publishedVersion); err != nil {
		t.Fatal(err)
	}
	var d, p Song
	if err = json.Unmarshal(draft, &d); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(published, &p); err != nil {
		t.Fatal(err)
	}
	if d.Title != "Unpublished edit" || p.Title != "Published hymn" || len(d.CategoryIDs) != 2 || len(p.CategoryIDs) != 1 || len(labels) != 3 || version != 2 || publishedVersion != 1 {
		t.Fatal(string(draft), string(published), labels, version, publishedVersion)
	}
	rr := request(s, "GET", "/v1/categories", "", nil)
	if strings.Contains(rr.Body.String(), "Kester") || strings.Contains(rr.Body.String(), `"Marian"`) {
		t.Fatal(rr.Body)
	}
	rr = request(s, "GET", "/v1/songs?category="+p.CategoryIDs[0], "", nil)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "Published hymn") {
		t.Fatal("remapped filter failed", rr.Code, rr.Body)
	}
	// Applying migrations again must not change content or optimistic versions.
	if err = migrations.Apply(ctx, db); err != nil {
		t.Fatal(err)
	}
	var after int
	if err = db.QueryRow(ctx, `SELECT version FROM songs WHERE source_id='legacy-source'`).Scan(&after); err != nil || after != version {
		t.Fatal(after, err)
	}
}

package importer

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func fingerprintFixture() Entry {
	return Entry{
		SourceID:  "tag:blogger.com,1999:blog-6936100729546217639.post-42",
		SourceURL: "https://grejolyrics.blogspot.com/2020/01/karthave.html",
		Title:     "Karthave", TitleMalayalam: "കർത്താവേ",
		LyricsMalayalam: "കർത്താവേ\u200d\n\nനാഥാ (2)", LyricsManglish: "Karthave\n\nNadha (2)",
		Credits: "Lyrics: A", Labels: []string{"Mass", "കൃപ"},
		Links:       []Link{{Label: "Watch", URL: "https://youtu.be/example"}},
		ReviewNotes: []string{"Review words", "Check credits"}, Hash: strings.Repeat("a", 64),
	}
}

func TestContentFingerprintCrossLanguageVector(t *testing.T) {
	entry := fingerprintFixture()
	// Independently calculated with Python hashlib over UTF-8 byte-length fields.
	const want = "fdaafbcf1e20d1866f2763db9b640efef1a51fdbc7551a20c062d6f846aaa6cd"
	if got := ContentFingerprint(entry); got != want {
		t.Fatalf("UTF-8 fingerprint contract changed: got %s, want %s", got, want)
	}
	entry.Hash = "different source HTML hash"
	entry.RawHTML = "not included in editable content fingerprint"
	if ContentFingerprint(entry) != want {
		t.Fatal("source HTML unexpectedly changed the content fingerprint")
	}
	entry.LyricsMalayalam += "\n"
	if ContentFingerprint(entry) == want {
		t.Fatal("a changed lyric byte did not change the fingerprint")
	}
}

func TestExportBundlePreservesUnicodeStanzasAndAvailableScripts(t *testing.T) {
	first := fingerprintFixture()
	second := Entry{SourceID: "tag:blogger.com,1999:blog-6936100729546217639.post-43", Title: "കൃപ", LyricsMalayalam: "കൃപ\n\nനൽകണമേ"}
	third := Entry{SourceID: "tag:blogger.com,1999:blog-6936100729546217639.post-44", Title: "Karthave", LyricsManglish: "Karthave kaniyaname"}
	media := Entry{SourceID: "tag:blogger.com,1999:blog-6936100729546217639.post-45", Title: "Karaoke", Links: []Link{{Label: "Listen", URL: "https://youtu.be/example"}}}
	output := filepath.Join(t.TempDir(), "collection")
	if err := os.Mkdir(output, 0755); err != nil {
		t.Fatal(err)
	}
	summary, err := ExportBundle([]Entry{first, second, third, media}, output)
	if err != nil {
		t.Fatal(err)
	}
	if summary != (BundleSummary{SourceCount: 4, SongCount: 3, SkippedCount: 1, MalayalamCount: 2, ManglishCount: 2, BothScriptsCount: 1}) {
		t.Fatalf("incorrect bundle counts: %+v", summary)
	}
	manifest := readTestManifest(t, output)
	if manifest.SchemaVersion != 1 || manifest.Source != BundleSource || len(manifest.Songs) != 3 || len(manifest.Skipped) != 1 {
		t.Fatalf("incorrect manifest: %+v", manifest)
	}
	if manifest.Songs[0].SourceHash != first.Hash || manifest.Songs[0].ContentFingerprint != ContentFingerprint(first) || manifest.Songs[0].Links[0].Label != "Watch" {
		t.Fatal("source metadata or fingerprint was not preserved")
	}
	if manifest.Songs[1].MalayalamFile != "malayalam/hymn-43.txt" || manifest.Songs[1].ManglishFile != "" || manifest.Songs[2].MalayalamFile != "" {
		t.Fatal("missing scripts should have no file path", manifest.Songs)
	}
	for index, entry := range []Entry{first, second, third} {
		song := manifest.Songs[index]
		for _, script := range []struct{ path, text string }{{song.MalayalamFile, entry.LyricsMalayalam}, {song.ManglishFile, entry.LyricsManglish}} {
			if script.path == "" {
				continue
			}
			if !regexp.MustCompile(`^(malayalam|manglish)/[a-z0-9-]+\.txt$`).MatchString(script.path) {
				t.Errorf("unsafe/non-ASCII file path: %q", script.path)
			}
			data, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(script.path)))
			if err != nil || string(data) != script.text {
				t.Fatalf("lyric bytes/stanzas changed: %q, %v", data, err)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(output, "manglish", "hymn-43.txt")); !os.IsNotExist(err) {
		t.Fatal("an absent script was written")
	}
	if manifest.Skipped[0].Reason != "no_lyrics" || manifest.Skipped[0].MalayalamFile != "" || manifest.Skipped[0].ManglishFile != "" {
		t.Fatal("media-only entry became a lyric file")
	}
	data, err := os.ReadFile(filepath.Join(output, "manifest.json"))
	if err != nil || strings.Contains(string(data), ": null") || strings.Contains(string(data), `"SourceID"`) {
		t.Fatal("metadata requires camelCase names and non-null arrays", err)
	}
	index, err := os.Open(filepath.Join(output, "index.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer index.Close()
	rows, err := csv.NewReader(index).ReadAll()
	if err != nil || len(rows) != 5 || rows[1][2] != first.TitleMalayalam || rows[4][0] != "no_lyrics" {
		t.Fatal("review index lost a title or skip", rows, err)
	}
}

func TestExportBundleDuplicatesDoNotCollide(t *testing.T) {
	first := fingerprintFixture()
	second := first
	second.SourceID = strings.Replace(first.SourceID, "post-42", "post-43", 1)
	output := filepath.Join(t.TempDir(), "forward")
	summary, err := ExportBundle([]Entry{first, second, first}, output)
	if err != nil {
		t.Fatal(err)
	}
	manifest := readTestManifest(t, output)
	if summary.SongCount != 2 || summary.DuplicateSourceCount != 1 || summary.SkippedCount != 1 || manifest.Skipped[0].Reason != "duplicate_source_id" {
		t.Fatal("duplicate source was not explicitly skipped", summary)
	}
	if manifest.Songs[0].ManglishFile == manifest.Songs[1].ManglishFile {
		t.Fatal("distinct posts with matching titles share a file")
	}
	reversed := filepath.Join(t.TempDir(), "reverse")
	if _, err := ExportBundle([]Entry{second, first}, reversed); err != nil {
		t.Fatal(err)
	}
	backwards := readTestManifest(t, reversed)
	if backwards.Songs[1].ManglishFile != manifest.Songs[0].ManglishFile {
		t.Fatal("filename depends on feed order")
	}
}

func TestExportBundleRefusesToOverwriteEditedOutput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "collection")
	if _, err := ExportBundle([]Entry{fingerprintFixture()}, output); err != nil {
		t.Fatal(err)
	}
	manifest := readTestManifest(t, output)
	file := filepath.Join(output, filepath.FromSlash(manifest.Songs[0].MalayalamFile))
	const edited = "കർത്താവേ\nUser's corrected lyrics\n"
	if err := os.WriteFile(file, []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportBundle([]Entry{fingerprintFixture()}, output); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatal("nonempty edited bundle accepted", err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != edited {
		t.Fatal("edited lyrics were overwritten", err)
	}
	linked := filepath.Join(t.TempDir(), "symlink")
	if err := os.Symlink(output, linked); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportBundle(nil, linked); err == nil {
		t.Fatal("symbolic-link destination accepted")
	}
	if _, err := ExportBundle(nil, file); err == nil {
		t.Fatal("file destination accepted")
	}
}

func TestExportBundleFailureLeavesNoPartialOutput(t *testing.T) {
	parent := t.TempDir()
	output := filepath.Join(parent, "collection")
	if _, err := ExportBundle([]Entry{fingerprintFixture(), {Title: "Missing identity"}}, output); err == nil {
		t.Fatal("missing source identity accepted")
	}
	children, err := os.ReadDir(parent)
	if err != nil || len(children) != 0 {
		t.Fatal("partial files were left after failure", children, err)
	}
}

func TestBundleCSVNeutralizesFormulaTitles(t *testing.T) {
	entry := fingerprintFixture()
	entry.Title = "=SUM(1,2)"
	output := filepath.Join(t.TempDir(), "collection")
	if _, err := ExportBundle([]Entry{entry}, output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(output, "index.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil || rows[1][1] != "'=SUM(1,2)" {
		t.Fatal("unsafe spreadsheet formula title", rows, err)
	}
	if readTestManifest(t, output).Songs[0].Title != entry.Title {
		t.Fatal("formula escaping unexpectedly changed authoritative metadata")
	}
}

func TestExportBundleKeepsReviewMaterialSeparateFromLyrics(t *testing.T) {
	entry := fingerprintFixture()
	entry.RawHTML = `<div>കർത്താവേ</div><p>English review material</p>`
	entry.ReviewText = "English review material\n\nCheck original source"
	entry.ReviewReason = "non_manglish_latin"
	entry.LyricsManglish = ""
	skipped := Entry{
		SourceID: "tag:blogger.com,1999:blog-6936100729546217639.post-43",
		Title:    "Collection index", RawHTML: `<a href="/song">Song list</a>`,
		ReviewReason: "index_only", ReviewText: "Song list",
	}
	output := filepath.Join(t.TempDir(), "collection")
	summary, err := ExportBundle([]Entry{entry, skipped}, output)
	if err != nil {
		t.Fatal(err)
	}
	manifest := readTestManifest(t, output)
	if summary.ReviewCount != 2 || summary.SongCount != 1 || summary.SkippedCount != 1 || summary.ManglishCount != 0 {
		t.Fatal("review text became uploadable lyrics", summary)
	}
	if manifest.Skipped[0].Reason != "index_only" {
		t.Fatal("skip reason should retain the classification reason")
	}
	for index, song := range []BundleSong{manifest.Songs[0], manifest.Skipped[0].BundleSong} {
		original := []Entry{entry, skipped}[index]
		if song.ReviewReason != original.ReviewReason || !strings.HasPrefix(song.SourceFile, "source/") || !strings.HasPrefix(song.ReviewFile, "review/") {
			t.Fatal("review/source metadata missing", song)
		}
		for path, want := range map[string]string{song.SourceFile: original.RawHTML, song.ReviewFile: original.ReviewText} {
			data, err := os.ReadFile(filepath.Join(output, filepath.FromSlash(path)))
			if err != nil || string(data) != want {
				t.Fatalf("review/original bytes changed in %s: %q, %v", path, data, err)
			}
		}
	}
	baseline := fingerprintFixture()
	changed := baseline
	changed.ReviewReason, changed.ReviewText, changed.RawHTML = "review", "unclassified text", "<p>source</p>"
	if ContentFingerprint(baseline) != ContentFingerprint(changed) {
		t.Fatal("review-only material changed uploadable content fingerprint")
	}
}

func TestExportBundleRejectsConflictingDuplicateSource(t *testing.T) {
	entry := fingerprintFixture()
	conflicting := entry
	conflicting.RawHTML = "different version of the same post"
	output := filepath.Join(t.TempDir(), "collection")
	if _, err := ExportBundle([]Entry{entry, conflicting}, output); err == nil || !strings.Contains(err.Error(), "conflicting duplicate") {
		t.Fatal("conflicting duplicate should require explicit selection", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("conflicting duplicate left a partial collection")
	}
}

func readTestManifest(t *testing.T, directory string) BundleManifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest BundleManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest
}

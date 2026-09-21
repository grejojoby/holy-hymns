package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"holyhymns/internal/importer"
)

type ImportReport struct {
	Total     int      `json:"total"`
	Created   int      `json:"created"`
	Unchanged int      `json:"unchanged"`
	Conflicts int      `json:"conflicts"`
	Skipped   int      `json:"skipped"`
	Review    []string `json:"review"`
}

// ImportEntries never publishes or overwrites an existing song. Changed sources
// are review conflicts. A complete run and its report commit together, so retries
// after a failed transaction cannot leave partially imported data.
func ImportEntries(ctx context.Context, db *pgxpool.Pool, entries []importer.Entry, dry bool) (ImportReport, error) {
	return importEntries(ctx, db, entries, dry, nil)
}

func importEntries(ctx context.Context, db *pgxpool.Pool, entries []importer.Entry, dry bool, recordAudit func(pgx.Tx) error) (ImportReport, error) {
	report := ImportReport{Total: len(entries), Review: []string{}}
	var tx pgx.Tx
	var err error
	if dry {
		tx, err = db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly, IsoLevel: pgx.RepeatableRead})
	} else {
		tx, err = beginContent(ctx, db)
	}
	if err != nil {
		return report, err
	}
	defer tx.Rollback(ctx)
	for _, entry := range entries {
		if strings.TrimSpace(entry.LyricsMalayalam+entry.LyricsManglish) == "" {
			report.Skipped++
			report.Review = append(report.Review, entry.Title+": no usable lyrics")
			continue
		}
		var hash string
		e := tx.QueryRow(ctx, `SELECT COALESCE(source_hash,'') FROM songs WHERE source_id=$1`, entry.SourceID).Scan(&hash)
		if e == nil {
			if hash == entry.Hash {
				report.Unchanged++
			} else {
				report.Conflicts++
				report.Review = append(report.Review, entry.Title+": source changed; existing song preserved")
			}
			continue
		}
		if e != pgx.ErrNoRows {
			return report, e
		}
		song := importedSong(entry)
		if e = song.validate(false); e != nil {
			report.Skipped++
			report.Review = append(report.Review, entry.Title+": "+e.Error())
			continue
		}
		for _, note := range entry.ReviewNotes {
			report.Review = append(report.Review, entry.Title+": "+note)
		}
		if dry {
			report.Created++
			continue
		}
		for _, label := range entry.Labels {
			var cid string
			e = tx.QueryRow(ctx, `INSERT INTO categories(name,kind) VALUES($1,'theme') ON CONFLICT(name,kind) DO UPDATE SET name=excluded.name RETURNING id::text`, label).Scan(&cid)
			if e != nil {
				break
			}
			song.CategoryIDs = append(song.CategoryIDs, cid)
		}
		if e != nil {
			return report, e
		}
		raw, e := json.Marshal(song)
		if e != nil {
			return report, e
		}
		var sid string
		e = tx.QueryRow(ctx, `INSERT INTO songs(draft,source_id,source_hash,source_html,imported_version) VALUES($1,$2,$3,$4,1) ON CONFLICT(source_id) DO NOTHING RETURNING id::text`, raw, entry.SourceID, entry.Hash, entry.RawHTML).Scan(&sid)
		if e == pgx.ErrNoRows {
			// The shared editorial lock serializes normal import callers. Still
			// recheck the durable hash if an external writer inserted this source.
			if e = tx.QueryRow(ctx, `SELECT COALESCE(source_hash,'') FROM songs WHERE source_id=$1`, entry.SourceID).Scan(&hash); e != nil {
				return report, e
			}
			if hash == entry.Hash {
				report.Unchanged++
			} else {
				report.Conflicts++
				report.Review = append(report.Review, entry.Title+": source changed; existing song preserved")
			}
			continue
		}
		if e == nil {
			_, e = tx.Exec(ctx, `INSERT INTO song_revisions(song_id,version,content) VALUES($1,1,$2)`, sid, raw)
		}
		if e != nil {
			return report, fmt.Errorf("import %s: %w", entry.Title, e)
		}
		report.Created++
	}
	if !dry {
		raw, err := json.Marshal(report)
		if err != nil {
			return report, err
		}
		if _, e := tx.Exec(ctx, `INSERT INTO import_runs(summary) VALUES($1)`, raw); e != nil {
			return report, e
		}
		if recordAudit != nil {
			if err = recordAudit(tx); err != nil {
				return report, err
			}
		}
	}
	return report, tx.Commit(ctx)
}

func importedSong(entry importer.Entry) Song {
	song := Song{Title: entry.Title, TitleMalayalam: entry.TitleMalayalam, LyricsMalayalam: entry.LyricsMalayalam, LyricsManglish: entry.LyricsManglish, Credits: entry.Credits, SourceURL: entry.SourceURL, ReviewNotes: entry.ReviewNotes, Version: 1, Status: "draft"}
	for _, link := range entry.Links {
		song.Links = append(song.Links, Link{Label: link.Label, URL: link.URL})
	}
	song.clean()
	return song
}

type lyricsImportEntry struct {
	SourceID        string   `json:"sourceId"`
	SourceURL       string   `json:"sourceUrl"`
	Hash            string   `json:"hash"`
	Title           string   `json:"title"`
	TitleMalayalam  string   `json:"titleMalayalam"`
	LyricsMalayalam string   `json:"lyricsMalayalam"`
	LyricsManglish  string   `json:"lyricsManglish"`
	Credits         string   `json:"credits"`
	Labels          []string `json:"labels"`
	Links           []Link   `json:"links"`
	ReviewNotes     []string `json:"reviewNotes"`
}

var grejoSourceID = regexp.MustCompile(`^tag:blogger\.com,1999:blog-6936100729546217639\.post-[0-9]{1,32}$`)
var grejoPermalink = regexp.MustCompile(`^/[0-9]{4}/(0[1-9]|1[0-2])/[A-Za-z0-9_-][A-Za-z0-9._~-]*\.html$`)
var importHash = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)

func (input lyricsImportEntry) entry() (importer.Entry, error) {
	var entry importer.Entry
	if !grejoSourceID.MatchString(input.SourceID) {
		return entry, errors.New("sourceId must identify a post on the Grejo Lyrics Blogger site")
	}
	u, err := url.Parse(input.SourceURL)
	if err != nil || len(input.SourceURL) > 2048 || u.Scheme != "https" || u.Host != "grejolyrics.blogspot.com" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawPath != "" || !grejoPermalink.MatchString(u.Path) {
		return entry, errors.New("sourceUrl must be a canonical HTTPS Grejo Lyrics post permalink")
	}
	if !importHash.MatchString(input.Hash) {
		return entry, errors.New("hash must contain 64 hexadecimal characters")
	}
	if utf8.RuneCountInString(input.Credits) > 4000 || len(input.Labels) > 30 || len(input.ReviewNotes) > 30 {
		return entry, errors.New("import metadata exceeds its limit")
	}
	entry = importer.Entry{SourceID: input.SourceID, SourceURL: input.SourceURL, Hash: strings.ToLower(input.Hash), Title: input.Title, TitleMalayalam: input.TitleMalayalam, LyricsMalayalam: input.LyricsMalayalam, LyricsManglish: input.LyricsManglish, Credits: input.Credits}
	for _, value := range []string{entry.Title, entry.TitleMalayalam, entry.LyricsMalayalam, entry.LyricsManglish, entry.Credits} {
		if strings.ContainsRune(value, 0) {
			return entry, errors.New("import text cannot contain null characters")
		}
	}
	seen := map[string]bool{}
	for _, label := range input.Labels {
		label = strings.TrimSpace(label)
		if label == "" || utf8.RuneCountInString(label) > 120 || strings.ContainsAny(label, "\x00\r\n") {
			return entry, errors.New("labels must contain 1–120 characters without line breaks")
		}
		if !seen[label] {
			entry.Labels = append(entry.Labels, label)
			seen[label] = true
		}
	}
	for _, note := range input.ReviewNotes {
		note = strings.TrimSpace(note)
		if note == "" || utf8.RuneCountInString(note) > 1000 || strings.ContainsRune(note, 0) {
			return entry, errors.New("review notes must contain 1–1000 characters")
		}
		entry.ReviewNotes = append(entry.ReviewNotes, note)
	}
	for _, link := range input.Links {
		label := strings.TrimSpace(link.Label)
		if label == "" || utf8.RuneCountInString(label) > 120 || len(link.URL) > 2048 || strings.ContainsRune(label, 0) {
			return entry, errors.New("media labels must contain 1–120 characters and URLs at most 2048 bytes")
		}
		entry.Links = append(entry.Links, importer.Link{Label: label, URL: link.URL})
	}
	song := importedSong(entry)
	if err = song.validate(false); err != nil {
		return entry, err
	}
	return entry, nil
}

func (s *Server) importLyrics(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Entry  *lyricsImportEntry `json:"entry"`
		DryRun bool               `json:"dryRun"`
	}
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		fail(w, 400, "invalid import JSON body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		fail(w, 400, "only one import JSON object is allowed")
		return
	}
	if body.Entry == nil {
		fail(w, 400, "entry is required")
		return
	}
	entry, err := body.Entry.entry()
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	report, err := importEntries(r.Context(), s.DB, []importer.Entry{entry}, body.DryRun, func(tx pgx.Tx) error { return audit(r.Context(), tx, r, "lyrics.import", entry.SourceID) })
	if err != nil {
		dbError(w, err)
		return
	}
	write(w, 200, report)
}

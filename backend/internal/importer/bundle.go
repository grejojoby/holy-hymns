package importer

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const BundleSource = "https://grejolyrics.blogspot.com/"

type BundleLink struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

type BundleSong struct {
	SourceID           string       `json:"sourceId"`
	SourceURL          string       `json:"sourceUrl"`
	SourceHash         string       `json:"sourceHash"`
	Title              string       `json:"title"`
	TitleMalayalam     string       `json:"titleMalayalam"`
	Credits            string       `json:"credits"`
	Labels             []string     `json:"labels"`
	Links              []BundleLink `json:"links"`
	ReviewNotes        []string     `json:"reviewNotes"`
	ContentFingerprint string       `json:"contentFingerprint"`
	MalayalamFile      string       `json:"malayalamFile"`
	ManglishFile       string       `json:"manglishFile"`
	SourceFile         string       `json:"sourceFile"`
	ReviewFile         string       `json:"reviewFile"`
	ReviewReason       string       `json:"reviewReason"`
}

type BundleSkipped struct {
	BundleSong
	Reason string `json:"reason"`
}

type BundleManifest struct {
	SchemaVersion int             `json:"schemaVersion"`
	Source        string          `json:"source"`
	Songs         []BundleSong    `json:"songs"`
	Skipped       []BundleSkipped `json:"skipped"`
}

type BundleSummary struct {
	SourceCount          int `json:"sourceCount"`
	SongCount            int `json:"songCount"`
	SkippedCount         int `json:"skippedCount"`
	MalayalamCount       int `json:"malayalamCount"`
	ManglishCount        int `json:"manglishCount"`
	BothScriptsCount     int `json:"bothScriptsCount"`
	DuplicateSourceCount int `json:"duplicateSourceCount"`
	ReviewCount          int `json:"reviewCount"`
}

// ContentFingerprint is a language-independent hash of editable content and its
// source identity. Each ordered string is UTF-8 encoded and prefixed with its
// decimal byte length and a colon. Neither sourceHash nor file paths are hashed.
func ContentFingerprint(entry Entry) string {
	h := sha256.New()
	add := func(value string) {
		io.WriteString(h, strconv.Itoa(len(value)))
		io.WriteString(h, ":")
		io.WriteString(h, value)
	}
	for _, value := range []string{entry.SourceID, entry.SourceURL, entry.Title, entry.TitleMalayalam, entry.LyricsMalayalam, entry.LyricsManglish, entry.Credits} {
		add(value)
	}
	add(strconv.Itoa(len(entry.Labels)))
	for _, value := range entry.Labels {
		add(value)
	}
	add(strconv.Itoa(len(entry.Links)))
	for _, link := range entry.Links {
		add(link.Label)
		add(link.URL)
	}
	add(strconv.Itoa(len(entry.ReviewNotes)))
	for _, value := range entry.ReviewNotes {
		add(value)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ExportBundle writes an editable, unpublished lyric collection without a
// database. Output must be absent or an empty directory. Files are prepared in a
// sibling temporary directory, then renamed together; a nonempty destination is
// never replaced, including if it becomes nonempty while export is running.
func ExportBundle(entries []Entry, output string) (BundleSummary, error) {
	summary := BundleSummary{SourceCount: len(entries)}
	if strings.TrimSpace(output) == "" {
		return summary, errors.New("output directory is required")
	}
	output, err := filepath.Abs(output)
	if err != nil {
		return summary, err
	}
	if err = checkEmptyOutput(output); err != nil {
		return summary, err
	}
	if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return summary, err
	}
	staging, err := os.MkdirTemp(filepath.Dir(output), ".lyrics-export-")
	if err != nil {
		return summary, err
	}
	defer os.RemoveAll(staging)
	for _, script := range []string{"malayalam", "manglish", "review", "source"} {
		if err = os.Mkdir(filepath.Join(staging, script), 0755); err != nil {
			return summary, err
		}
	}
	manifest := BundleManifest{SchemaVersion: 1, Source: BundleSource, Songs: []BundleSong{}, Skipped: []BundleSkipped{}}
	seen := make(map[string]Entry, len(entries))
	files := make(map[string]BundleSong, len(entries))
	for _, entry := range entries {
		if entry.SourceID == "" {
			return summary, errors.New("cannot export a post without a source ID")
		}
		song := bundleSong(entry)
		if previous, exists := seen[entry.SourceID]; exists {
			if song.ContentFingerprint != ContentFingerprint(previous) || entry.Hash != previous.Hash || entry.RawHTML != previous.RawHTML || entry.ReviewText != previous.ReviewText || entry.ReviewReason != previous.ReviewReason {
				return summary, fmt.Errorf("conflicting duplicate source ID %q; select one version before exporting", entry.SourceID)
			}
			manifest.Skipped = append(manifest.Skipped, BundleSkipped{BundleSong: files[entry.SourceID], Reason: "duplicate_source_id"})
			summary.DuplicateSourceCount++
			continue
		}
		seen[entry.SourceID] = entry
		filename := bundleFilename(entry)
		song.SourceFile = "source/" + strings.TrimSuffix(filename, ".txt") + ".html"
		if err = writeBundleFile(staging, song.SourceFile, []byte(entry.RawHTML)); err != nil {
			return summary, err
		}
		if entry.ReviewText != "" || entry.ReviewReason != "" {
			song.ReviewFile = "review/" + filename
			review := entry.ReviewText
			if review == "" {
				review = entry.ReviewReason
			}
			if err = writeBundleFile(staging, song.ReviewFile, []byte(review)); err != nil {
				return summary, err
			}
			summary.ReviewCount++
		}
		if entry.LyricsMalayalam == "" && entry.LyricsManglish == "" {
			reason := entry.ReviewReason
			if reason == "" {
				reason = "no_lyrics"
			}
			manifest.Skipped = append(manifest.Skipped, BundleSkipped{BundleSong: song, Reason: reason})
			files[entry.SourceID] = song
			continue
		}
		if entry.LyricsMalayalam != "" {
			song.MalayalamFile = "malayalam/" + filename
			if err = writeBundleFile(staging, song.MalayalamFile, []byte(entry.LyricsMalayalam)); err != nil {
				return summary, err
			}
			summary.MalayalamCount++
		}
		if entry.LyricsManglish != "" {
			song.ManglishFile = "manglish/" + filename
			if err = writeBundleFile(staging, song.ManglishFile, []byte(entry.LyricsManglish)); err != nil {
				return summary, err
			}
			summary.ManglishCount++
		}
		if entry.LyricsMalayalam != "" && entry.LyricsManglish != "" {
			summary.BothScriptsCount++
		}
		manifest.Songs = append(manifest.Songs, song)
		files[entry.SourceID] = song
	}
	summary.SongCount = len(manifest.Songs)
	summary.SkippedCount = len(manifest.Skipped)
	for name, value := range map[string]any{"manifest.json": manifest, "summary.json": summary} {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return summary, err
		}
		if err = writeBundleFile(staging, name, append(data, '\n')); err != nil {
			return summary, err
		}
	}
	if err = writeBundleIndex(staging, manifest); err != nil {
		return summary, err
	}
	if err = os.Chmod(staging, 0755); err != nil {
		return summary, err
	}
	// Recheck for a clear error message. Rename also refuses a nonempty directory.
	if err = checkEmptyOutput(output); err != nil {
		return summary, err
	}
	// macOS does not replace an existing empty directory with Rename. Rmdir
	// removes only empty directories, so a concurrently created file, symlink or
	// populated directory is protected (unlike a generic os.Remove call).
	if err = syscall.Rmdir(output); err != nil && !errors.Is(err, os.ErrNotExist) {
		return summary, fmt.Errorf("prepare empty output directory: %w", err)
	}
	if err = os.Rename(staging, output); err != nil {
		return summary, fmt.Errorf("install lyric bundle without overwriting existing files: %w", err)
	}
	return summary, nil
}

func checkEmptyOutput(output string) error {
	info, err := os.Lstat(output)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("output must be an absent or empty directory, not a file or symbolic link")
	}
	dir, err := os.Open(output)
	if err != nil {
		return err
	}
	defer dir.Close()
	_, err = dir.ReadDir(1)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return errors.New("output directory is not empty; choose a new directory to preserve edits")
}

func bundleSong(entry Entry) BundleSong {
	song := BundleSong{
		SourceID: entry.SourceID, SourceURL: entry.SourceURL, SourceHash: entry.Hash,
		Title: entry.Title, TitleMalayalam: entry.TitleMalayalam, Credits: entry.Credits,
		Labels: append([]string{}, entry.Labels...), Links: []BundleLink{},
		ReviewNotes: append([]string{}, entry.ReviewNotes...), ContentFingerprint: ContentFingerprint(entry),
		ReviewReason: entry.ReviewReason,
	}
	for _, link := range entry.Links {
		song.Links = append(song.Links, BundleLink{Label: link.Label, URL: link.URL})
	}
	return song
}

func bundleFilename(entry Entry) string {
	var slug strings.Builder
	separator := false
	for _, char := range strings.ToLower(entry.Title) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			if separator && slug.Len() > 0 {
				slug.WriteByte('-')
			}
			slug.WriteRune(char)
			separator = false
			if slug.Len() >= 72 {
				break
			}
		} else {
			separator = true
		}
	}
	if slug.Len() == 0 {
		slug.WriteString("hymn")
	}
	postID := ""
	if _, tail, ok := strings.Cut(entry.SourceID, ".post-"); ok && tail != "" {
		postID = tail
		for _, char := range tail {
			if char < '0' || char > '9' {
				postID = ""
				break
			}
		}
	}
	if postID == "" {
		sum := sha256.Sum256([]byte(entry.SourceID))
		postID = hex.EncodeToString(sum[:8])
	}
	return slug.String() + "-" + postID + ".txt"
}

func writeBundleFile(root, name string, data []byte) error {
	file, err := os.OpenFile(filepath.Join(root, filepath.FromSlash(name)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("write bundle file %s: %w", name, err)
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func writeBundleIndex(root string, manifest BundleManifest) error {
	var data strings.Builder
	w := csv.NewWriter(&data)
	rows := [][]string{{"status", "title", "titleMalayalam", "malayalamFile", "manglishFile", "sourceUrl", "reviewNotes", "reviewFile", "reviewReason", "sourceFile"}}
	add := func(status string, song BundleSong) {
		row := []string{status, song.Title, song.TitleMalayalam, song.MalayalamFile, song.ManglishFile, song.SourceURL, strings.Join(song.ReviewNotes, " | "), song.ReviewFile, song.ReviewReason, song.SourceFile}
		for index, value := range row {
			// Treat metadata as text when the review index is opened in a spreadsheet.
			if trimmed := strings.TrimLeft(value, " \t\r\n"); trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
				row[index] = "'" + value
			}
		}
		rows = append(rows, row)
	}
	for _, song := range manifest.Songs {
		add("review", song)
	}
	for _, skipped := range manifest.Skipped {
		add(skipped.Reason, skipped.BundleSong)
	}
	if err := w.WriteAll(rows); err != nil {
		return err
	}
	return writeBundleFile(root, "index.csv", []byte(data.String()))
}

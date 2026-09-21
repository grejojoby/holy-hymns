// Package importer reads the owner's Blogger collection into unpublished review entries.
package importer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)

const FeedURL = "https://grejolyrics.blogspot.com/feeds/posts/default?max-results=150"
const maxFeedBytes = 32 << 20
const blogID = "6936100729546217639"

type Link struct{ Label, URL string }
type Entry struct {
	SourceID, SourceURL, Title, TitleMalayalam, LyricsMalayalam, LyricsManglish, Credits, RawHTML, Hash string
	ReviewText, ReviewReason                                                                            string
	Labels, ReviewNotes                                                                                 []string
	Links                                                                                               []Link
}
type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
}
type atomCategory struct {
	Scheme string `xml:"scheme,attr"`
	Term   string `xml:"term,attr"`
}
type atomText struct {
	Type  string `xml:"type,attr"`
	Text  string `xml:",chardata"`
	Inner string `xml:",innerxml"`
}
type atomEntry struct {
	ID         string         `xml:"id"`
	Title      string         `xml:"title"`
	Content    atomText       `xml:"content"`
	Summary    atomText       `xml:"summary"`
	Links      []atomLink     `xml:"link"`
	Categories []atomCategory `xml:"category"`
	Draft      string         `xml:"control>draft"`
}
type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
	Links   []atomLink  `xml:"link"`
}

// Parse accepts a public Atom feed or Blogger Atom/XML export, including exports
// containing comments/settings. Only post entries become unpublished candidates.
func Parse(r io.Reader) ([]Entry, error) {
	f, err := decode(r)
	if err != nil {
		return nil, err
	}
	return entries(f), nil
}
func decode(r io.Reader) (atomFeed, error) {
	var f atomFeed
	data, err := io.ReadAll(io.LimitReader(r, maxFeedBytes+1))
	if err != nil {
		return f, err
	}
	if len(data) > maxFeedBytes {
		return f, errors.New("Blogger feed exceeds 32 MiB limit")
	}
	if err = xml.Unmarshal(data, &f); err != nil {
		return f, fmt.Errorf("decode Blogger Atom: %w", err)
	}
	return f, nil
}
func entries(f atomFeed) []Entry {
	out := make([]Entry, 0, len(f.Entries))
	for _, a := range f.Entries {
		skip := false
		for _, c := range a.Categories {
			if c.Scheme == "http://schemas.google.com/g/2005#kind" && c.Term != "http://schemas.google.com/blogger/2008/kind#post" {
				skip = true
			}
		}
		if skip || !strings.Contains(a.ID, ".post-") {
			continue
		}
		e := Entry{SourceID: a.ID, Title: strings.TrimSpace(a.Title), Labels: []string{}, Links: []Link{}, ReviewNotes: []string{"Imported draft: check lyrics, categories, credits and permission before publishing."}}
		for _, l := range a.Links {
			if l.Rel == "alternate" && safeURL(l.Href) != "" {
				e.SourceURL = l.Href
				break
			}
		}
		if e.SourceURL == "" {
			e.ReviewNotes = append(e.ReviewNotes, "Source permalink missing from export.")
		}
		for _, c := range a.Categories {
			if c.Scheme == "http://www.blogger.com/atom/ns#" {
				e.Labels = append(e.Labels, c.Term)
			}
		}
		sort.Strings(e.Labels)
		content := a.Content
		if content.Text == "" && content.Inner == "" {
			content = a.Summary
			e.ReviewNotes = append(e.ReviewNotes, "Only a feed summary was available; obtain full content before publishing.")
		}
		e.RawHTML = content.Text
		if content.Type == "xhtml" {
			e.RawHTML = content.Inner
		}
		sum := sha256.Sum256([]byte(a.Title + "\x00" + e.RawHTML + "\x00" + strings.Join(e.Labels, "\x00")))
		e.Hash = hex.EncodeToString(sum[:])
		plain, links, media := extractHTML(e.RawHTML)
		e.Links = links
		if len(e.Links) > 10 {
			e.Links = e.Links[:10]
			e.ReviewNotes = append(e.ReviewNotes, "More than ten external links: only the first ten retained; review this possible download collection.")
		}
		if strings.Contains(e.RawHTML, "http://") {
			e.ReviewNotes = append(e.ReviewNotes, "Legacy HTTP links were upgraded for known providers or omitted; check links against the source.")
		}
		splitContent(&e, plain)
		titleParts := strings.Split(a.Title, "|")
		latinTitleFound := false
		for _, p := range titleParts {
			p = strings.TrimSpace(p)
			if countMalayalam(p) > 0 && e.TitleMalayalam == "" {
				e.TitleMalayalam = p
			}
			if !latinTitleFound && countMalayalam(p) == 0 && hasLatin(p) {
				e.Title = trimTitle(p)
				latinTitleFound = true
			}
		}
		applyAuditedClassification(&e, plain)
		if a.Draft == "yes" {
			e.ReviewNotes = append(e.ReviewNotes, "This was a draft in Blogger.")
		}
		if e.LyricsMalayalam == "" && e.LyricsManglish == "" {
			if media || len(e.Links) > 0 {
				e.ReviewNotes = append(e.ReviewNotes, "Media-only post: no importable lyrics; keep unpublished or exclude.")
			} else {
				e.ReviewNotes = append(e.ReviewNotes, "No importable lyric text found.")
			}
		}
		if e.LyricsManglish != "" {
			e.ReviewNotes = append(e.ReviewNotes, "Latin-script text needs review: confirm it is Manglish lyrics and remove any remaining prose.")
		}
		out = append(out, e)
	}
	markPossibleDuplicates(out)
	return out
}

// Fetch follows only this owner's public feed. Blogger's next links use its API
// hostname, so the exact known blog path is canonicalized to the blog origin.
// It does not fetch links, images, videos or other user-supplied website URLs.
func Fetch(ctx context.Context, rawURL string) ([]Entry, error) {
	if rawURL == "" {
		rawURL = FeedURL
	}
	next, err := canonicalFeed(rawURL)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many feed redirects")
		}
		_, err := canonicalFeed(req.URL.String())
		return err
	}}
	out := []Entry{}
	seenPages := map[string]bool{}
	seenEntries := map[string]bool{}
	for page := 0; next != ""; page++ {
		if page >= 100 || len(out) > 10000 {
			return nil, errors.New("feed pagination limit reached")
		}
		if seenPages[next] {
			return nil, errors.New("feed pagination loop")
		}
		seenPages[next] = true
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "HolyHymns/1.0 (owner-authorized one-time Blogger import)")
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("fetch Blogger page: %w", err)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("Blogger returned HTTP %d", resp.StatusCode)
		}
		f, err := decode(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, e := range entries(f) {
			if !seenEntries[e.SourceID] {
				seenEntries[e.SourceID] = true
				out = append(out, e)
			}
		}
		next = ""
		for _, l := range f.Links {
			if l.Rel == "next" {
				next, err = canonicalFeed(l.Href)
				if err != nil {
					return nil, err
				}
				break
			}
		}
	}
	markPossibleDuplicates(out)
	return out, nil
}
func canonicalFeed(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" || u.User != nil || u.Port() != "" || u.Fragment != "" {
		return "", errors.New("only the HTTPS Grejo Lyrics public feed is allowed")
	}
	allowed := u.Host == "grejolyrics.blogspot.com" && strings.TrimRight(u.Path, "/") == "/feeds/posts/default"
	blogger := u.Host == "www.blogger.com" && u.Path == "/feeds/"+blogID+"/posts/default"
	if !allowed && !blogger {
		return "", errors.New("feed URL is outside the authorized Grejo Lyrics blog")
	}
	q := u.Query()
	for k := range q {
		if k != "start-index" && k != "max-results" && k != "alt" {
			return "", fmt.Errorf("unsupported feed parameter %q", k)
		}
	}
	if alt := q.Get("alt"); alt != "" && alt != "atom" {
		return "", errors.New("feed must be Atom")
	}
	for _, key := range []string{"start-index", "max-results"} {
		if q.Get(key) != "" {
			n, err := strconv.Atoi(q.Get(key))
			if err != nil || n < 1 {
				return "", fmt.Errorf("invalid feed %s", key)
			}
			if key == "max-results" && n > 150 {
				q.Set(key, "150")
			}
		}
	}
	if q.Get("max-results") == "" {
		q.Set("max-results", "150")
	}
	u.Host = "grejolyrics.blogspot.com"
	u.Path = "/feeds/posts/default"
	u.RawQuery = q.Encode()
	return u.String(), nil
}

var urls = regexp.MustCompile(`https?://[^\s<>"']+`)
var credit = regexp.MustCompile(`(?i)^(lyrics?\s*(by|:|&)|music\s*(by|:|&)|sung\s*by|singer\s*:|album\s*:|credits?\s*:|രചന\s*:|സംഗീതം\s*:|ആലാപനം\s*:)`)
var trimSuffix = regexp.MustCompile(`(?i)\s+(malayalam\s+)?(christian\s+)?(lyrics|carol song|christmas song|communion song)\s*$`)
var chordToken = regexp.MustCompile(`^[A-G](?:#|b)?(?:m|M|maj|min|dim|aug|sus|add)?[0-9]*(?:/[A-G](?:#|b)?)?$`)
var repeatToken = regexp.MustCompile(`^[xX][0-9]+$`)

// These exceptions were checked against the owner's 19 September 2026 Atom
// snapshot. They describe source content, not inferred translations. Keep their
// original HTML even when a passage is unsuitable for either lyric folder.
var unsupportedLanguagePosts = map[string]bool{
	"7467296383819479544": true, // There shall be showers of blessing (English).
	"653406135880047531":  true, // Blessed Assurance (English).
	"8891548616016530348": true, // Lord I Need You (English).
	"8974880354421202274": true, // 10000 Reasons (English).
	"8933912427001022538": true, // More Love More Power (English).
	"1064789329039523778": true, // Light of the World (English).
	"8095165510691989318": true, // Above All (English).
	"5211936272090014315": true, // Yehova yire Chords (English version).
	"2317786297776393823": true, // Lekhar khao (romanized Hindi).
	"3674504320875468804": true, // Lord I Lift Your Name (English + Hindi).
}

func sourcePostID(e *Entry) string {
	prefix := "tag:blogger.com,1999:blog-" + blogID + ".post-"
	if !strings.HasPrefix(e.SourceID, prefix) {
		return ""
	}
	return strings.TrimPrefix(e.SourceID, prefix)
}

func applyAuditedClassification(e *Entry, plain string) {
	id := sourcePostID(e)
	// The source titles place an artist or album beside the actual song title.
	switch id {
	case "884282782559744544": // Yeshuve യേശുവേ | Anil Adoor
		e.Title, e.TitleMalayalam = "Yeshuve", "യേശുവേ"
	case "7990905786705019939": // യഹോവ തൻ - Yahova Than | Eesow | Malayalam Lyrics
		e.Title, e.TitleMalayalam = "Yahova Than", "യഹോവ തൻ"
	case "5934187810894494664": // EDAN | AMME ENNASRAYAME | അമ്മേ എന്നാശ്രയമേ | Nithya Mammen
		e.Title, e.TitleMalayalam = "AMME ENNASRAYAME", "അമ്മേ എന്നാശ്രയമേ"
	}
	switch {
	case unsupportedLanguagePosts[id]:
		e.ReviewText, e.ReviewReason = e.LyricsManglish, "unsupported-language"
		e.LyricsManglish = ""
		e.ReviewNotes = appendUnique(e.ReviewNotes, "Source contains English or Hindi lyrics, not Manglish; original text retained for language review.")
	case id == "2065110906634477698": // Mother Mary Songs: prose and twelve links.
		e.ReviewText, e.ReviewReason = strings.TrimSpace(plain), "index-only"
		e.LyricsMalayalam, e.LyricsManglish = "", ""
		e.ReviewNotes = appendUnique(e.ReviewNotes, "Index of other song posts, not a lyric entry.")
	case id == "1659740555254951777" || id == "4048871075412699486":
		e.ReviewText, e.ReviewReason = strings.TrimSpace(plain), "multiple-songs"
		e.LyricsMalayalam, e.LyricsManglish = "", ""
		e.ReviewNotes = appendUnique(e.ReviewNotes, "Multiple separately headed hymns share this post; split and review them before upload.")
	case id == "8936795683805259737": // Entho nee thiranju vannee: four images and a cross-link.
		e.ReviewText, e.ReviewReason = strings.TrimSpace(plain), "image-only"
		e.LyricsMalayalam, e.LyricsManglish = "", ""
		e.ReviewNotes = appendUnique(e.ReviewNotes, "Lyrics are in images; no OCR or transcription was generated. Review the preserved source HTML.")
	case strings.Contains(strings.ToLower(e.Title), "chords") && containsChordNotation(e.LyricsManglish):
		e.ReviewText, e.ReviewReason = e.LyricsManglish, "chord-notation"
		e.LyricsManglish = ""
		e.ReviewNotes = appendUnique(e.ReviewNotes, "Chord notation remains in the extracted text; separate lyrics from the arrangement before upload.")
	}
}

// Only a whole line of chord/repeat tokens qualifies; never strip possible
// chord names out of words. This check only classifies explicitly titled chord
// arrangements, and keeps the text intact for manual review.
func containsChordNotation(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		count, valid := 0, true
		for _, field := range strings.Fields(line) {
			field = strings.Trim(field, ".,|%[]()")
			if field == "" || repeatToken.MatchString(field) {
				continue
			}
			if !chordToken.MatchString(field) {
				valid = false
				break
			}
			count++
		}
		if valid && count > 0 {
			return true
		}
	}
	return false
}

func auditedCredit(e *Entry, line string) bool {
	var prefixes []string
	switch sourcePostID(e) {
	case "5978461950277127855", "8138959059767832510":
		prefixes = []string{"Album ", "Lyricist ", "Music ", "Singer "}
	case "3183341688813281657":
		prefixes = []string{"Co ordinator :", "Mixing & Editing :"}
	case "9162823684747299452":
		prefixes = []string{"Writer:", "Language written in:", "Year written:", "Music set in:"}
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func auditedNonLyricLine(e *Entry, line string) bool {
	switch sourcePostID(e) {
	case "93294363000065340":
		return line == "English (Manglish) (Transliteration) Lyrics:"
	case "2102785157239960108":
		return line == "x"
	case "3183341688813281657":
		return line == "Video Youtube Link"
	case "2784294285229204806":
		return line == "Song - Manasoru Sacrariyy" || line == "Karaoke - Kar_Mansasoru Sakrariyay"
	}
	return false
}

func trimTitle(s string) string {
	for {
		n := strings.TrimSpace(trimSuffix.ReplaceAllString(s, ""))
		if n == s || n == "" {
			return s
		}
		s = n
	}
}
func safeURL(s string) string {
	u, e := url.Parse(strings.TrimSpace(s))
	if e != nil || u.User != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	if u.Scheme == "http" {
		switch strings.ToLower(u.Hostname()) {
		case "youtube.com", "www.youtube.com", "youtu.be", "www.youtu.be", "vimeo.com", "www.vimeo.com", "grejolyrics.blogspot.com", "drive.google.com", "www.mediafire.com", "mediafire.com":
			u.Scheme = "https"
		default:
			return ""
		}
	}
	return u.String()
}
func countMalayalam(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x0D00 && r <= 0x0D7F {
			n++
		}
	}
	return n
}
func hasLatin(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}
func nodeText(n *html.Node) string {
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			text.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.TrimSpace(text.String())
}
func extractHTML(raw string) (string, []Link, bool) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return "", []Link{}, false
	}
	var b strings.Builder
	links := []Link{}
	seen := map[string]bool{}
	media := false
	addLink := func(raw, label string) {
		u := safeURL(raw)
		if u == "" || seen[u] {
			return
		}
		seen[u] = true
		if label == "" {
			label = "Song link"
		}
		links = append(links, Link{label, u})
	}
	newline := func() {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n") {
			b.WriteByte('\n')
		}
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "nav", "footer", "form", "noscript", "ins":
				return
			}
			for _, a := range n.Attr {
				if a.Key == "class" {
					for _, class := range strings.Fields(a.Val) {
						if class == "cgl-chord-token" {
							text := nodeText(n)
							if chordToken.MatchString(text) || repeatToken.MatchString(text) || text == "INSTRUMENTAL" || text == "INTRO" || text == "VERSE" || text == "CHORUS" {
								return // Do not join positioned chord labels into lyric words.
							}
							// Two source posts accidentally apply this class to actual
							// lyric words such as Aakasame/kelkka. Preserve those words.
						}
					}
				}
				if (a.Key == "class" || a.Key == "id") && (strings.Contains(a.Val, "blogger-post-footer") || strings.Contains(a.Val, "adsbygoogle") || strings.Contains(a.Val, "comment")) {
					return
				}
			}
			if n.Data == "img" {
				media = true
				return
			}
			if n.Data == "iframe" || n.Data == "video" || n.Data == "audio" {
				media = true
				for _, a := range n.Attr {
					if a.Key == "src" {
						addLink(a.Val, "Song / karaoke")
					}
				}
				return
			}
			if n.Data == "a" {
				// Linked cover/lyric images are not playable external song links.
				imageOnly := false
				for c := n.FirstChild; c != nil; c = c.NextSibling {
					if c.Type == html.ElementNode && c.Data == "img" {
						imageOnly = true
					}
				}
				if imageOnly {
					media = true
					return
				}
				for _, a := range n.Attr {
					if a.Key == "href" {
						if u, err := url.Parse(a.Val); err == nil && (strings.EqualFold(u.Hostname(), "www.chordbank.com") || strings.EqualFold(u.Hostname(), "chordbank.com")) {
							return // This source uses chordbank anchors for chord tokens, not lyrics.
						}
						addLink(a.Val, "Song link")
					}
				}
			}
			if n.Data == "br" {
				b.WriteByte('\n')
				return
			}
			if n.Data == "div" || n.Data == "p" || n.Data == "li" || n.Data == "h1" || n.Data == "h2" || n.Data == "h3" {
				newline()
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(strings.ReplaceAll(n.Data, "\u00a0", " "))
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && (n.Data == "div" || n.Data == "p" || n.Data == "li" || n.Data == "h1" || n.Data == "h2" || n.Data == "h3") {
			newline()
			if n.Data == "p" {
				b.WriteByte('\n')
			}
		}
	}
	walk(doc)
	// Raw Blogger prose often contains unlinked karaoke URLs.
	for _, s := range urls.FindAllString(b.String(), -1) {
		addLink(strings.TrimRight(s, ".,);"), "Song / karaoke")
	}
	return b.String(), links, media
}
func splitContent(e *Entry, plain string) {
	ml, latin := []string{}, []string{}
	credits := []string{}
	last := 0
	pendingBlank := false
	for _, raw := range strings.Split(strings.ReplaceAll(plain, "\r", ""), "\n") {
		line := strings.Join(strings.Fields(raw), " ")
		if line == "" {
			pendingBlank = true
			continue
		}
		lower := strings.ToLower(line)
		if credit.MatchString(line) || auditedCredit(e, line) {
			credits = append(credits, line)
			continue
		}
		if urls.MatchString(line) {
			e.ReviewNotes = appendUnique(e.ReviewNotes, "Extracted URLs from content; review the external links and remaining lyric text.")
			line = strings.TrimSpace(urls.ReplaceAllString(line, ""))
			lower = strings.ToLower(line)
			if line == "" {
				continue
			}
		}
		if auditedNonLyricLine(e, line) {
			continue
		}
		if strings.Contains(e.RawHTML, "cgl-chord-token") {
			// These labels accompany positioned chord spans in seven audited
			// posts. Removing them leaves the surrounding lyric words untouched.
			if strings.HasPrefix(line, "Scale:") || strings.HasPrefix(line, "Original Key:") || strings.HasPrefix(line, "Time Signature:") || strings.HasPrefix(line, "Tempo:") || repeatToken.MatchString(line) {
				continue
			}
			switch strings.ToUpper(line) {
			case "INTRO", "INSTRUMENTAL", "VERSE", "CHORUS", ".", "INTRO X2", "INTRO X4", "INSTRUMENTAL X2", "INSTRUMENTAL X4":
				continue
			}
		}
		lower = strings.TrimSpace(strings.TrimRight(lower, ":"))
		if strings.HasPrefix(lower, "download") || strings.HasPrefix(lower, "pdf file of") || strings.HasSuffix(lower, "- karaoke") || strings.HasSuffix(lower, "– karaoke") || strings.Contains(lower, "hindi mass book") || strings.HasSuffix(lower, ".pdf") {
			continue
		}
		switch lower {
		case "malayalam", "malayalam lyrics", "മലയാളം", "മലയാളം വരികൾ", "manglish", "manglish lyrics", "english lyrics", "lyrics", "karaoke", "karaoke link", "song link", "songs playlist", "song playlist", "watch video", "click here":
			continue
		}
		script := 0
		if countMalayalam(line) > 0 {
			script = 1
			if hasLatin(line) {
				e.ReviewNotes = appendUnique(e.ReviewNotes, "Mixed scripts occur within lyric lines; verify the script separation.")
			}
		} else if hasLatin(line) {
			script = 2
		} else {
			script = last
		}
		if script == 0 {
			e.ReviewNotes = appendUnique(e.ReviewNotes, "Unclassified text omitted; compare with the source before publishing.")
			continue
		}
		dst := &ml
		if script == 2 {
			dst = &latin
		}
		if pendingBlank && len(*dst) > 0 && last == script {
			*dst = append(*dst, "")
		}
		*dst = append(*dst, line)
		last = script
		pendingBlank = false
	}
	e.LyricsMalayalam = strings.TrimSpace(strings.Join(ml, "\n"))
	e.LyricsManglish = strings.TrimSpace(strings.Join(latin, "\n"))
	e.Credits = strings.Join(credits, "\n")
}
func appendUnique(items []string, s string) []string {
	for _, v := range items {
		if v == s {
			return items
		}
	}
	return append(items, s)
}

// Title matching only raises a review note; it never merges or discards sources.
func markPossibleDuplicates(entries []Entry) {
	groups := map[string][]int{}
	for i, e := range entries {
		key := strings.ToLower(strings.Join(strings.Fields(e.Title), " "))
		if key != "" {
			groups[key] = append(groups[key], i)
		}
	}
	for _, indexes := range groups {
		if len(indexes) < 2 {
			continue
		}
		for _, i := range indexes {
			entries[i].ReviewNotes = appendUnique(entries[i].ReviewNotes, "Possible duplicate title: compare other source posts before publishing.")
		}
	}
}

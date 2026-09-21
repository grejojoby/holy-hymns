package importer

import (
	"os"
	"strings"
	"testing"
)

func feed(body string) string {
	return `<feed xmlns="http://www.w3.org/2005/Atom"><entry><id>tag:blogger.com,1999:blog-6936100729546217639.post-1</id><title>Grace Lyrics | Malayalam Christian</title><link rel="alternate" href="https://grejolyrics.blogspot.com/2020/01/grace.html"/><category scheme="http://www.blogger.com/atom/ns#" term="Communion"/><content type="html"><![CDATA[` + body + `]]></content></entry></feed>`
}
func TestParsePreservesScriptsStanzasAndCredits(t *testing.T) {
	in := feed(`<div>കർത്താവേ കനിയണമേ</div><div>കൃപ നൽകണമേ</div><div><br/></div><div>നാഥാ (2)</div><div><br/></div><div>Karthave kaniyaname</div><div>Kripa nalkaname</div><div><br/></div><div>Nadha (2)</div><div>Lyrics: Author</div><div>Karaoke Link: https://www.youtube.com/watch?v=example</div><script>tracking()</script><div class="blogger-post-footer">Subscribe</div>`)
	es, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 1 {
		t.Fatal(es)
	}
	e := es[0]
	if e.LyricsMalayalam != "കർത്താവേ കനിയണമേ\nകൃപ നൽകണമേ\n\nനാഥാ (2)" {
		t.Errorf("Malayalam stanza changed: %q", e.LyricsMalayalam)
	}
	if e.LyricsManglish != "Karthave kaniyaname\nKripa nalkaname\n\nNadha (2)" {
		t.Errorf("Manglish stanza changed: %q", e.LyricsManglish)
	}
	if e.Credits != "Lyrics: Author" || len(e.Links) != 1 || e.Title != "Grace" || len(e.Labels) != 1 || len(e.Hash) != 64 {
		t.Fatalf("metadata not preserved: %#v", e)
	}
	again, _ := Parse(strings.NewReader(in))
	if again[0].Hash != e.Hash {
		t.Fatal("unstable hash")
	}
}
func TestMediaOnlyAndComments(t *testing.T) {
	es, err := Parse(strings.NewReader(feed(`<iframe src="https://www.youtube.com/embed/example"></iframe><img src="cover.png"/><div>Karaoke</div>`)))
	if err != nil {
		t.Fatal(err)
	}
	if es[0].LyricsManglish != "" || es[0].LyricsMalayalam != "" || len(es[0].Links) != 1 {
		t.Fatal(es[0])
	}
	if !strings.Contains(strings.Join(es[0].ReviewNotes, " "), "Media-only") {
		t.Fatal("not flagged for exclusion")
	}
	comment := strings.Replace(feed("A comment"), `<content`, `<category scheme="http://schemas.google.com/g/2005#kind" term="http://schemas.google.com/blogger/2008/kind#comment"/><content`, 1)
	es, err = Parse(strings.NewReader(comment))
	if err != nil || len(es) != 0 {
		t.Fatal("comment became song", es, err)
	}
}
func TestUnicodeNotDestroyed(t *testing.T) {
	text := "കർത്താവേ\u200d എൻ്റെ നാഥാ"
	es, err := Parse(strings.NewReader(feed(text)))
	if err != nil {
		t.Fatal(err)
	}
	if es[0].LyricsMalayalam != text {
		t.Fatalf("meaningful Unicode changed: %q", es[0].LyricsMalayalam)
	}
}
func TestFeedURLRestriction(t *testing.T) {
	for _, u := range []string{"http://grejolyrics.blogspot.com/feeds/posts/default", "https://grejolyrics.blogspot.com.evil.test/feeds/posts/default", "https://www.blogger.com/feeds/999/posts/default", "https://grejolyrics.blogspot.com:443/feeds/posts/default", "https://user@grejolyrics.blogspot.com/feeds/posts/default", "https://127.0.0.1/feeds/posts/default", "https://grejolyrics.blogspot.com/feeds/posts/default?alt=json", "https://grejolyrics.blogspot.com/feeds/posts/default?redirect=http://localhost"} {
		if _, err := canonicalFeed(u); err == nil {
			t.Errorf("accepted unsafe feed %s", u)
		}
	}
	got, err := canonicalFeed("https://www.blogger.com/feeds/6936100729546217639/posts/default?start-index=151&max-results=150")
	if err != nil || !strings.HasPrefix(got, "https://grejolyrics.blogspot.com/") {
		t.Fatal(got, err)
	}
}
func TestSummaryAndMalformedInput(t *testing.T) {
	in := strings.ReplaceAll(strings.ReplaceAll(feed("Partial text"), "<content", "<summary"), "</content>", "</summary>")
	es, err := Parse(strings.NewReader(in))
	if err != nil || !strings.Contains(strings.Join(es[0].ReviewNotes, " "), "summary") {
		t.Fatal(es, err)
	}
	if _, err := Parse(strings.NewReader("<html>not Atom</html>")); err == nil {
		t.Fatal("non-feed accepted")
	}
	if _, err := Parse(strings.NewReader("<feed>")); err == nil {
		t.Fatal("malformed XML accepted")
	}
}

func TestParagraphsAndLegacyMedia(t *testing.T) {
	es, err := Parse(strings.NewReader(feed(`<p>Karthave<br/>Kaniyaname</p><p>Nadha<br/>Arulename</p><iframe src="http://www.youtube.com/embed/example"></iframe><a href="http://unknown.example/file">Click here</a>`)))
	if err != nil {
		t.Fatal(err)
	}
	if es[0].LyricsManglish != "Karthave\nKaniyaname\n\nNadha\nArulename" {
		t.Fatal(es[0].LyricsManglish)
	}
	if len(es[0].Links) != 1 || es[0].Links[0].URL != "https://www.youtube.com/embed/example" {
		t.Fatal(es[0].Links)
	}
	got, err := canonicalFeed("https://grejolyrics.blogspot.com/feeds/posts/default?max-results=500")
	if err != nil || !strings.Contains(got, "max-results=150") {
		t.Fatal("Blogger page cap not enforced", got, err)
	}
}
func TestRepositorySnapshot(t *testing.T) {
	f, err := os.Open("../../../content/grejo-lyrics.atom")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	es, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 331 {
		t.Fatalf("expected the verified 19 September 2026 snapshot's 331 posts, got %d", len(es))
	}
	skipped, ml, latin, both := 0, 0, 0, 0
	reasons := map[string]int{}
	byID := map[string]Entry{}
	for _, e := range es {
		byID[sourcePostID(&e)] = e
		if e.ReviewReason != "" {
			reasons[e.ReviewReason]++
		}
		if e.TitleMalayalam != "" && countMalayalam(e.TitleMalayalam) == 0 {
			t.Errorf("Malayalam title contains no Malayalam: %s", e.SourceID)
		}
		if strings.TrimSpace(e.LyricsMalayalam+e.LyricsManglish) == "" {
			skipped++
		}
		if e.LyricsMalayalam != "" {
			ml++
		}
		if e.LyricsManglish != "" {
			latin++
		}
		if e.LyricsMalayalam != "" && e.LyricsManglish != "" {
			both++
		}
		if len(e.Links) > 10 {
			t.Errorf("too many links in %s", e.Title)
		}
		for _, l := range e.Links {
			if !strings.HasPrefix(l.URL, "https://") {
				t.Errorf("unsafe link %s in %s", l.URL, e.Title)
			}
		}
		if strings.Contains(e.Title, "Latest Short Syro Malabar Qurbana Karaoke") || strings.Contains(e.Title, "Hindi Qurbana Songs") || e.Title == "Entho nee thiranju vannee" {
			if e.LyricsMalayalam+e.LyricsManglish != "" {
				t.Errorf("media-only post interpreted as lyrics %s: %q", e.Title, e.LyricsMalayalam+e.LyricsManglish)
			}
		}
	}
	if skipped != 43 || ml != 46 || latin != 279 || both != 37 {
		t.Fatalf("audited snapshot classification changed: excluded=%d Malayalam=%d Manglish=%d both=%d", skipped, ml, latin, both)
	}
	for reason, want := range map[string]int{"unsupported-language": 10, "chord-notation": 27, "index-only": 1, "multiple-songs": 2, "image-only": 1} {
		if reasons[reason] != want {
			t.Errorf("%s: got %d reviewed entries, want %d", reason, reasons[reason], want)
		}
	}
	for id, title := range map[string]string{"884282782559744544": "Yeshuve", "7990905786705019939": "Yahova Than", "5934187810894494664": "AMME ENNASRAYAME"} {
		if byID[id].Title != title {
			t.Errorf("audited title override %s: %q", id, byID[id].Title)
		}
	}
	for id, words := range map[string]string{"8338878166529163869": "Aakasame kelkka", "6453163812901209870": "Aazhangal kadanneedumpol"} {
		if !strings.Contains(byID[id].LyricsManglish, words) {
			t.Errorf("lyric words incorrectly tagged as chords were removed from %s", id)
		}
	}
	t.Logf("331 source posts; %d without text; %d have Malayalam; %d have Latin text; %d have both", skipped, ml, latin, both)
}

func TestDuplicateTitlesAreFlaggedWithoutMerging(t *testing.T) {
	first := feed("Karthave kaniyaname")
	second := strings.Replace(first, "post-1", "post-2", 1)
	start := strings.Index(second, "<entry>")
	end := strings.Index(second, "</entry>") + len("</entry>")
	combined := strings.Replace(first, "</feed>", second[start:end]+"</feed>", 1)
	es, err := Parse(strings.NewReader(combined))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 {
		t.Fatal("distinct sources were merged")
	}
	for _, e := range es {
		if !strings.Contains(strings.Join(e.ReviewNotes, " "), "Possible duplicate title") {
			t.Fatal("duplicate title unflagged")
		}
	}
}

func postFeed(id, title, body string) string {
	in := strings.Replace(feed(body), ".post-1", ".post-"+id, 1)
	return strings.Replace(in, "Grace Lyrics | Malayalam Christian", title, 1)
}

func TestTitleScriptsAreIndependentOfOrder(t *testing.T) {
	for _, title := range []string{"Enne Nannai Ariyunnone | എന്നെ നന്നായി അറിയുന്നോനെ", "എന്നെ നന്നായി അറിയുന്നോനെ | Enne Nannai Ariyunnone"} {
		es, err := Parse(strings.NewReader(postFeed("1", title, "എന്നെ നന്നായി അറിയുന്നോനെ")))
		if err != nil || len(es) != 1 {
			t.Fatal(es, err)
		}
		if es[0].Title != "Enne Nannai Ariyunnone" || es[0].TitleMalayalam != "എന്നെ നന്നായി അറിയുന്നോനെ" {
			t.Fatalf("wrong bilingual title for %q: %#v", title, es[0])
		}
	}
}

func TestInlineChordTokensPreserveLyricWords(t *testing.T) {
	body := `<div>Original Key: E Minor</div><div>Time Signature: 4/4</div><div>Tempo: 80</div><div>CHORUS</div><div><a href="https://www.chordbank.com/chords/c-major"><span class="cgl-chord-token">C</span></a>Ente ullil va<span class="cgl-chord-token">D</span>sikkan va<span class="cgl-chord-token">Em</span>rane</div><div><span class="cgl-chord-token">Aakasame</span>&nbsp;<span class="cgl-chord-token">kelkka</span></div><div><span class="cgl-chord-token">x2</span></div><iframe src="https://www.youtube.com/embed/example"></iframe>`
	es, err := Parse(strings.NewReader(postFeed("1", "Aathmavam Chords", body)))
	if err != nil || len(es) != 1 {
		t.Fatal(es, err)
	}
	e := es[0]
	if e.LyricsManglish != "Ente ullil vasikkan varane\nAakasame kelkka" || e.ReviewReason != "" {
		t.Fatalf("lyric words changed or clean lyrics quarantined: %#v", e)
	}
	if e.RawHTML != body || len(e.Links) != 1 || !strings.Contains(e.Links[0].URL, "youtube.com") {
		t.Fatal("raw source lost or chord link imported", e)
	}
}

func TestAuditedNoiseAndCreditsStaySourceScoped(t *testing.T) {
	cases := []struct{ id, body, wantLyrics, wantCredit string }{
		{"93294363000065340", `<div>മണികൾ മുഴങ്ങീടുമീ</div><p>English (Manglish) (Transliteration) Lyrics: https://grejolyrics.blogspot.com/2016/12/manikal-muzhangeedumee.html</p>`, "", ""},
		{"2102785157239960108", `<p>ഞങ്ങളുടെ കുടുംബത്തിന്മേൽ</p><p>x</p>`, "", ""},
		{"5978461950277127855", `<div>Album Snehapratheekam</div><div>Lyricist AJ Joseph</div><div>Music AJ Joseph</div><div>Singer KJ Yesudas</div><div>Doore ninnum</div>`, "Doore ninnum", "Album Snehapratheekam\nLyricist AJ Joseph\nMusic AJ Joseph\nSinger KJ Yesudas"},
		{"3183341688813281657", `<div>Manju peyyunna rathri</div><div>Co ordinator : Lijo Varghese Moonjely</div><div>Mixing &amp; Editing : Jomon Moonjely</div><div>Video Youtube Link</div>`, "Manju peyyunna rathri", "Co ordinator : Lijo Varghese Moonjely\nMixing & Editing : Jomon Moonjely"},
		{"2784294285229204806", `<div>Manassoru sacraariyay</div><div>Song - Manasoru Sacrariyy</div><div>Karaoke - Kar_Mansasoru Sakrariyay</div>`, "Manassoru sacraariyay", ""},
		{"1", `<div>x</div><div>Music AJ Joseph</div>`, "x\nMusic AJ Joseph", ""},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			es, err := Parse(strings.NewReader(postFeed(c.id, "A source title", c.body)))
			if err != nil || len(es) != 1 {
				t.Fatal(es, err)
			}
			if es[0].LyricsManglish != c.wantLyrics || es[0].Credits != c.wantCredit || es[0].RawHTML != c.body {
				t.Fatalf("incorrect extraction: %#v", es[0])
			}
		})
	}
}

func TestReviewClassificationPreservesRejectedText(t *testing.T) {
	for id := range unsupportedLanguagePosts {
		es, err := Parse(strings.NewReader(postFeed(id, "A source title", `<div>കർത്താവേ</div><div>English words for review</div>`)))
		if err != nil || len(es) != 1 {
			t.Fatal(es, err)
		}
		e := es[0]
		if e.ReviewReason != "unsupported-language" || e.ReviewText != "English words for review" || e.LyricsMalayalam != "കർത്താവേ" || e.LyricsManglish != "" {
			t.Fatal("unsupported language lost, mislabeled, or valid Malayalam removed", e)
		}
	}
	for id, reason := range map[string]string{"2065110906634477698": "index-only", "1659740555254951777": "multiple-songs", "4048871075412699486": "multiple-songs", "8936795683805259737": "image-only"} {
		es, err := Parse(strings.NewReader(postFeed(id, "A source title", `<div>Source text</div><img src="lyrics.png"/>`)))
		if err != nil || len(es) != 1 {
			t.Fatal(es, err)
		}
		e := es[0]
		if e.ReviewReason != reason || e.ReviewText != "Source text" || e.LyricsMalayalam+e.LyricsManglish != "" {
			t.Fatal("review text not preserved separately", e)
		}
	}
	for _, c := range []struct{ title, body, reason string }{
		{"Krooshil Chords", "<div>Bm Em D C</div><div>Krooshil kandu njaan</div>", "chord-notation"},
		{"Krooshil Chords", "<div>Krooshil kandu njaan</div>", ""},
		{"A song", "<div>Amma ente amme</div>", ""},
	} {
		es, err := Parse(strings.NewReader(postFeed("1", c.title, c.body)))
		if err != nil || len(es) != 1 {
			t.Fatal(es, err)
		}
		e := es[0]
		if e.ReviewReason != c.reason || (c.reason != "" && (e.ReviewText == "" || e.LyricsManglish != "")) || (c.reason == "" && e.LyricsManglish == "") {
			t.Fatalf("chord classification must require actual notation: %#v", e)
		}
	}
}

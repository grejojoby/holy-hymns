package server

import "testing"

func TestNormalizePreservesMalayalamMarks(t *testing.T) {
	s := " കർത്താവേ   കനിയണമേ "
	if got := normalize(s); got != "കർത്താവേ കനിയണമേ" {
		t.Fatal(got)
	}
}
func TestPublishNeedsLyricsAndSafeLinks(t *testing.T) {
	s := Song{Title: "Test"}
	if s.validate(true) == nil {
		t.Fatal("empty lyric published")
	}
	s.LyricsManglish = "A line"
	s.Links = []Link{{URL: "javascript:alert(1)"}}
	if s.validate(true) == nil {
		t.Fatal("unsafe link accepted")
	}
	s.Links = []Link{{URL: "https://www.youtube.com/watch?v=abc"}}
	if e := s.validate(true); e != nil {
		t.Fatal(e)
	}
}
func TestOneScriptIsValid(t *testing.T) {
	s := Song{Title: "ഹേ", LyricsMalayalam: "കർത്താവേ"}
	if e := s.validate(true); e != nil {
		t.Fatal(e)
	}
}

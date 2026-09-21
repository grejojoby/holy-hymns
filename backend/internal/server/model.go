package server

import (
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

type Link struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}
type Song struct {
	ID              string   `json:"id"`
	Title           string   `json:"title"`
	TitleMalayalam  string   `json:"titleMalayalam"`
	LyricsMalayalam string   `json:"lyricsMalayalam"`
	LyricsManglish  string   `json:"lyricsManglish"`
	Aliases         []string `json:"aliases"`
	Credits         string   `json:"credits"`
	Links           []Link   `json:"links"`
	CategoryIDs     []string `json:"categoryIds"`
	Featured        bool     `json:"featured"`
	Version         int      `json:"version"`
	Status          string   `json:"status"`
	UpdatedAt       string   `json:"updatedAt"`
	SourceURL       string   `json:"sourceUrl,omitempty"`
	ReviewNotes     []string `json:"reviewNotes,omitempty"`
}
type Category struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	NameMalayalam string `json:"nameMalayalam"`
	Kind          string `json:"kind"`
	Position      int    `json:"position"`
	Version       int    `json:"version"`
}
type AppConfig struct {
	AppName      string `json:"appName"`
	Announcement string `json:"announcement"`
	AboutText    string `json:"aboutText"`
	SupportEmail string `json:"supportEmail"`
	Revision     int64  `json:"revision"`
	Version      int    `json:"version"`
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(s)), " "))
}
func (s *Song) clean() {
	s.Title = strings.TrimSpace(norm.NFC.String(s.Title))
	s.TitleMalayalam = strings.TrimSpace(norm.NFC.String(s.TitleMalayalam))
	s.LyricsMalayalam = strings.TrimSpace(norm.NFC.String(strings.ReplaceAll(s.LyricsMalayalam, "\r\n", "\n")))
	s.LyricsManglish = strings.TrimSpace(norm.NFC.String(strings.ReplaceAll(s.LyricsManglish, "\r\n", "\n")))
	if s.Aliases == nil {
		s.Aliases = []string{}
	}
	if s.Links == nil {
		s.Links = []Link{}
	}
	if s.CategoryIDs == nil {
		s.CategoryIDs = []string{}
	}
	if s.ReviewNotes == nil {
		s.ReviewNotes = []string{}
	}
}
func (s Song) validate(publishing bool) error {
	if s.Title == "" && s.TitleMalayalam == "" {
		return errors.New("a song title is required")
	}
	if utf8.RuneCountInString(s.Title) > 500 || utf8.RuneCountInString(s.TitleMalayalam) > 500 {
		return errors.New("title is too long")
	}
	if len(s.LyricsMalayalam)+len(s.LyricsManglish) > 200000 {
		return errors.New("lyrics are too long")
	}
	if len(s.Aliases) > 30 || len(s.CategoryIDs) > 30 || len(s.Links) > 10 {
		return errors.New("too many aliases, categories or media links")
	}
	for _, a := range s.Aliases {
		if utf8.RuneCountInString(a) > 500 {
			return errors.New("alias is too long")
		}
	}
	for _, id := range s.CategoryIDs {
		if !validID(id) {
			return errors.New("invalid category identifier")
		}
	}
	for _, l := range s.Links {
		u, e := url.Parse(l.URL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
			return errors.New("media links must be public HTTPS URLs")
		}
	}
	if publishing && s.LyricsMalayalam == "" && s.LyricsManglish == "" {
		return errors.New("at least one lyric script is required to publish")
	}
	return nil
}
func validID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
func decodeSong(raw []byte, id string, version int, status, updated string) (Song, error) {
	var s Song
	err := json.Unmarshal(raw, &s)
	s.ID = id
	s.Version = version
	s.Status = status
	s.UpdatedAt = updated
	s.clean()
	return s, err
}

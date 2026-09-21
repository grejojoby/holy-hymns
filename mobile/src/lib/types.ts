export type Role = "reader" | "admin" | "owner";
export type User = {
  id: string;
  email: string;
  name: string;
  role: Role;
  verified: boolean;
  suspended: boolean;
};
export type SongLink = { label: string; url: string };
export type Song = {
  id: string;
  title: string;
  titleMalayalam: string;
  lyricsMalayalam: string;
  lyricsManglish: string;
  aliases: string[];
  credits: string;
  links: SongLink[];
  categoryIds: string[];
  featured: boolean;
  version: number;
  status: "draft" | "published";
  updatedAt: string;
  sourceUrl?: string;
  reviewNotes?: string[];
};
export type SongInput = Pick<
  Song,
  | "title"
  | "titleMalayalam"
  | "lyricsMalayalam"
  | "lyricsManglish"
  | "aliases"
  | "credits"
  | "links"
  | "categoryIds"
  | "featured"
>;
export type Category = {
  id: string;
  name: string;
  nameMalayalam: string;
  kind: "purpose" | "occasion" | "theme";
  position: number;
  version: number;
};
export type AppConfig = {
  appName: string;
  announcement: string;
  aboutText: string;
  supportEmail: string;
  revision: number;
  version: number;
};
export type SongRevision = {
  id: string;
  version: number;
  createdAt: string;
  content: Song;
};
export type List<T> = { items: T[]; total?: number };
export type AuthResult = { token: string; user: User };
export type AnalyticsEvent = {
  event: "song_open" | "category_open" | "search" | "favorite";
  songId?: string;
  categoryId?: string;
  resultCount?: number;
};
export type Analytics = {
  daily: { date: string; event: string; count: number }[];
  topSongs: { id: string; title: string; count: number }[];
  totals: {
    song_open: number;
    category_open: number;
    search: number;
    no_results: number;
    favorite: number;
  };
  mailAlerts?: {
    kind: string;
    message: string;
    occurrences: number;
    lastSeenAt: string;
  }[];
};
export type Preferences = {
  darkMode: "system" | "light" | "dark";
  fontSize: number;
  script: "malayalam" | "manglish";
  analytics: boolean;
  keepAwake: boolean;
};

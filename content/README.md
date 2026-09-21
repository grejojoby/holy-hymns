# Initial source snapshot

`grejo-lyrics.atom` contains 331 unique public Blogger post entries from the owner's Grejo Lyrics blog, retrieved on 19 September 2026 with explicit owner authorization.

The public feed was fetched in three pages (150, 150 and 31 posts), and their Atom entries were consolidated without changing post content. The transient pagination/self links were removed from the consolidated feed. Source IDs, original links, labels, author/credit fields and content are retained. Blogger silently caps pages at 150 even when requesting 500; use the importer's bounded pagination instead of a larger request.

Sources:

- https://grejolyrics.blogspot.com/feeds/posts/default?max-results=150
- https://grejolyrics.blogspot.com/feeds/posts/default?max-results=150&start-index=151
- https://grejolyrics.blogspot.com/feeds/posts/default?max-results=150&start-index=301

SHA-256 of the consolidated Atom file:

```text
b8d0bc13e76228f790d72099777c94d9515f68a11a788407b85ed09cae55d694
```

The reviewed parser identifies 288 draft candidates, with 46 Malayalam and 279 Manglish files (37 songs have both). It excludes 43 posts from automatic upload, retaining original HTML and review material for unsupported languages, chord notation, collections, index pages and media-only entries. See [the extracted folder and upload instructions](lyrics/README.md).

The initial broad parser identified 328 potential entries; the lower count follows content classification rather than deleted source posts. Latin-script text still needs review because a parser cannot reliably distinguish every transliteration from prose. This source material is an unpublished import queue, not an approved public song catalogue or a license grant for the underlying compositions. Review lyrics, credits, categories, links, duplicate titles and distribution permission before publishing.

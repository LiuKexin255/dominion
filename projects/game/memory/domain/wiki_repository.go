package domain

import "context"

// WikiSearchHit is one SearchWikis candidate returned by the storage-layer
// prefilter: the resource id and title text of a wiki whose indexed
// title_grams satisfied the query's $all prefilter (or whose normalized title
// matched the one-rune regex fallback). It is a superset of the final result:
// the handler verifies the exact substring and ranks the hits. Content is
// never part of a search hit (spec 068 FR-004;
// specs/068-wiki-round-pause/contracts/wiki-service.md §4.3).
type WikiSearchHit struct {
	// WikiID is the resource id (the {wiki} name segment).
	WikiID string
	// Title is the stored title text, returned as-is.
	Title string
	// TitleNorm is the title's normalized form, computed once here so the
	// handler verifies and ranks with the same normalization the prefilter
	// used.
	TitleNorm string
}

// WikiRepository defines storage operations for Wiki entities, scoped to the
// (template, session) scope key carried on each Wiki.
type WikiRepository interface {
	// CreateWiki stores a new Wiki. It returns ErrAlreadyExists if a wiki with
	// the same (template, session_id, wiki_id) or the same normalized title
	// already exists in the scope (spec 068 FR-002).
	CreateWiki(ctx context.Context, wiki *Wiki) error
	// GetWiki retrieves a Wiki by its identity. It returns ErrNotFound when no
	// wiki with the given id exists in the scope.
	GetWiki(ctx context.Context, template, session, wikiID string) (*Wiki, error)
	// UpdateWiki replaces the stored Wiki identified by wiki.WikiID under
	// (template, session), preserving the stored create_time. It returns
	// ErrNotFound when the target does not exist and ErrAlreadyExists when the
	// new title normalizes to another wiki's title in the scope.
	UpdateWiki(ctx context.Context, wiki *Wiki) (*Wiki, error)
	// DeleteWiki removes a Wiki by identity. It returns ErrNotFound when no
	// wiki with the given id exists in the scope.
	DeleteWiki(ctx context.Context, template, session, wikiID string) error
	// ListWikis retrieves a page of Wikis under a session in the fixed
	// wiki_id ascending order (spec 068 FR-003; AIP-132:
	// https://google.aip.dev/132). cursor is a DecodeWikiListPageToken
	// product; a nil cursor is the first page (the only criterion). pageSize
	// is the page upper bound; the returned token is empty on the last page
	// (AIP-158: https://google.aip.dev/158).
	ListWikis(ctx context.Context, template, session string, cursor *WikiListPageToken, pageSize int) ([]*Wiki, string, error)
	// SearchWikis returns the candidates whose indexed bigrams contain every
	// bigram of the already-normalized query q, or — for a query shorter than
	// two runes — whose normalized title matches q as a regex literal
	// (specs/068-wiki-round-pause/data-model.md §3.1). The result is an
	// unordered superset; verification, ranking and offset pagination belong
	// to the handler.
	SearchWikis(ctx context.Context, template, session, q string) ([]*WikiSearchHit, error)
}

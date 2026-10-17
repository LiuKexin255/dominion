package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	// MaxWikiTitleRunes is the maximum wiki title length in runes
	// (specs/069-wiki-template-web/data-model.md §1.1).
	MaxWikiTitleRunes = 128
	// MaxWikiContentBytes is the maximum wiki content length in bytes (64KB,
	// specs/069-wiki-template-web/data-model.md §1.1).
	MaxWikiContentBytes = 64 * 1024
	// DefaultWikiPageSize is the default ListWikis/SearchWikis page size
	// (specs/069-wiki-template-web/contracts/wiki-service.md §2/§4).
	DefaultWikiPageSize = 20
	// MaxWikiPageSize is the maximum ListWikis/SearchWikis page size; larger
	// requested values are coerced down to it (AIP-158:
	// https://google.aip.dev/158).
	MaxWikiPageSize = 100
)

// Wiki is one planner knowledge-base entry of a template (spec
// specs/069-wiki-template-web/spec.md FR-001;
// specs/069-wiki-template-web/data-model.md §1.1). The template is the single
// scope key: every session of a template shares one wiki collection, so the
// entry carries no session dimension. The wiki id is the resource id segment
// {wiki} of the resource pattern templates/{template}/wikis/{wiki} (the
// resource message carries no separate id field;
// specs/069-wiki-template-web/contracts/wiki-service.md §1). The title is the
// model-facing address; the storage layer derives its normalized,
// scope-unique form and search bigrams on write
// (specs/069-wiki-template-web/data-model.md §2.1-2.2).
type Wiki struct {
	// Template is the template path segment this wiki belongs to; it is the
	// single scope key.
	Template string
	// WikiID is the wiki resource id (the {wiki} name segment).
	WikiID string
	// Title is the model-facing address of the entry, stored as-is.
	Title string
	// Content is the knowledge body.
	Content string
	// CreateTime is the timestamp when this wiki was created.
	CreateTime time.Time
	// UpdateTime is the timestamp when this wiki was last updated.
	UpdateTime time.Time
}

// wikiIDPattern is the allowed character set for the wiki resource id (the
// {wiki} name segment), aligned with the memory id charset
// (specs/069-wiki-template-web/data-model.md §1.1). The agent generates the id
// side, the service rejects anything outside [a-z0-9_-].
var wikiIDPattern = regexp.MustCompile(`^[a-z0-9_-]+$`)

// ValidateWikiID reports whether id is a valid wiki resource id.
func ValidateWikiID(id string) error {
	if !wikiIDPattern.MatchString(id) {
		return fmt.Errorf("wiki_id %q must match %s", id, wikiIDPattern.String())
	}
	return nil
}

// ValidateWikiTitle reports whether title is a non-empty title of at most
// MaxWikiTitleRunes runes (specs/069-wiki-template-web/data-model.md §1.1).
func ValidateWikiTitle(title string) error {
	if title == "" {
		return errors.New("title is required")
	}
	if utf8.RuneCountInString(title) > MaxWikiTitleRunes {
		return fmt.Errorf("title exceeds maximum of %d runes", MaxWikiTitleRunes)
	}
	return nil
}

// ValidateWikiContent reports whether content is non-empty and at most
// MaxWikiContentBytes bytes (specs/069-wiki-template-web/data-model.md §1.1).
func ValidateWikiContent(content string) error {
	if content == "" {
		return errors.New("content is required")
	}
	if len(content) > MaxWikiContentBytes {
		return fmt.Errorf("content exceeds maximum of %d bytes", MaxWikiContentBytes)
	}
	return nil
}

// NormalizeTitle returns the template-scope normal form of a wiki title or
// search query: NFKC Unicode normalization followed by lower-casing
// (specs/069-wiki-template-web/data-model.md §1.1/§2.1). Both titles and
// search queries go through it, so matching is case- and
// full-width-insensitive; the result is the value stored in the title_norm
// index and compared against search candidates.
func NormalizeTitle(title string) string {
	return strings.ToLower(norm.NFKC.String(title))
}

// TitleBigrams returns the deduplicated rune-level bigrams of a normalized
// title or query. The storage layer indexes the bigrams of every title and
// the search prefilter matches a query's bigrams with $all
// (specs/069-wiki-template-web/contracts/wiki-service.md §4;
// survey/game-wiki-knowledge-base.md §4.3). A normalized title that contains
// a normalized query as a substring necessarily contains every bigram of the
// query, so the prefilter is a superset check; the exact substring match is
// verified separately. Inputs shorter than two runes have no bigrams.
func TitleBigrams(normalized string) []string {
	runes := []rune(normalized)
	if len(runes) < 2 {
		return nil
	}
	var grams []string
	seen := make(map[string]bool, len(runes)-1)
	for i := 0; i+1 < len(runes); i++ {
		gram := string(runes[i : i+2])
		if seen[gram] {
			continue
		}
		seen[gram] = true
		grams = append(grams, gram)
	}
	return grams
}

// WikiSearchRank is the match specificity of a search hit, ordered most
// specific first: an exact normalized title match beats a prefix match, which
// beats a substring match. Ranking is deterministic and locale-free
// (specs/069-wiki-template-web/contracts/wiki-service.md §4).
type WikiSearchRank int

const (
	// WikiSearchRankExact is the rank of a normalized title equal to the
	// normalized query. It is the zero rank, so the zero value is the most
	// specific.
	WikiSearchRankExact WikiSearchRank = iota
	// WikiSearchRankPrefix is the rank of a normalized title that starts with
	// the normalized query.
	WikiSearchRankPrefix
	// WikiSearchRankSubstring is the rank of any other normalized title
	// containing the normalized query.
	WikiSearchRankSubstring
)

// WikiSearchMatchRank returns the match rank of a normalized title against a
// normalized query. ok is false when the title does not contain the query —
// the exact substring test that filters out the bigram prefilter's false
// positives (specs/069-wiki-template-web/contracts/wiki-service.md §4).
func WikiSearchMatchRank(query, titleNorm string) (rank WikiSearchRank, ok bool) {
	if !strings.Contains(titleNorm, query) {
		return WikiSearchRankExact, false
	}
	switch {
	case titleNorm == query:
		return WikiSearchRankExact, true
	case strings.HasPrefix(titleNorm, query):
		return WikiSearchRankPrefix, true
	default:
		return WikiSearchRankSubstring, true
	}
}

package mongo

import (
	"time"

	"dominion/projects/game/memory/domain"
)

// BSON field name constants for wiki documents.
const (
	fieldWikiID     = "wiki_id"
	fieldTitle      = "title"
	fieldTitleNorm  = "title_norm"
	fieldTitleGrams = "title_grams"
	fieldContent    = "content"
	fieldCreateTime = "create_time"
)

// wikiDocument stores Wiki documents in MongoDB. The _id is left to the
// database to generate and is never overwritten by the repository
// (style/mongo.md). The unique identities are the
// (template, session_id, wiki_id) compound index and the
// (template, session_id, title_norm) unique index; title_grams backs the
// search prefilter with a multikey index. Both derived fields are computed
// from Title on write (specs/068-wiki-round-pause/data-model.md §2).
type wikiDocument struct {
	ID         interface{} `bson:"_id,omitempty"`
	Template   string      `bson:"template"`
	SessionID  string      `bson:"session_id"`
	WikiID     string      `bson:"wiki_id"`
	Title      string      `bson:"title"`
	TitleNorm  string      `bson:"title_norm"`
	TitleGrams []string    `bson:"title_grams"`
	Content    string      `bson:"content"`
	CreateTime time.Time   `bson:"create_time"`
	UpdateTime time.Time   `bson:"update_time"`
}

// toDomain converts a MongoDB document into its domain representation.
func (d *wikiDocument) toDomain() *domain.Wiki {
	if d == nil {
		return nil
	}
	return &domain.Wiki{
		Template:   d.Template,
		SessionID:  d.SessionID,
		WikiID:     d.WikiID,
		Title:      d.Title,
		Content:    d.Content,
		CreateTime: d.CreateTime,
		UpdateTime: d.UpdateTime,
	}
}

// toSearchHit converts a search-projected document into a lightweight hit.
// The projection carries title and wiki_id only, so the normalized form is
// derived from the returned title with the same domain function the prefilter
// indexed with (specs/068-wiki-round-pause/data-model.md §3.1).
func (d *wikiDocument) toSearchHit() *domain.WikiSearchHit {
	if d == nil {
		return nil
	}
	return &domain.WikiSearchHit{
		WikiID:    d.WikiID,
		Title:     d.Title,
		TitleNorm: domain.NormalizeTitle(d.Title),
	}
}

// wikiDocumentFromDomain converts a domain Wiki into its MongoDB
// representation, deriving the normalized title and its search bigrams from
// the title (specs/068-wiki-round-pause/data-model.md §2.1-2.2).
func wikiDocumentFromDomain(w *domain.Wiki) *wikiDocument {
	if w == nil {
		return nil
	}
	titleNorm := domain.NormalizeTitle(w.Title)
	return &wikiDocument{
		Template:   w.Template,
		SessionID:  w.SessionID,
		WikiID:     w.WikiID,
		Title:      w.Title,
		TitleNorm:  titleNorm,
		TitleGrams: domain.TitleBigrams(titleNorm),
		Content:    w.Content,
		CreateTime: w.CreateTime,
		UpdateTime: w.UpdateTime,
	}
}

// wikiFilter is a concrete BSON filter struct for querying a wiki by its
// (template, session_id, wiki_id) identity.
type wikiFilter struct {
	Template  string `bson:"template"`
	SessionID string `bson:"session_id"`
	WikiID    string `bson:"wiki_id"`
}

package mongo

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"dominion/projects/game/memory/domain"

	"go.mongodb.org/mongo-driver/bson"
	mongodriver "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// wikisCollectionName is the MongoDB collection name for wikis. The memory
// service owns the "game_memory" database, so wiki documents share the
// database with memories and are isolated by collection
// (specs/069-wiki-template-web/contracts/wiki-service.md §1; style/mongo.md).
const wikisCollectionName = "wikis"

// legacyWikiIndexNames are the index names of the superseded session-scoped
// scope, dropped at startup so the collection's storage state matches the
// template scope (specs/069-wiki-template-web/data-model.md §2.3). MongoDB
// names a compound index by joining its key fields with their direction, e.g.
// {template: 1, session_id: 1, wiki_id: 1} becomes
// "template_1_session_id_1_wiki_id_1"
// (https://www.mongodb.com/docs/manual/reference/method/db.collection.dropIndex/).
var legacyWikiIndexNames = []string{
	"template_1_session_id_1_wiki_id_1",
	"template_1_session_id_1_title_norm_1",
	"template_1_session_id_1_title_grams_1",
}

// wikiRepository stores Wiki entities in MongoDB.
type wikiRepository struct {
	collection collectionOps
}

// NewWikiRepository creates a MongoDB-backed WikiRepository in the given
// database, migrating the collection to the template scope and creating the
// wiki indexes at startup
// (specs/069-wiki-template-web/data-model.md §2.2-2.3).
func NewWikiRepository(client *mongodriver.Client, dbName string) (domain.WikiRepository, error) {
	return newWikiRepository(newCollection(client, dbName, wikisCollectionName))
}

// newWikiRepository migrates coll to the template scope and creates the wiki
// indexes. The migration is the clean break mandated by
// specs/069-wiki-template-web/data-model.md §2.3: drop the three legacy
// (template, session_id, ...) indexes, delete the documents still carrying
// session_id (new documents have no such field, so once migrated this delete
// matches nothing), then create the template-scoped indexes. Dropping a
// missing legacy index is the normal fresh-install case and is ignored;
// deleting the legacy documents must succeed, because leftover
// session-scoped documents would still match template-scoped queries and
// pollute the shared collection.
func newWikiRepository(coll collectionOps) (domain.WikiRepository, error) {
	ctx := context.Background()
	indexes := coll.Indexes()

	for _, name := range legacyWikiIndexNames {
		_, _ = indexes.DropOne(ctx, name)
	}

	if _, err := coll.DeleteMany(ctx, bson.M{fieldSessionID: bson.M{"$exists": true}}); err != nil {
		return nil, fmt.Errorf("delete legacy session-scoped wikis: %w", err)
	}

	for _, model := range wikiIndexModels() {
		// Ignore the duplicate-index error of a previous service start.
		_, _ = indexes.CreateOne(ctx, model)
	}

	return &wikiRepository{collection: coll}, nil
}

// wikiIndexModels returns the wiki indexes created at startup
// (specs/069-wiki-template-web/data-model.md §2.2): the unique
// (template, wiki_id) identity index backing the fixed wiki_id ascending List
// order, the unique (template, title_norm) index enforcing template-unique
// titles, and the (template, title_grams) multikey index backing the Search
// $all prefilter.
func wikiIndexModels() []mongodriver.IndexModel {
	return []mongodriver.IndexModel{
		{
			Keys: bson.D{
				{Key: fieldTemplate, Value: 1},
				{Key: fieldWikiID, Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: fieldTemplate, Value: 1},
				{Key: fieldTitleNorm, Value: 1},
			},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{
				{Key: fieldTemplate, Value: 1},
				{Key: fieldTitleGrams, Value: 1},
			},
		},
	}
}

// CreateWiki stores a new Wiki. A duplicate (template, wiki_id) or a duplicate
// template-normalized title maps to ErrAlreadyExists
// (specs/069-wiki-template-web/contracts/wiki-service.md §3).
func (r *wikiRepository) CreateWiki(ctx context.Context, wiki *domain.Wiki) error {
	doc := wikiDocumentFromDomain(wiki)

	if _, err := r.collection.InsertOne(ctx, doc); err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return domain.ErrAlreadyExists
		}
		return err
	}

	return nil
}

// GetWiki retrieves a Wiki by its template-scoped identity.
func (r *wikiRepository) GetWiki(ctx context.Context, template, wikiID string) (*domain.Wiki, error) {
	filter := wikiFilter{Template: template, WikiID: wikiID}

	doc := new(wikiDocument)
	if err := r.collection.FindOne(ctx, filter).Decode(doc); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	return doc.toDomain(), nil
}

// UpdateWiki replaces the stored Wiki identified by its (template, wiki_id)
// identity, preserving the stored create_time. The normalized title and its
// bigrams are rebuilt from the new title; a title that collides with another
// wiki in the template scope maps to ErrAlreadyExists.
func (r *wikiRepository) UpdateWiki(ctx context.Context, wiki *domain.Wiki) (*domain.Wiki, error) {
	filter := wikiFilter{Template: wiki.Template, WikiID: wiki.WikiID}

	existing := new(wikiDocument)
	if err := r.collection.FindOne(ctx, filter).Decode(existing); err != nil {
		if errors.Is(err, mongodriver.ErrNoDocuments) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}

	doc := wikiDocumentFromDomain(wiki)
	doc.ID = existing.ID
	doc.CreateTime = existing.CreateTime

	result, err := r.collection.ReplaceOne(ctx, filter, doc)
	if err != nil {
		if mongodriver.IsDuplicateKeyError(err) {
			return nil, domain.ErrAlreadyExists
		}
		return nil, err
	}
	if result.MatchedCount == 0 {
		return nil, domain.ErrNotFound
	}

	return doc.toDomain(), nil
}

// DeleteWiki removes a Wiki by identity.
func (r *wikiRepository) DeleteWiki(ctx context.Context, template, wikiID string) error {
	filter := wikiFilter{Template: template, WikiID: wikiID}
	result, err := r.collection.DeleteOne(ctx, filter)
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return domain.ErrNotFound
	}

	return nil
}

// ListWikis retrieves a page of Wikis under a template in the fixed wiki_id
// ascending order (AIP-132: https://google.aip.dev/132). A nil cursor is the
// first page; a non-nil cursor resumes with a single-key seek (wiki_id >
// cursor.WikiID). The page's last wiki id becomes the next token (AIP-158:
// https://google.aip.dev/158).
func (r *wikiRepository) ListWikis(ctx context.Context, template string, cursor *domain.WikiListPageToken, pageSize int) ([]*domain.Wiki, string, error) {
	filter := bson.M{fieldTemplate: template}
	if cursor != nil {
		filter[fieldWikiID] = bson.M{"$gt": cursor.WikiID}
	}

	opts := options.Find().
		SetSort(bson.D{{Key: fieldWikiID, Value: 1}}).
		SetLimit(int64(pageSize) + 1)

	docs, err := r.find(ctx, filter, opts)
	if err != nil {
		return nil, "", err
	}
	if len(docs) == 0 {
		return nil, "", nil
	}

	nextPageToken := ""
	if len(docs) > pageSize {
		nextPageToken = domain.EncodeWikiListPageToken(&domain.WikiListPageToken{WikiID: docs[pageSize-1].WikiID})
		docs = docs[:pageSize]
	}

	return wikiDocsToDomain(docs), nextPageToken, nil
}

// SearchWikis returns the search candidates for an already-normalized query
// (specs/069-wiki-template-web/contracts/wiki-service.md §4). A query of two
// or more runes is prefiltered with $all over the indexed title_grams (an
// indexed superset check); a shorter query cannot form a bigram and falls back
// to an unanchored, quote-escaped regex over title_norm, bounded by the
// template's entry count. Only title and wiki_id are projected — content never
// leaves storage for a search. The handler verifies the exact substring and
// ranks.
func (r *wikiRepository) SearchWikis(ctx context.Context, template, q string) ([]*domain.WikiSearchHit, error) {
	filter := bson.M{fieldTemplate: template}
	if len([]rune(q)) >= 2 {
		filter[fieldTitleGrams] = bson.M{"$all": domain.TitleBigrams(q)}
	} else {
		// QuoteMeta closes the regex-injection surface
		// (survey/game-wiki-knowledge-base.md §4.4).
		filter[fieldTitleNorm] = bson.M{"$regex": regexp.QuoteMeta(q)}
	}

	opts := options.Find().SetProjection(bson.M{fieldTitle: 1, fieldWikiID: 1})

	docs, err := r.find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}

	hits := make([]*domain.WikiSearchHit, 0, len(docs))
	for _, doc := range docs {
		hits = append(hits, doc.toSearchHit())
	}
	return hits, nil
}

// find runs the cursor query and decodes the matching wiki documents.
func (r *wikiRepository) find(ctx context.Context, filter bson.M, opts *options.FindOptions) ([]*wikiDocument, error) {
	cur, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var docs []*wikiDocument
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

// wikiDocsToDomain converts stored documents into domain wikis.
func wikiDocsToDomain(docs []*wikiDocument) []*domain.Wiki {
	wikis := make([]*domain.Wiki, 0, len(docs))
	for _, doc := range docs {
		wikis = append(wikis, doc.toDomain())
	}
	return wikis
}

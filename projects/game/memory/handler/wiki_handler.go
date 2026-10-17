package handler

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	game "dominion/projects/game"
	"dominion/projects/game/memory/domain"
	"dominion/projects/game/pkg/gameconst"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// WikiHandler implements WikiServiceServer for Wiki CRUD, listing and search
// (specs/069-wiki-template-web/spec.md FR-001~002;
// specs/069-wiki-template-web/contracts/wiki-service.md §2-4). It shares the
// memory service's error mapping (toStatusError).
type WikiHandler struct {
	game.UnimplementedWikiServiceServer

	wikiRepo domain.WikiRepository
}

// NewWikiHandler creates a new WikiHandler with the given repository.
func NewWikiHandler(wikiRepo domain.WikiRepository) *WikiHandler {
	return &WikiHandler{
		wikiRepo: wikiRepo,
	}
}

// ─── Wiki RPCs ────────────────────────────────────────────────────────────

// CreateWiki creates a Wiki under a template (AIP-133:
// https://google.aip.dev/133). The resource is embedded in the request
// (req.wiki.title / req.wiki.content) and the caller supplies wiki_id
// (REQUIRED, agent-generated per the proto contract). A title that normalizes
// to an existing title in the template scope returns ALREADY_EXISTS
// (specs/069-wiki-template-web/contracts/wiki-service.md §2-3).
func (h *WikiHandler) CreateWiki(ctx context.Context, req *game.CreateWikiRequest) (*game.Wiki, error) {
	tplName, err := game.ParseTemplateName(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(tplName); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	wikiID := req.GetWikiId()
	if err := domain.ValidateWikiID(wikiID); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	body := req.GetWiki()
	if body == nil {
		return nil, status.Error(codes.InvalidArgument, "wiki is required")
	}
	if err := domain.ValidateWikiTitle(body.GetTitle()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := domain.ValidateWikiContent(body.GetContent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	now := time.Now()
	wiki := &domain.Wiki{
		Template:   tplName.TemplateID,
		WikiID:     wikiID,
		Title:      body.GetTitle(),
		Content:    body.GetContent(),
		CreateTime: now,
		UpdateTime: now,
	}

	if err := h.wikiRepo.CreateWiki(ctx, wiki); err != nil {
		return nil, toStatusError(err)
	}

	return wikiToProto(wiki), nil
}

// GetWiki gets a Wiki by name (AIP-131: https://google.aip.dev/131) — the
// standard read path that returns content.
func (h *WikiHandler) GetWiki(ctx context.Context, req *game.GetWikiRequest) (*game.Wiki, error) {
	name, err := req.ParseName()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(name.Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	wiki, err := h.wikiRepo.GetWiki(ctx, name.TemplateID, name.WikiID)
	if err != nil {
		return nil, toStatusError(err)
	}

	return wikiToProto(wiki), nil
}

// UpdateWiki applies a partial update described by update_mask to an existing
// Wiki (AIP-134: https://google.aip.dev/134). Identity is carried on the
// resource itself (req.wiki.name). update_mask supports title and content; an
// omitted mask updates both populated fields. A title change rebuilds the
// search index while keeping template-scope uniqueness
// (specs/069-wiki-template-web/contracts/wiki-service.md §2-3); a missing
// resource errors with NOT_FOUND and an unknown mask path with
// INVALID_ARGUMENT.
func (h *WikiHandler) UpdateWiki(ctx context.Context, req *game.UpdateWikiRequest) (*game.Wiki, error) {
	body := req.GetWiki()
	if body == nil {
		return nil, status.Error(codes.InvalidArgument, "wiki is required")
	}
	name, err := game.ParseWikiName(body.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(name.Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	updateTitle, updateContent, err := applyWikiMask(req.GetUpdateMask())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	// Validate the submitted fields before the existence lookup, so an
	// invalid request is rejected regardless of whether the target exists.
	if updateTitle {
		if err := domain.ValidateWikiTitle(body.GetTitle()); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}
	if updateContent {
		if err := domain.ValidateWikiContent(body.GetContent()); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
	}

	existing, err := h.wikiRepo.GetWiki(ctx, name.TemplateID, name.WikiID)
	if err != nil {
		return nil, toStatusError(err)
	}

	wiki := *existing
	if updateTitle {
		wiki.Title = body.GetTitle()
	}
	if updateContent {
		wiki.Content = body.GetContent()
	}
	wiki.UpdateTime = time.Now()

	persisted, err := h.wikiRepo.UpdateWiki(ctx, &wiki)
	if err != nil {
		return nil, toStatusError(err)
	}

	return wikiToProto(persisted), nil
}

// DeleteWiki deletes a Wiki by name (AIP-135: https://google.aip.dev/135). A
// missing resource errors with NOT_FOUND (no allow_missing).
func (h *WikiHandler) DeleteWiki(ctx context.Context, req *game.DeleteWikiRequest) (*emptypb.Empty, error) {
	name, err := req.ParseName()
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(name.Parent()); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	if err := h.wikiRepo.DeleteWiki(ctx, name.TemplateID, name.WikiID); err != nil {
		return nil, toStatusError(err)
	}
	return new(emptypb.Empty), nil
}

// ListWikis retrieves a paginated list of full Wiki resources under a
// template in the fixed wiki_id ascending order
// (specs/069-wiki-template-web/contracts/wiki-service.md §2; AIP-132:
// https://google.aip.dev/132; AIP-158: https://google.aip.dev/158). There is
// no order_by; page_size defaults to domain.DefaultWikiPageSize and values
// above domain.MaxWikiPageSize are coerced down to it.
func (h *WikiHandler) ListWikis(ctx context.Context, req *game.ListWikisRequest) (*game.ListWikisResponse, error) {
	tplName, err := game.ParseTemplateName(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(tplName); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	pageSize, err := wikiPageSize(req.GetPageSize())
	if err != nil {
		return nil, err
	}

	var cursor *domain.WikiListPageToken
	if token := req.GetPageToken(); token != "" {
		cursor, err = domain.DecodeWikiListPageToken(token)
		if err != nil {
			return nil, toStatusError(err)
		}
	}

	wikis, nextPageToken, err := h.wikiRepo.ListWikis(ctx, tplName.TemplateID, cursor, pageSize)
	if err != nil {
		return nil, toStatusError(err)
	}

	protos := make([]*game.Wiki, 0, len(wikis))
	for _, wiki := range wikis {
		protos = append(protos, wikiToProto(wiki))
	}

	return &game.ListWikisResponse{
		Wikis:         protos,
		NextPageToken: nextPageToken,
	}, nil
}

// SearchWikis searches Wikis by normalized title substring
// (specs/069-wiki-template-web/contracts/wiki-service.md §4; AIP-136 custom
// method: https://google.aip.dev/136). The query is normalized (NFKC +
// lower-case) and must be non-empty; the repository returns the bigram
// prefilter candidates, which this handler verifies with an exact substring
// test, ranks (exact > prefix > substring, then normalized title ascending)
// and slices by the page token's offset. Entries are lightweight
// ({name, title}, never content).
func (h *WikiHandler) SearchWikis(ctx context.Context, req *game.SearchWikisRequest) (*game.SearchWikisResponse, error) {
	tplName, err := game.ParseTemplateName(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := gameconst.ValidateTemplateName(tplName); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	pageSize, err := wikiPageSize(req.GetPageSize())
	if err != nil {
		return nil, err
	}

	query := domain.NormalizeTitle(req.GetQuery())
	if query == "" {
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}

	offset := 0
	if token := req.GetPageToken(); token != "" {
		cursor, err := domain.DecodeWikiSearchPageToken(token, query)
		if err != nil {
			return nil, toStatusError(err)
		}
		offset = cursor.Offset
	}

	hits, err := h.wikiRepo.SearchWikis(ctx, tplName.TemplateID, query)
	if err != nil {
		return nil, toStatusError(err)
	}

	matches := make([]wikiSearchMatch, 0, len(hits))
	for _, hit := range hits {
		rank, ok := domain.WikiSearchMatchRank(query, hit.TitleNorm)
		if !ok {
			continue
		}
		matches = append(matches, wikiSearchMatch{hit: hit, rank: rank})
	}
	slices.SortStableFunc(matches, func(a, b wikiSearchMatch) int {
		if a.rank != b.rank {
			return cmp.Compare(a.rank, b.rank)
		}
		return strings.Compare(a.hit.TitleNorm, b.hit.TitleNorm)
	})

	if offset > len(matches) {
		offset = len(matches)
	}
	end := offset + pageSize
	if end > len(matches) {
		end = len(matches)
	}
	page := matches[offset:end]

	nextPageToken := ""
	if end < len(matches) {
		nextPageToken = domain.EncodeWikiSearchPageToken(&domain.WikiSearchPageToken{Query: query, Offset: end})
	}

	summaries := make([]*game.WikiSummary, 0, len(page))
	for _, match := range page {
		summaries = append(summaries, wikiSummaryToProto(tplName.TemplateID, match.hit))
	}

	return &game.SearchWikisResponse{
		Wikis:         summaries,
		NextPageToken: nextPageToken,
	}, nil
}

// ─── Validation helpers ───────────────────────────────────────────────────

// wikiMaskFields enumerates the writable Wiki fields addressable via
// update_mask (AIP-161: https://google.aip.dev/161): title and content
// (specs/069-wiki-template-web/contracts/wiki-service.md §2).
var wikiMaskFields = []string{"title", "content"}

// applyWikiMask validates update_mask against the writable Wiki fields and
// reports which of title/content the update applies. A nil or empty mask is
// treated as "all populated fields", i.e. both (AIP-134:
// https://google.aip.dev/134).
func applyWikiMask(mask *fieldmaskpb.FieldMask) (updateTitle, updateContent bool, err error) {
	paths := mask.GetPaths()
	if len(paths) == 0 {
		return true, true, nil
	}

	for _, path := range paths {
		if !slices.Contains(wikiMaskFields, path) {
			return false, false, fmt.Errorf("update_mask path %q is not a writable Wiki field", path)
		}
	}

	return slices.Contains(paths, "title"), slices.Contains(paths, "content"), nil
}

// wikiPageSize resolves an AIP-158 page_size (https://google.aip.dev/158):
// zero takes the default, values above the maximum are coerced down to it, and
// a negative value is INVALID_ARGUMENT.
func wikiPageSize(pageSize int32) (int, error) {
	if pageSize < 0 {
		return 0, status.Error(codes.InvalidArgument, "page_size must not be negative")
	}
	if pageSize == 0 {
		return domain.DefaultWikiPageSize, nil
	}
	if pageSize > domain.MaxWikiPageSize {
		return domain.MaxWikiPageSize, nil
	}
	return int(pageSize), nil
}

// ─── Conversion helpers ───────────────────────────────────────────────────

// wikiSearchMatch pairs a verified search candidate with its match rank for
// deterministic ordering.
type wikiSearchMatch struct {
	hit  *domain.WikiSearchHit
	rank domain.WikiSearchRank
}

// wikiToProto converts a domain Wiki to a proto Wiki. The resource carries no
// separate id field: the wiki id is the {wiki} terminal segment of the
// resource name (specs/069-wiki-template-web/contracts/wiki-service.md §1).
func wikiToProto(w *domain.Wiki) *game.Wiki {
	if w == nil {
		return nil
	}

	pb := &game.Wiki{
		Name:    game.WikiName{TemplateID: w.Template, WikiID: w.WikiID}.String(),
		Title:   w.Title,
		Content: w.Content,
	}
	if !w.CreateTime.IsZero() {
		pb.CreateTime = timestamppb.New(w.CreateTime)
	}
	if !w.UpdateTime.IsZero() {
		pb.UpdateTime = timestamppb.New(w.UpdateTime)
	}

	return pb
}

// wikiSummaryToProto converts a search hit into the lightweight {name, title}
// entry (content is never returned by search; contract §4.3).
func wikiSummaryToProto(template string, hit *domain.WikiSearchHit) *game.WikiSummary {
	if hit == nil {
		return nil
	}
	return &game.WikiSummary{
		Name:  game.WikiName{TemplateID: template, WikiID: hit.WikiID}.String(),
		Title: hit.Title,
	}
}

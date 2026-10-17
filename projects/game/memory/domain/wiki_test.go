package domain

import (
	"strings"
	"testing"
)

func TestNormalizeTitle(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{
			name:  "chinese title stays unchanged",
			title: "策略复盘",
			want:  "策略复盘",
		},
		{
			name:  "full-width letters and digits normalize to half-width",
			title: "ＡＢＣ１２３",
			want:  "abc123",
		},
		{
			name:  "upper-case folds to lower-case",
			title: "ABC def",
			want:  "abc def",
		},
		{
			name:  "mixed full-width and case folds to lower-case ascii",
			title: "Ａｂｃ",
			want:  "abc",
		},
		{
			name:  "full-width punctuation normalizes under NFKC",
			title: "（策略）",
			want:  "(策略)",
		},
		{
			name:  "emoji is preserved and text folds",
			title: "😀 Strategy",
			want:  "😀 strategy",
		},
		{
			name:  "combining sequence composes under NFKC",
			title: "e\u0301",
			want:  "é",
		},
		{
			name:  "empty title stays empty",
			title: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := NormalizeTitle(tt.title)

			// then
			if got != tt.want {
				t.Fatalf("NormalizeTitle(%q) = %q, want %q", tt.title, got, tt.want)
			}
		})
	}
}

func TestTitleBigrams(t *testing.T) {
	tests := []struct {
		name       string
		normalized string
		want       []string
	}{
		{
			name:       "empty has no bigrams",
			normalized: "",
			want:       nil,
		},
		{
			name:       "single rune has no bigrams",
			normalized: "a",
			want:       nil,
		},
		{
			name:       "two runes yield one bigram",
			normalized: "ab",
			want:       []string{"ab"},
		},
		{
			name:       "three runes yield two bigrams",
			normalized: "abc",
			want:       []string{"ab", "bc"},
		},
		{
			name:       "repeated runes deduplicate",
			normalized: "aaa",
			want:       []string{"aa"},
		},
		{
			name:       "chinese title yields rune bigrams",
			normalized: "中文标题",
			want:       []string{"中文", "文标", "标题"},
		},
		{
			name:       "emoji bigram is one rune pair",
			normalized: "😀😀",
			want:       []string{"😀😀"},
		},
		{
			name:       "mixed scripts stay rune aligned",
			normalized: "a中b",
			want:       []string{"a中", "中b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got := TitleBigrams(tt.normalized)

			// then
			assertStringsEqual(t, got, tt.want)
		})
	}
}

func TestValidateWikiID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{name: "lower-case letters digits hyphen and underscore", id: "wiki-1_ab"},
		{name: "digits only", id: "123"},
		{name: "upper-case is rejected", id: "Wiki-1", wantErr: true},
		{name: "slash is rejected", id: "wiki/1", wantErr: true},
		{name: "dot is rejected", id: "wiki.1", wantErr: true},
		{name: "empty is rejected", id: "", wantErr: true},
		{name: "non-ascii is rejected", id: "维基", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateWikiID(tt.id)

			// then
			assertErrorPresence(t, err, tt.wantErr)
		})
	}
}

func TestValidateWikiTitle(t *testing.T) {
	tests := []struct {
		name    string
		title   string
		wantErr bool
	}{
		{name: "non-empty title is accepted", title: "策略复盘"},
		{name: "title at the rune limit is accepted", title: strings.Repeat("中", MaxWikiTitleRunes)},
		{name: "title over the rune limit is rejected", title: strings.Repeat("中", MaxWikiTitleRunes+1), wantErr: true},
		{name: "empty title is rejected", title: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateWikiTitle(tt.title)

			// then
			assertErrorPresence(t, err, tt.wantErr)
		})
	}
}

func TestValidateWikiContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "non-empty content is accepted", content: "知识正文"},
		{name: "content at the byte limit is accepted", content: strings.Repeat("x", MaxWikiContentBytes)},
		{name: "content over the byte limit is rejected", content: strings.Repeat("x", MaxWikiContentBytes+1), wantErr: true},
		{name: "empty content is rejected", content: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			err := ValidateWikiContent(tt.content)

			// then
			assertErrorPresence(t, err, tt.wantErr)
		})
	}
}

func TestWikiSearchMatchRank(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		title    string
		wantRank WikiSearchRank
		wantOK   bool
	}{
		{name: "exact match ranks first", query: "策略", title: "策略", wantRank: WikiSearchRankExact, wantOK: true},
		{name: "prefix match ranks second", query: "策略", title: "策略复盘", wantRank: WikiSearchRankPrefix, wantOK: true},
		{name: "substring match ranks third", query: "策略", title: "中期策略复盘", wantRank: WikiSearchRankSubstring, wantOK: true},
		{name: "missing query is not a match", query: "策略", title: "开局", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			rank, ok := WikiSearchMatchRank(tt.query, tt.title)

			// then
			if ok != tt.wantOK {
				t.Fatalf("WikiSearchMatchRank(%q, %q) ok = %v, want %v", tt.query, tt.title, ok, tt.wantOK)
			}
			if ok && rank != tt.wantRank {
				t.Fatalf("WikiSearchMatchRank(%q, %q) rank = %d, want %d", tt.query, tt.title, rank, tt.wantRank)
			}
		})
	}
}

// assertStringsEqual asserts got equals want element for element (nil and
// empty compare equal through length).
func assertStringsEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d] = %q, want %q (full: %v, want %v)", i, got[i], want[i], got, want)
		}
	}
}

// assertErrorPresence asserts err is non-nil exactly when wantErr is set.
func assertErrorPresence(t *testing.T, err error, wantErr bool) {
	t.Helper()
	if wantErr && err == nil {
		t.Fatal("expected error, got nil")
	}
	if !wantErr && err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

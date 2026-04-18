package storage

import (
	"context"
	"errors"
	"testing"

	mongodemo "dominion/experimental/golang/mongo_demo"

	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestParseResourceName(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantParent string
		wantID     string
		wantErr    bool
	}{
		{
			name:       "valid resource name",
			input:      "apps/myapp/mongoRecords/rec1",
			wantParent: "apps/myapp",
			wantID:     "rec1",
		},
		{
			name:    "missing record id",
			input:   "apps/myapp/mongoRecords/",
			wantErr: true,
		},
		{
			name:    "invalid format",
			input:   "invalid",
			wantErr: true,
		},
		{
			name:    "too many segments",
			input:   "apps/myapp/mongoRecords/rec1/extra",
			wantErr: true,
		},
		{
			name:    "missing app name",
			input:   "apps//mongoRecords/rec1",
			wantErr: true,
		},
		{
			name:    "wrong collection name",
			input:   "apps/myapp/otherRecords/rec1",
			wantErr: true,
		},
		{
			name:    "wrong prefix",
			input:   "projects/myapp/mongoRecords/rec1",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			parent, id, err := ParseResourceName(tt.input)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseResourceName(%q) expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseResourceName(%q) unexpected error: %v", tt.input, err)
			}
			if parent != tt.wantParent {
				t.Fatalf("ParseResourceName(%q) parent = %q, want %q", tt.input, parent, tt.wantParent)
			}
			if id != tt.wantID {
				t.Fatalf("ParseResourceName(%q) id = %q, want %q", tt.input, id, tt.wantID)
			}
		})
	}
}

func TestEncodeDecodePageToken(t *testing.T) {
	tests := []struct {
		name  string
		skip  int
		token string
	}{
		{name: "zero skip", skip: 0},
		{name: "skip 100", skip: 100},
		{name: "skip 1", skip: 1},
		{name: "large skip", skip: 9999},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// given
			encoded := EncodePageToken(tt.skip)

			// when
			decoded, err := DecodePageToken(encoded)

			// then
			if err != nil {
				t.Fatalf("DecodePageToken(%q) unexpected error: %v", encoded, err)
			}
			if decoded != tt.skip {
				t.Fatalf("DecodePageToken(EncodePageToken(%d)) = %d, want %d", tt.skip, decoded, tt.skip)
			}
		})
	}
}

func TestDecodePageToken(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		want    int
		wantErr bool
	}{
		{
			name:  "empty token returns zero",
			token: "",
			want:  0,
		},
		{
			name:    "invalid token returns error",
			token:   "invalid-token",
			wantErr: true,
		},
		{
			name:    "non-numeric base64 returns error",
			token:   EncodePageToken(0)[:4] + "!!",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// when
			got, err := DecodePageToken(tt.token)

			// then
			if tt.wantErr {
				if err == nil {
					t.Fatalf("DecodePageToken(%q) expected error, got nil", tt.token)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodePageToken(%q) unexpected error: %v", tt.token, err)
			}
			if got != tt.want {
				t.Fatalf("DecodePageToken(%q) = %d, want %d", tt.token, got, tt.want)
			}
		})
	}
}

func TestFakeStore_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("success with timestamps", func(t *testing.T) {
		// given
		store := NewFakeStore()
		record := &mongodemo.MongoRecord{
			Name:        "apps/myapp/mongoRecords/rec1",
			Title:       "Test Record",
			Description: "A test record",
		}

		// when
		got, err := store.Create(ctx, record)

		// then
		if err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}
		if got.GetName() != record.GetName() {
			t.Fatalf("Create() name = %q, want %q", got.GetName(), record.GetName())
		}
		if got.GetTitle() != record.GetTitle() {
			t.Fatalf("Create() title = %q, want %q", got.GetTitle(), record.GetTitle())
		}
		if got.GetCreateTime() == nil {
			t.Fatalf("Create() create_time is nil, expected timestamp")
		}
		if got.GetUpdateTime() == nil {
			t.Fatalf("Create() update_time is nil, expected timestamp")
		}
	})

	t.Run("duplicate returns ErrAlreadyExists", func(t *testing.T) {
		// given
		store := NewFakeStore()
		record := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "First",
		}
		if _, err := store.Create(ctx, record); err != nil {
			t.Fatalf("Create() first insert unexpected error: %v", err)
		}

		// when
		_, err := store.Create(ctx, record)

		// then
		if err == nil {
			t.Fatalf("Create() duplicate expected error, got nil")
		}
		if !errors.Is(err, ErrAlreadyExists) {
			t.Fatalf("Create() error = %v, want ErrAlreadyExists", err)
		}
	})

	t.Run("timestamps are set on create", func(t *testing.T) {
		// given
		store := NewFakeStore()
		record := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Timestamps",
		}

		// when
		got, err := store.Create(ctx, record)

		// then
		if err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}
		if got.GetCreateTime() == nil || got.GetUpdateTime() == nil {
			t.Fatalf("Create() timestamps not set: create_time=%v, update_time=%v", got.GetCreateTime(), got.GetUpdateTime())
		}
	})
}

func TestFakeStore_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("existing record", func(t *testing.T) {
		// given
		store := NewFakeStore()
		record := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Found",
		}
		if _, err := store.Create(ctx, record); err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}

		// when
		got, err := store.Get(ctx, "apps/myapp/mongoRecords/rec1")

		// then
		if err != nil {
			t.Fatalf("Get() unexpected error: %v", err)
		}
		if got.GetName() != record.GetName() {
			t.Fatalf("Get() name = %q, want %q", got.GetName(), record.GetName())
		}
		if got.GetTitle() != record.GetTitle() {
			t.Fatalf("Get() title = %q, want %q", got.GetTitle(), record.GetTitle())
		}
	})

	t.Run("non-existing record returns ErrNotFound", func(t *testing.T) {
		// given
		store := NewFakeStore()

		// when
		_, err := store.Get(ctx, "apps/myapp/mongoRecords/nonexistent")

		// then
		if err == nil {
			t.Fatalf("Get() expected error, got nil")
		}
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get() error = %v, want ErrNotFound", err)
		}
	})
}

func TestFakeStore_List(t *testing.T) {
	ctx := context.Background()

	t.Run("filter by parent", func(t *testing.T) {
		// given
		store := NewFakeStore()
		records := []*mongodemo.MongoRecord{
			{Name: "apps/app1/mongoRecords/rec1", Title: "A1-1"},
			{Name: "apps/app1/mongoRecords/rec2", Title: "A1-2"},
			{Name: "apps/app2/mongoRecords/rec3", Title: "A2-1"},
		}
		for _, r := range records {
			if _, err := store.Create(ctx, r); err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
		}

		// when
		got, _, err := store.List(ctx, "apps/app1", 0, "", false)

		// then
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("List() returned %d records, want 2", len(got))
		}
		for _, r := range got {
			if r.GetName() != "apps/app1/mongoRecords/rec1" && r.GetName() != "apps/app1/mongoRecords/rec2" {
				t.Fatalf("List() returned record with name %q, not under parent apps/app1", r.GetName())
			}
		}
	})

	t.Run("exclude archived records", func(t *testing.T) {
		// given
		store := NewFakeStore()
		records := []*mongodemo.MongoRecord{
			{Name: "apps/myapp/mongoRecords/rec1", Title: "Active", Archived: false},
			{Name: "apps/myapp/mongoRecords/rec2", Title: "Archived", Archived: true},
		}
		for _, r := range records {
			if _, err := store.Create(ctx, r); err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
		}

		// when
		got, _, err := store.List(ctx, "apps/myapp", 0, "", false)

		// then
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("List() returned %d records, want 1", len(got))
		}
		if got[0].GetName() != "apps/myapp/mongoRecords/rec1" {
			t.Fatalf("List() returned %q, want apps/myapp/mongoRecords/rec1", got[0].GetName())
		}
	})

	t.Run("include archived records when showArchived=true", func(t *testing.T) {
		// given
		store := NewFakeStore()
		records := []*mongodemo.MongoRecord{
			{Name: "apps/myapp/mongoRecords/rec1", Title: "Active", Archived: false},
			{Name: "apps/myapp/mongoRecords/rec2", Title: "Archived", Archived: true},
		}
		for _, r := range records {
			if _, err := store.Create(ctx, r); err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
		}

		// when
		got, _, err := store.List(ctx, "apps/myapp", 0, "", true)

		// then
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("List() returned %d records, want 2", len(got))
		}
	})

	t.Run("pagination with pageSize=2", func(t *testing.T) {
		// given
		store := NewFakeStore()
		for i := 0; i < 5; i++ {
			record := &mongodemo.MongoRecord{
				Name:  "apps/myapp/mongoRecords/rec" + string(rune('0'+i)),
				Title: "Record " + string(rune('0'+i)),
			}
			if _, err := store.Create(ctx, record); err != nil {
				t.Fatalf("Create() unexpected error: %v", err)
			}
		}

		// when - first page
		page1, nextToken, err := store.List(ctx, "apps/myapp", 2, "", false)

		// then
		if err != nil {
			t.Fatalf("List() page 1 unexpected error: %v", err)
		}
		if len(page1) != 2 {
			t.Fatalf("List() page 1 returned %d records, want 2", len(page1))
		}
		if nextToken == "" {
			t.Fatalf("List() page 1 next_token is empty, expected token")
		}

		// when - second page
		page2, nextToken2, err := store.List(ctx, "apps/myapp", 2, nextToken, false)

		// then
		if err != nil {
			t.Fatalf("List() page 2 unexpected error: %v", err)
		}
		if len(page2) != 2 {
			t.Fatalf("List() page 2 returned %d records, want 2", len(page2))
		}
		if nextToken2 == "" {
			t.Fatalf("List() page 2 next_token is empty, expected token")
		}

		// when - third page (last)
		page3, nextToken3, err := store.List(ctx, "apps/myapp", 2, nextToken2, false)

		// then
		if err != nil {
			t.Fatalf("List() page 3 unexpected error: %v", err)
		}
		if len(page3) != 1 {
			t.Fatalf("List() page 3 returned %d records, want 1", len(page3))
		}
		if nextToken3 != "" {
			t.Fatalf("List() page 3 next_token = %q, want empty", nextToken3)
		}
	})

	t.Run("empty parent returns nil", func(t *testing.T) {
		// given
		store := NewFakeStore()
		record := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Record",
		}
		if _, err := store.Create(ctx, record); err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}

		// when
		got, nextToken, err := store.List(ctx, "apps/other", 0, "", false)

		// then
		if err != nil {
			t.Fatalf("List() unexpected error: %v", err)
		}
		if got != nil {
			t.Fatalf("List() returned %v, want nil", got)
		}
		if nextToken != "" {
			t.Fatalf("List() next_token = %q, want empty", nextToken)
		}
	})
}

func TestFakeStore_Update(t *testing.T) {
	ctx := context.Background()

	t.Run("update title with mask", func(t *testing.T) {
		// given
		store := NewFakeStore()
		original := &mongodemo.MongoRecord{
			Name:        "apps/myapp/mongoRecords/rec1",
			Title:       "Original Title",
			Description: "Original Description",
		}
		created, err := store.Create(ctx, original)
		if err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}
		originalCreateTime := created.GetCreateTime()

		update := &mongodemo.MongoRecord{
			Name:        "apps/myapp/mongoRecords/rec1",
			Title:       "Updated Title",
			Description: "Should Not Change",
		}
		mask := &fieldmaskpb.FieldMask{Paths: []string{"title"}}

		// when
		got, err := store.Update(ctx, update, mask)

		// then
		if err != nil {
			t.Fatalf("Update() unexpected error: %v", err)
		}
		if got.GetTitle() != "Updated Title" {
			t.Fatalf("Update() title = %q, want %q", got.GetTitle(), "Updated Title")
		}
		if got.GetDescription() != "Original Description" {
			t.Fatalf("Update() description = %q, want %q", got.GetDescription(), "Original Description")
		}
		if got.GetCreateTime().GetSeconds() != originalCreateTime.GetSeconds() {
			t.Fatalf("Update() create_time changed after update")
		}
	})

	t.Run("update non-existing returns ErrNotFound", func(t *testing.T) {
		// given
		store := NewFakeStore()
		update := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/nonexistent",
			Title: "Updated",
		}
		mask := &fieldmaskpb.FieldMask{Paths: []string{"title"}}

		// when
		_, err := store.Update(ctx, update, mask)

		// then
		if err == nil {
			t.Fatalf("Update() expected error, got nil")
		}
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Update() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("empty mask returns error", func(t *testing.T) {
		// given
		store := NewFakeStore()
		original := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Original",
		}
		if _, err := store.Create(ctx, original); err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}

		update := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Updated",
		}
		mask := &fieldmaskpb.FieldMask{Paths: nil}

		// when
		_, err := store.Update(ctx, update, mask)

		// then
		if err == nil {
			t.Fatalf("Update() expected error for empty mask, got nil")
		}
	})

	t.Run("update profile.owner with nested mask", func(t *testing.T) {
		// given
		store := NewFakeStore()
		original := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Original",
		}
		if _, err := store.Create(ctx, original); err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}

		update := &mongodemo.MongoRecord{
			Name: "apps/myapp/mongoRecords/rec1",
			Profile: &mongodemo.MongoRecordProfile{
				Owner:    "new-owner",
				Priority: 99,
			},
		}
		mask := &fieldmaskpb.FieldMask{Paths: []string{"profile.owner"}}

		// when
		got, err := store.Update(ctx, update, mask)

		// then
		if err != nil {
			t.Fatalf("Update() unexpected error: %v", err)
		}
		if got.GetProfile().GetOwner() != "new-owner" {
			t.Fatalf("Update() profile.owner = %q, want %q", got.GetProfile().GetOwner(), "new-owner")
		}
		// Priority should not change since it wasn't in the mask.
		// Profile was nil, so only owner was set via the nested mask.
		if got.GetProfile().GetPriority() != 0 {
			t.Fatalf("Update() profile.priority = %d, want 0 (unchanged)", got.GetProfile().GetPriority())
		}
	})

	t.Run("update profile.owner when profile already exists", func(t *testing.T) {
		// given
		store := NewFakeStore()
		original := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Original",
			Profile: &mongodemo.MongoRecordProfile{
				Owner:    "old-owner",
				Priority: 5,
				Watchers: []string{"watcher1"},
			},
		}
		if _, err := store.Create(ctx, original); err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}

		update := &mongodemo.MongoRecord{
			Name: "apps/myapp/mongoRecords/rec1",
			Profile: &mongodemo.MongoRecordProfile{
				Owner: "new-owner",
			},
		}
		mask := &fieldmaskpb.FieldMask{Paths: []string{"profile.owner"}}

		// when
		got, err := store.Update(ctx, update, mask)

		// then
		if err != nil {
			t.Fatalf("Update() unexpected error: %v", err)
		}
		if got.GetProfile().GetOwner() != "new-owner" {
			t.Fatalf("Update() profile.owner = %q, want %q", got.GetProfile().GetOwner(), "new-owner")
		}
		if got.GetProfile().GetPriority() != 5 {
			t.Fatalf("Update() profile.priority = %d, want 5 (unchanged)", got.GetProfile().GetPriority())
		}
		if len(got.GetProfile().GetWatchers()) != 1 || got.GetProfile().GetWatchers()[0] != "watcher1" {
			t.Fatalf("Update() profile.watchers changed unexpectedly: %v", got.GetProfile().GetWatchers())
		}
	})
}

func TestFakeStore_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("delete existing record", func(t *testing.T) {
		// given
		store := NewFakeStore()
		record := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "To Delete",
		}
		if _, err := store.Create(ctx, record); err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}

		// when
		err := store.Delete(ctx, "apps/myapp/mongoRecords/rec1")

		// then
		if err != nil {
			t.Fatalf("Delete() unexpected error: %v", err)
		}
	})

	t.Run("delete non-existing returns ErrNotFound", func(t *testing.T) {
		// given
		store := NewFakeStore()

		// when
		err := store.Delete(ctx, "apps/myapp/mongoRecords/nonexistent")

		// then
		if err == nil {
			t.Fatalf("Delete() expected error, got nil")
		}
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("delete then get returns ErrNotFound", func(t *testing.T) {
		// given
		store := NewFakeStore()
		record := &mongodemo.MongoRecord{
			Name:  "apps/myapp/mongoRecords/rec1",
			Title: "Delete Then Get",
		}
		if _, err := store.Create(ctx, record); err != nil {
			t.Fatalf("Create() unexpected error: %v", err)
		}

		// when
		if err := store.Delete(ctx, "apps/myapp/mongoRecords/rec1"); err != nil {
			t.Fatalf("Delete() unexpected error: %v", err)
		}

		// then
		_, err := store.Get(ctx, "apps/myapp/mongoRecords/rec1")
		if err == nil {
			t.Fatalf("Get() after delete expected error, got nil")
		}
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get() after delete error = %v, want ErrNotFound", err)
		}
	})
}

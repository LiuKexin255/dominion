// Package storage defines MongoDB storage models for mongo_demo.
package storage

import (
	mongodemo "dominion/experimental/mongo_demo"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// mongoRecord stores MongoRecord documents in MongoDB.
type mongoRecord struct {
	Name        string            `bson:"name"`
	App         string            `bson:"app"`
	Title       string            `bson:"title"`
	Description string            `bson:"description"`
	Labels      map[string]string `bson:"labels"`
	Tags        []string          `bson:"tags"`
	Profile     *mongoProfile     `bson:"profile"`
	Archived    bool              `bson:"archived"`
	CreateTime  time.Time         `bson:"create_time"`
	UpdateTime  time.Time         `bson:"update_time"`
}

// mongoProfile stores MongoRecordProfile as a nested MongoDB document.
type mongoProfile struct {
	Owner    string   `bson:"owner"`
	Priority int32    `bson:"priority"`
	Watchers []string `bson:"watchers"`
}

// toProto converts a MongoDB record into its proto representation.
func (r *mongoRecord) toProto() *mongodemo.MongoRecord {
	if r == nil {
		return nil
	}

	p := &mongodemo.MongoRecord{
		Name:        r.Name,
		Title:       r.Title,
		Description: r.Description,
		Labels:      cloneStringMap(r.Labels),
		Tags:        cloneStringSlice(r.Tags),
		Archived:    r.Archived,
	}
	if r.Profile != nil {
		p.Profile = &mongodemo.MongoRecordProfile{
			Owner:    r.Profile.Owner,
			Priority: r.Profile.Priority,
			Watchers: cloneStringSlice(r.Profile.Watchers),
		}
	}
	if !r.CreateTime.IsZero() {
		p.CreateTime = timestamppb.New(r.CreateTime)
	}
	if !r.UpdateTime.IsZero() {
		p.UpdateTime = timestamppb.New(r.UpdateTime)
	}

	return p
}

// mongoRecordFromProto converts a proto record into its MongoDB representation.
func mongoRecordFromProto(p *mongodemo.MongoRecord) *mongoRecord {
	if p == nil {
		return nil
	}

	r := &mongoRecord{
		Name:        p.GetName(),
		Title:       p.GetTitle(),
		Description: p.GetDescription(),
		Labels:      cloneStringMap(p.GetLabels()),
		Tags:        cloneStringSlice(p.GetTags()),
		Archived:    p.GetArchived(),
		CreateTime:  mongoDateTimeFromProto(p.GetCreateTime()),
		UpdateTime:  mongoDateTimeFromProto(p.GetUpdateTime()),
	}
	if parent, _, err := ParseResourceName(p.GetName()); err == nil {
		r.App = parent
	}
	if profile := p.GetProfile(); profile != nil {
		r.Profile = &mongoProfile{
			Owner:    profile.GetOwner(),
			Priority: profile.GetPriority(),
			Watchers: cloneStringSlice(profile.GetWatchers()),
		}
	}

	return r
}

func mongoDateTimeFromProto(t *timestamppb.Timestamp) time.Time {
	if t == nil {
		return time.Time{}
	}

	return t.AsTime()
}

func cloneStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}

	cloned := make(map[string]string)
	for key, value := range m {
		cloned[key] = value
	}

	return cloned
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	cloned := make([]string, len(values))
	copy(cloned, values)

	return cloned
}

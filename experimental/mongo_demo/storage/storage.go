// Package storage defines the storage contract and helpers for Mongo demo records.
package storage

import (
	"context"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"

	mongodemo "dominion/experimental/mongo_demo"

	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

var (
	// ErrNotFound indicates the requested record does not exist.
	ErrNotFound = errors.New("record not found")
	// ErrAlreadyExists indicates the record already exists.
	ErrAlreadyExists = errors.New("record already exists")
)

const (
	// DefaultPageSize is the default number of records returned by list operations.
	DefaultPageSize = 20
	// MaxPageSize is the maximum number of records returned by list operations.
	MaxPageSize = 100
)

const (
	// DatabaseName is the MongoDB database name used by the demo.
	DatabaseName = "mongo_demo"
	// CollectionName is the MongoDB collection name used by the demo.
	CollectionName = "mongo_records"
)

// MongoRecordStore defines storage operations for Mongo demo records.
type MongoRecordStore interface {
	Create(ctx context.Context, record *mongodemo.MongoRecord) (*mongodemo.MongoRecord, error)
	Get(ctx context.Context, name string) (*mongodemo.MongoRecord, error)
	List(ctx context.Context, parent string, pageSize int32, pageToken string, showArchived bool) ([]*mongodemo.MongoRecord, string, error)
	Update(ctx context.Context, record *mongodemo.MongoRecord, updateMask *fieldmaskpb.FieldMask) (*mongodemo.MongoRecord, error)
	Delete(ctx context.Context, name string) error
}

// ParseResourceName parses a Mongo record resource name.
func ParseResourceName(name string) (parent string, id string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "apps" || parts[2] != "mongoRecords" || parts[1] == "" || parts[3] == "" {
		return "", "", errors.New("invalid resource name")
	}
	return strings.Join(parts[:2], "/"), parts[3], nil
}

// EncodePageToken encodes a skip count into a page token.
func EncodePageToken(skip int) string {
	return base64.StdEncoding.EncodeToString([]byte(strconv.Itoa(skip)))
}

// DecodePageToken decodes a page token into a skip count.
func DecodePageToken(token string) (int, error) {
	if token == "" {
		return 0, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return 0, err
	}
	value, err := strconv.Atoi(string(decoded))
	if err != nil {
		return 0, err
	}
	return value, nil
}

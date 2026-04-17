package testplan

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	mongo_demo "dominion/experimental/mongo_demo"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	envSUTHostURL = "SUT_HOST_URL"
	envSUTEnvName = "SUT_ENV_NAME"
	headerEnv     = "env"
	testParent    = "apps/testapp"
)

var (
	jsonMarshaler   = protojson.MarshalOptions{}
	jsonUnmarshaler = protojson.UnmarshalOptions{}
)

// requireEnv reads an environment variable or fails the test.
func requireEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Fatalf("environment variable %s is required", name)
	}
	return v
}

// uniqueID generates a unique record ID for test isolation.
func uniqueID() string {
	return fmt.Sprintf("rec-%d", time.Now().UnixNano())
}

// doRequest creates and executes an HTTP request with the env header set.
func doRequest(t *testing.T, method, url, envName string, body io.Reader) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("http.NewRequest(%s, %s) unexpected error: %v", method, url, err)
	}
	req.Header.Set(headerEnv, envName)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.Do(%s, %s) unexpected error: %v", method, url, err)
	}
	return resp
}

// decodeMongoRecord reads and decodes a MongoRecord from an HTTP response body.
func decodeMongoRecord(t *testing.T, resp *http.Response) *mongo_demo.MongoRecord {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	got := new(mongo_demo.MongoRecord)
	if err := jsonUnmarshaler.Unmarshal(body, got); err != nil {
		t.Fatalf("decode MongoRecord: %v", err)
	}
	return got
}

// createTestRecord creates a Mongo record and returns the created record.
// Used as "given" setup for tests that test other operations.
func createTestRecord(t *testing.T, hostURL, envName, recordID, title, description string) *mongo_demo.MongoRecord {
	t.Helper()
	body, err := jsonMarshaler.Marshal(&mongo_demo.MongoRecord{Title: title, Description: description})
	if err != nil {
		t.Fatalf("marshal MongoRecord: %v", err)
	}

	url := fmt.Sprintf("%s/v1/%s/mongoRecords?mongoRecordId=%s", hostURL, testParent, recordID)
	resp := doRequest(t, http.MethodPost, url, envName, bytes.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("create record status = %d, want %d, body: %s", resp.StatusCode, http.StatusOK, string(respBody))
	}

	return decodeMongoRecord(t, resp)
}

// deleteTestRecord deletes a Mongo record by resource name.
// Used for cleanup via defer.
func deleteTestRecord(t *testing.T, hostURL, envName, name string) {
	t.Helper()
	url := fmt.Sprintf("%s/v1/%s", hostURL, name)
	resp := doRequest(t, http.MethodDelete, url, envName, nil)
	defer resp.Body.Close()
}

func TestCreateMongoRecord(t *testing.T) {
	sutHostURL := requireEnv(t, envSUTHostURL)
	sutEnvName := requireEnv(t, envSUTEnvName)

	recordID := uniqueID()
	defer deleteTestRecord(t, sutHostURL, sutEnvName, fmt.Sprintf("%s/mongoRecords/%s", testParent, recordID))

	// given
	wantTitle := "Test Record"
	wantDesc := "A test description"
	body, err := jsonMarshaler.Marshal(&mongo_demo.MongoRecord{Title: wantTitle, Description: wantDesc})
	if err != nil {
		t.Fatalf("marshal MongoRecord: %v", err)
	}
	url := fmt.Sprintf("%s/v1/%s/mongoRecords?mongoRecordId=%s", sutHostURL, testParent, recordID)

	// when
	resp := doRequest(t, http.MethodPost, url, sutEnvName, bytes.NewReader(body))
	defer resp.Body.Close()

	got := decodeMongoRecord(t, resp)

	// then
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	wantName := fmt.Sprintf("%s/mongoRecords/%s", testParent, recordID)
	if got.GetName() != wantName {
		t.Errorf("got.Name = %q, want %q", got.GetName(), wantName)
	}
	if got.GetTitle() != wantTitle {
		t.Errorf("got.Title = %q, want %q", got.GetTitle(), wantTitle)
	}
	if got.GetDescription() != wantDesc {
		t.Errorf("got.Description = %q, want %q", got.GetDescription(), wantDesc)
	}
	if got.GetCreateTime() == nil {
		t.Error("got.CreateTime is empty, want non-empty")
	}
	if got.GetUpdateTime() == nil {
		t.Error("got.UpdateTime is empty, want non-empty")
	}
}

func TestGetMongoRecord(t *testing.T) {
	sutHostURL := requireEnv(t, envSUTHostURL)
	sutEnvName := requireEnv(t, envSUTEnvName)

	recordID := uniqueID()
	created := createTestRecord(t, sutHostURL, sutEnvName, recordID, "Get Test", "get description")
	defer deleteTestRecord(t, sutHostURL, sutEnvName, created.GetName())

	// given
	url := fmt.Sprintf("%s/v1/%s", sutHostURL, created.GetName())

	// when
	resp := doRequest(t, http.MethodGet, url, sutEnvName, nil)
	defer resp.Body.Close()

	got := decodeMongoRecord(t, resp)

	// then
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got.GetName() != created.GetName() {
		t.Errorf("got.Name = %q, want %q", got.GetName(), created.GetName())
	}
	if got.GetTitle() != "Get Test" {
		t.Errorf("got.Title = %q, want %q", got.GetTitle(), "Get Test")
	}
	if got.GetDescription() != "get description" {
		t.Errorf("got.Description = %q, want %q", got.GetDescription(), "get description")
	}
	if got.GetCreateTime() == nil {
		t.Error("got.CreateTime is empty, want non-empty")
	}
}

func TestGetMongoRecord_NotFound(t *testing.T) {
	sutHostURL := requireEnv(t, envSUTHostURL)
	sutEnvName := requireEnv(t, envSUTEnvName)

	// given
	url := fmt.Sprintf("%s/v1/%s/mongoRecords/%s", sutHostURL, testParent, uniqueID())

	// when
	resp := doRequest(t, http.MethodGet, url, sutEnvName, nil)
	defer resp.Body.Close()

	// then
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestUpdateMongoRecord(t *testing.T) {
	sutHostURL := requireEnv(t, envSUTHostURL)
	sutEnvName := requireEnv(t, envSUTEnvName)

	recordID := uniqueID()
	created := createTestRecord(t, sutHostURL, sutEnvName, recordID, "Original Title", "Keep this description")
	defer deleteTestRecord(t, sutHostURL, sutEnvName, created.GetName())

	// given
	updateBody, err := jsonMarshaler.Marshal(&mongo_demo.MongoRecord{Title: "Updated Title"})
	if err != nil {
		t.Fatalf("marshal MongoRecord: %v", err)
	}
	url := fmt.Sprintf("%s/v1/%s?updateMask=title", sutHostURL, created.GetName())

	// when
	resp := doRequest(t, http.MethodPatch, url, sutEnvName, bytes.NewReader(updateBody))
	defer resp.Body.Close()

	got := decodeMongoRecord(t, resp)

	// then
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got.GetTitle() != "Updated Title" {
		t.Errorf("got.Title = %q, want %q", got.GetTitle(), "Updated Title")
	}
	if got.GetDescription() != "Keep this description" {
		t.Errorf("got.Description = %q, want %q (unchanged)", got.GetDescription(), "Keep this description")
	}
	if got.GetName() != created.GetName() {
		t.Errorf("got.Name = %q, want %q (unchanged)", got.GetName(), created.GetName())
	}
}

func TestDeleteMongoRecord(t *testing.T) {
	sutHostURL := requireEnv(t, envSUTHostURL)
	sutEnvName := requireEnv(t, envSUTEnvName)

	recordID := uniqueID()
	created := createTestRecord(t, sutHostURL, sutEnvName, recordID, "To Delete", "Will be deleted")

	// given
	url := fmt.Sprintf("%s/v1/%s", sutHostURL, created.GetName())

	// when: delete
	resp := doRequest(t, http.MethodDelete, url, sutEnvName, nil)
	defer resp.Body.Close()

	// then: delete returns 200
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// then: subsequent get returns 404
	getResp := doRequest(t, http.MethodGet, url, sutEnvName, nil)
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("get after delete status = %d, want %d", getResp.StatusCode, http.StatusNotFound)
	}
}

func TestListMongoRecords(t *testing.T) {
	sutHostURL := requireEnv(t, envSUTHostURL)
	sutEnvName := requireEnv(t, envSUTEnvName)

	id1 := uniqueID()
	id2 := uniqueID()
	rec1 := createTestRecord(t, sutHostURL, sutEnvName, id1, "List Item A", "first item")
	rec2 := createTestRecord(t, sutHostURL, sutEnvName, id2, "List Item B", "second item")
	defer deleteTestRecord(t, sutHostURL, sutEnvName, rec1.GetName())
	defer deleteTestRecord(t, sutHostURL, sutEnvName, rec2.GetName())

	// given
	url := fmt.Sprintf("%s/v1/%s/mongoRecords", sutHostURL, testParent)

	// when
	resp := doRequest(t, http.MethodGet, url, sutEnvName, nil)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	got := new(mongo_demo.ListMongoRecordsResponse)
	if err := jsonUnmarshaler.Unmarshal(body, got); err != nil {
		t.Fatalf("decode ListMongoRecordsResponse: %v", err)
	}

	// then
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	found1, found2 := false, false
	for _, r := range got.GetMongoRecords() {
		if r.GetName() == rec1.GetName() {
			found1 = true
		}
		if r.GetName() == rec2.GetName() {
			found2 = true
		}
	}
	if !found1 {
		t.Errorf("record %q not found in list response", rec1.GetName())
	}
	if !found2 {
		t.Errorf("record %q not found in list response", rec2.GetName())
	}
}

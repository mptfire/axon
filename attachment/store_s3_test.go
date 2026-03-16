package attachment

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/s3"
	"heckel.io/ntfy/v2/util"
)

// --- Integration tests using a mock S3 server ---

func TestS3Store_WriteReadRemove(t *testing.T) {
	server := newMockS3Server()
	defer server.Close()

	store := newTestS3Store(t, server, "my-bucket", "pfx", 10*1024)

	// Write
	size, err := store.Write("abcdefghijkl", strings.NewReader("hello world"))
	require.Nil(t, err)
	require.Equal(t, int64(11), size)
	require.Equal(t, int64(11), store.Size())

	// Read back
	reader, readSize, err := store.Read("abcdefghijkl")
	require.Nil(t, err)
	require.Equal(t, int64(11), readSize)
	data, err := io.ReadAll(reader)
	reader.Close()
	require.Nil(t, err)
	require.Equal(t, "hello world", string(data))

	// Remove
	require.Nil(t, store.Remove("abcdefghijkl"))
	require.Equal(t, int64(0), store.Size())

	// Read after remove should fail
	_, _, err = store.Read("abcdefghijkl")
	require.Error(t, err)
}

func TestS3Store_WriteNoPrefix(t *testing.T) {
	server := newMockS3Server()
	defer server.Close()

	store := newTestS3Store(t, server, "my-bucket", "", 10*1024)

	size, err := store.Write("abcdefghijkl", strings.NewReader("test"))
	require.Nil(t, err)
	require.Equal(t, int64(4), size)

	reader, _, err := store.Read("abcdefghijkl")
	require.Nil(t, err)
	data, err := io.ReadAll(reader)
	reader.Close()
	require.Nil(t, err)
	require.Equal(t, "test", string(data))
}

func TestS3Store_WriteTotalSizeLimit(t *testing.T) {
	server := newMockS3Server()
	defer server.Close()

	store := newTestS3Store(t, server, "my-bucket", "pfx", 100)

	// First write fits
	_, err := store.Write("abcdefghijk0", bytes.NewReader(make([]byte, 80)))
	require.Nil(t, err)
	require.Equal(t, int64(80), store.Size())
	require.Equal(t, int64(20), store.Remaining())

	// Second write exceeds total limit
	_, err = store.Write("abcdefghijk1", bytes.NewReader(make([]byte, 50)))
	require.Equal(t, util.ErrLimitReached, err)
}

func TestS3Store_WriteFileSizeLimit(t *testing.T) {
	server := newMockS3Server()
	defer server.Close()

	store := newTestS3Store(t, server, "my-bucket", "pfx", 10*1024)

	_, err := store.Write("abcdefghijkl", bytes.NewReader(make([]byte, 200)), util.NewFixedLimiter(100))
	require.Equal(t, util.ErrLimitReached, err)
}

func TestS3Store_WriteRemoveMultiple(t *testing.T) {
	server := newMockS3Server()
	defer server.Close()

	store := newTestS3Store(t, server, "my-bucket", "pfx", 10*1024)

	for i := 0; i < 5; i++ {
		_, err := store.Write(fmt.Sprintf("abcdefghijk%d", i), bytes.NewReader(make([]byte, 100)))
		require.Nil(t, err)
	}
	require.Equal(t, int64(500), store.Size())

	require.Nil(t, store.Remove("abcdefghijk1", "abcdefghijk3"))
	require.Equal(t, int64(300), store.Size())
}

func TestS3Store_ReadNotFound(t *testing.T) {
	server := newMockS3Server()
	defer server.Close()

	store := newTestS3Store(t, server, "my-bucket", "pfx", 10*1024)

	_, _, err := store.Read("abcdefghijkl")
	require.Error(t, err)
}

func TestS3Store_InvalidID(t *testing.T) {
	server := newMockS3Server()
	defer server.Close()

	store := newTestS3Store(t, server, "my-bucket", "pfx", 10*1024)

	_, err := store.Write("bad", strings.NewReader("x"))
	require.Equal(t, errInvalidFileID, err)

	_, _, err = store.Read("bad")
	require.Equal(t, errInvalidFileID, err)

	err = store.Remove("bad")
	require.Equal(t, errInvalidFileID, err)
}

// --- Helpers ---

func newTestS3Store(t *testing.T, server *httptest.Server, bucket, prefix string, totalSizeLimit int64) Store {
	t.Helper()
	// httptest.NewTLSServer URL is like "https://127.0.0.1:PORT"
	host := strings.TrimPrefix(server.URL, "https://")
	s := &s3Store{
		client: &s3.Client{
			AccessKey:  "AKID",
			SecretKey:  "SECRET",
			Region:     "us-east-1",
			Endpoint:   host,
			Bucket:     bucket,
			Prefix:     prefix,
			PathStyle:  true,
			HTTPClient: server.Client(),
		},
		totalSizeLimit: totalSizeLimit,
	}
	// Compute initial size (should be 0 for fresh mock)
	size, err := s.computeSize()
	require.Nil(t, err)
	s.totalSizeCurrent = size
	return s
}

// --- Mock S3 server ---
//
// A minimal S3-compatible HTTP server that supports PutObject, GetObject, DeleteObjects, and
// ListObjectsV2. Uses path-style addressing: /{bucket}/{key}. Objects are stored in memory.

type mockS3Server struct {
	objects map[string][]byte // full key (bucket/key) -> body
	mu      sync.RWMutex
}

func newMockS3Server() *httptest.Server {
	m := &mockS3Server{objects: make(map[string][]byte)}
	return httptest.NewTLSServer(m)
}

func (m *mockS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Path is /{bucket}[/{key...}]
	path := strings.TrimPrefix(r.URL.Path, "/")

	switch {
	case r.Method == http.MethodPut:
		m.handlePut(w, r, path)
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		m.handleList(w, r, path)
	case r.Method == http.MethodGet:
		m.handleGet(w, r, path)
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		m.handleDelete(w, r, path)
	default:
		http.Error(w, "not implemented", http.StatusNotImplemented)
	}
}

func (m *mockS3Server) handlePut(w http.ResponseWriter, r *http.Request, path string) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	m.mu.Lock()
	m.objects[path] = body
	m.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (m *mockS3Server) handleGet(w http.ResponseWriter, r *http.Request, path string) {
	m.mu.RLock()
	body, ok := m.objects[path]
	m.mu.RUnlock()
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchKey</Code><Message>The specified key does not exist.</Message></Error>`))
		return
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
	w.WriteHeader(http.StatusOK)
	w.Write(body)
}

func (m *mockS3Server) handleDelete(w http.ResponseWriter, r *http.Request, bucketPath string) {
	// bucketPath is just the bucket name
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var req struct {
		Objects []struct {
			Key string `xml:"Key"`
		} `xml:"Object"`
	}
	if err := xml.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	m.mu.Lock()
	for _, obj := range req.Objects {
		delete(m.objects, bucketPath+"/"+obj.Key)
	}
	m.mu.Unlock()
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><DeleteResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"></DeleteResult>`))
}

func (m *mockS3Server) handleList(w http.ResponseWriter, r *http.Request, bucketPath string) {
	prefix := r.URL.Query().Get("prefix")
	m.mu.RLock()
	var contents []s3ListObject
	for key, body := range m.objects {
		// key is "bucket/objectkey", strip bucket prefix
		objKey := strings.TrimPrefix(key, bucketPath+"/")
		if objKey == key {
			continue // different bucket
		}
		if prefix == "" || strings.HasPrefix(objKey, prefix) {
			contents = append(contents, s3ListObject{Key: objKey, Size: int64(len(body))})
		}
	}
	m.mu.RUnlock()

	resp := s3ListResponse{
		Contents:    contents,
		IsTruncated: false,
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp)
}

type s3ListResponse struct {
	XMLName     xml.Name       `xml:"ListBucketResult"`
	Contents    []s3ListObject `xml:"Contents"`
	IsTruncated bool           `xml:"IsTruncated"`
}

type s3ListObject struct {
	Key  string `xml:"Key"`
	Size int64  `xml:"Size"`
}

package attachment

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/s3"
)

// --- S3-specific tests ---

func TestS3Store_WriteNoPrefix(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()

	cache := newTestS3Store(t, server, "my-bucket", "", 10*1024)

	size, err := cache.Write("abcdefghijkl", strings.NewReader("test"), 0)
	require.Nil(t, err)
	require.Equal(t, int64(4), size)

	reader, _, err := cache.Read("abcdefghijkl")
	require.Nil(t, err)
	data, err := io.ReadAll(reader)
	reader.Close()
	require.Nil(t, err)
	require.Equal(t, "test", string(data))
}

// --- Helpers ---

func newTestS3Store(t *testing.T, server *httptest.Server, bucket, prefix string, totalSizeLimit int64) *Store {
	t.Helper()
	host := strings.TrimPrefix(server.URL, "https://")
	backend := newS3Backend(s3.New(&s3.Config{
		AccessKey:  "AKID",
		SecretKey:  "SECRET",
		Region:     "us-east-1",
		Endpoint:   host,
		Bucket:     bucket,
		Prefix:     prefix,
		PathStyle:  true,
		HTTPClient: server.Client(),
	}))
	cache, err := newStore(backend, totalSizeLimit, nil)
	require.Nil(t, err)
	t.Cleanup(func() { cache.Close() })
	return cache
}

// --- Mock S3 server ---
//
// A minimal S3-compatible HTTP server that supports PutObject, GetObject, DeleteObjects, and
// ListObjectsV2. Uses path-style addressing: /{bucket}/{key}. Objects are stored in memory.

type mockS3Server struct {
	objects  map[string][]byte         // full key (bucket/key) -> body
	modTimes map[string]time.Time      // full key (bucket/key) -> last modified time
	uploads  map[string]map[int][]byte // uploadID -> partNumber -> data
	nextID   int                       // counter for generating upload IDs
	mu       sync.RWMutex
}

func newMockS3Server() (*httptest.Server, *mockS3Server) {
	m := &mockS3Server{
		objects:  make(map[string][]byte),
		modTimes: make(map[string]time.Time),
		uploads:  make(map[string]map[int][]byte),
	}
	return httptest.NewTLSServer(m), m
}

func (m *mockS3Server) setModTime(path string, t time.Time) {
	m.mu.Lock()
	m.modTimes[path] = t
	m.mu.Unlock()
}

func (m *mockS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Path is /{bucket}[/{key...}]
	path := strings.TrimPrefix(r.URL.Path, "/")
	q := r.URL.Query()

	switch {
	case r.Method == http.MethodPut && q.Has("partNumber"):
		m.handleUploadPart(w, r, path)
	case r.Method == http.MethodPut:
		m.handlePut(w, r, path)
	case r.Method == http.MethodPost && q.Has("uploads"):
		m.handleInitiateMultipart(w, r, path)
	case r.Method == http.MethodPost && q.Has("uploadId"):
		m.handleCompleteMultipart(w, r, path)
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		m.handleAbortMultipart(w, r, path)
	case r.Method == http.MethodGet && q.Get("list-type") == "2":
		m.handleList(w, r, path)
	case r.Method == http.MethodGet:
		m.handleGet(w, r, path)
	case r.Method == http.MethodPost && q.Has("delete"):
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
	m.modTimes[path] = time.Now()
	m.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

func (m *mockS3Server) handleInitiateMultipart(w http.ResponseWriter, r *http.Request, path string) {
	m.mu.Lock()
	m.nextID++
	uploadID := fmt.Sprintf("upload-%d", m.nextID)
	m.uploads[uploadID] = make(map[int][]byte)
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><InitiateMultipartUploadResult><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, uploadID)
}

func (m *mockS3Server) handleUploadPart(w http.ResponseWriter, r *http.Request, path string) {
	uploadID := r.URL.Query().Get("uploadId")
	var partNumber int
	fmt.Sscanf(r.URL.Query().Get("partNumber"), "%d", &partNumber)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	m.mu.Lock()
	parts, ok := m.uploads[uploadID]
	if !ok {
		m.mu.Unlock()
		http.Error(w, "NoSuchUpload", http.StatusNotFound)
		return
	}
	parts[partNumber] = body
	m.mu.Unlock()

	etag := fmt.Sprintf(`"etag-part-%d"`, partNumber)
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

func (m *mockS3Server) handleCompleteMultipart(w http.ResponseWriter, r *http.Request, path string) {
	uploadID := r.URL.Query().Get("uploadId")

	m.mu.Lock()
	parts, ok := m.uploads[uploadID]
	if !ok {
		m.mu.Unlock()
		http.Error(w, "NoSuchUpload", http.StatusNotFound)
		return
	}

	// Assemble parts in order
	var assembled []byte
	for i := 1; i <= len(parts); i++ {
		assembled = append(assembled, parts[i]...)
	}
	m.objects[path] = assembled
	m.modTimes[path] = time.Now()
	delete(m.uploads, uploadID)
	m.mu.Unlock()

	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?><CompleteMultipartUploadResult><Key>%s</Key></CompleteMultipartUploadResult>`, path)
}

func (m *mockS3Server) handleAbortMultipart(w http.ResponseWriter, r *http.Request, path string) {
	uploadID := r.URL.Query().Get("uploadId")
	m.mu.Lock()
	delete(m.uploads, uploadID)
	m.mu.Unlock()
	w.WriteHeader(http.StatusNoContent)
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
			contents = append(contents, s3ListObject{
				Key:          objKey,
				Size:         int64(len(body)),
				LastModified: m.modTimes[key].Format(time.RFC3339),
			})
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
	Key          string `xml:"Key"`
	Size         int64  `xml:"Size"`
	LastModified string `xml:"LastModified"`
}

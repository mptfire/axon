package s3

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// --- Mock S3 server ---
//
// A minimal S3-compatible HTTP server that supports PutObject, GetObject, DeleteObjects, and
// ListObjectsV2. Uses path-style addressing: /{bucket}/{key}. Objects are stored in memory.

type mockS3Server struct {
	objects map[string][]byte         // full key (bucket/key) -> body
	uploads map[string]map[int][]byte // uploadID -> partNumber -> data
	nextID  int                       // counter for generating upload IDs
	mu      sync.RWMutex
}

func newMockS3Server() (*httptest.Server, *mockS3Server) {
	m := &mockS3Server{
		objects: make(map[string][]byte),
		uploads: make(map[string]map[int][]byte),
	}
	return httptest.NewTLSServer(m), m
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

type listObjectsResponse struct {
	XMLName  xml.Name     `xml:"ListBucketResult"`
	Contents []listObject `xml:"Contents"`
	// Pagination support
	IsTruncated           bool   `xml:"IsTruncated"`
	NextContinuationToken string `xml:"NextContinuationToken"`
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
	contToken := r.URL.Query().Get("continuation-token")

	m.mu.RLock()
	var allKeys []string
	for key := range m.objects {
		objKey := strings.TrimPrefix(key, bucketPath+"/")
		if objKey == key {
			continue // different bucket
		}
		if prefix == "" || strings.HasPrefix(objKey, prefix) {
			allKeys = append(allKeys, objKey)
		}
	}
	m.mu.RUnlock()
	sort.Strings(allKeys)

	// Simple continuation token: it's the key to start after
	startIdx := 0
	if contToken != "" {
		for i, k := range allKeys {
			if k == contToken {
				startIdx = i + 1
				break
			}
		}
	}

	maxKeys := 1000
	if mk := r.URL.Query().Get("max-keys"); mk != "" {
		fmt.Sscanf(mk, "%d", &maxKeys)
	}

	endIdx := startIdx + maxKeys
	truncated := false
	nextToken := ""
	if endIdx < len(allKeys) {
		truncated = true
		nextToken = allKeys[endIdx-1]
		allKeys = allKeys[startIdx:endIdx]
	} else {
		allKeys = allKeys[startIdx:]
	}

	m.mu.RLock()
	var contents []listObject
	for _, objKey := range allKeys {
		body := m.objects[bucketPath+"/"+objKey]
		contents = append(contents, listObject{Key: objKey, Size: int64(len(body)), LastModified: time.Now().Format(time.RFC3339)})
	}
	m.mu.RUnlock()

	resp := listObjectsResponse{
		Contents:              contents,
		IsTruncated:           truncated,
		NextContinuationToken: nextToken,
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	xml.NewEncoder(w).Encode(resp)
}

func (m *mockS3Server) objectCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.objects)
}

// --- Helper to create a test client pointing at mock server ---

func newTestClient(server *httptest.Server, bucket, prefix string) *Client {
	// httptest.NewTLSServer URL is like "https://127.0.0.1:PORT"
	host := strings.TrimPrefix(server.URL, "https://")
	return New(&Config{
		AccessKey:  "AKID",
		SecretKey:  "SECRET",
		Region:     "us-east-1",
		Endpoint:   host,
		Bucket:     bucket,
		Prefix:     prefix,
		PathStyle:  true,
		HTTPClient: server.Client(),
	})
}

// --- URL parsing tests ---

func TestParseURL_Success(t *testing.T) {
	cfg, err := ParseURL("s3://AKID:SECRET@my-bucket/attachments?region=us-east-1")
	require.Nil(t, err)
	require.Equal(t, "my-bucket", cfg.Bucket)
	require.Equal(t, "attachments", cfg.Prefix)
	require.Equal(t, "us-east-1", cfg.Region)
	require.Equal(t, "AKID", cfg.AccessKey)
	require.Equal(t, "SECRET", cfg.SecretKey)
	require.Equal(t, "s3.us-east-1.amazonaws.com", cfg.Endpoint)
	require.False(t, cfg.PathStyle)
}

func TestParseURL_NoPrefix(t *testing.T) {
	cfg, err := ParseURL("s3://AKID:SECRET@my-bucket?region=us-east-1")
	require.Nil(t, err)
	require.Equal(t, "my-bucket", cfg.Bucket)
	require.Equal(t, "", cfg.Prefix)
}

func TestParseURL_WithEndpoint(t *testing.T) {
	cfg, err := ParseURL("s3://AKID:SECRET@my-bucket/prefix?region=us-east-1&endpoint=https://s3.example.com")
	require.Nil(t, err)
	require.Equal(t, "my-bucket", cfg.Bucket)
	require.Equal(t, "prefix", cfg.Prefix)
	require.Equal(t, "s3.example.com", cfg.Endpoint)
	require.True(t, cfg.PathStyle)
}

func TestParseURL_EndpointHTTP(t *testing.T) {
	cfg, err := ParseURL("s3://AKID:SECRET@my-bucket?region=us-east-1&endpoint=http://localhost:9000")
	require.Nil(t, err)
	require.Equal(t, "localhost:9000", cfg.Endpoint)
	require.True(t, cfg.PathStyle)
}

func TestParseURL_EndpointTrailingSlash(t *testing.T) {
	cfg, err := ParseURL("s3://AKID:SECRET@my-bucket?region=us-east-1&endpoint=https://s3.example.com/")
	require.Nil(t, err)
	require.Equal(t, "s3.example.com", cfg.Endpoint)
}

func TestParseURL_NestedPrefix(t *testing.T) {
	cfg, err := ParseURL("s3://AKID:SECRET@my-bucket/a/b/c?region=us-east-1")
	require.Nil(t, err)
	require.Equal(t, "my-bucket", cfg.Bucket)
	require.Equal(t, "a/b/c", cfg.Prefix)
}

func TestParseURL_MissingRegion(t *testing.T) {
	_, err := ParseURL("s3://AKID:SECRET@my-bucket")
	require.Error(t, err)
	require.Contains(t, err.Error(), "region")
}

func TestParseURL_MissingCredentials(t *testing.T) {
	_, err := ParseURL("s3://my-bucket?region=us-east-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "access key")
}

func TestParseURL_MissingSecretKey(t *testing.T) {
	_, err := ParseURL("s3://AKID@my-bucket?region=us-east-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "secret key")
}

func TestParseURL_WrongScheme(t *testing.T) {
	_, err := ParseURL("http://AKID:SECRET@my-bucket?region=us-east-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "scheme")
}

func TestParseURL_EmptyBucket(t *testing.T) {
	_, err := ParseURL("s3://AKID:SECRET@?region=us-east-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "bucket")
}

// --- Unit tests: URL construction ---

func TestConfig_BucketURL_PathStyle(t *testing.T) {
	c := &Config{Endpoint: "s3.example.com", Bucket: "my-bucket", PathStyle: true}
	require.Equal(t, "https://s3.example.com/my-bucket", c.BucketURL())
}

func TestConfig_BucketURL_VirtualHosted(t *testing.T) {
	c := &Config{Endpoint: "s3.us-east-1.amazonaws.com", Bucket: "my-bucket", PathStyle: false}
	require.Equal(t, "https://my-bucket.s3.us-east-1.amazonaws.com", c.BucketURL())
}

func TestConfig_ObjectURL_PathStyle(t *testing.T) {
	c := &Config{Endpoint: "s3.example.com", Bucket: "my-bucket", Prefix: "prefix", PathStyle: true}
	require.Equal(t, "https://s3.example.com/my-bucket/prefix/obj", c.ObjectURL("obj"))
}

func TestConfig_ObjectURL_VirtualHosted(t *testing.T) {
	c := &Config{Endpoint: "s3.us-east-1.amazonaws.com", Bucket: "my-bucket", Prefix: "prefix", PathStyle: false}
	require.Equal(t, "https://my-bucket.s3.us-east-1.amazonaws.com/prefix/obj", c.ObjectURL("obj"))
}

func TestConfig_HostHeader_PathStyle(t *testing.T) {
	c := &Config{Endpoint: "s3.example.com", Bucket: "my-bucket", PathStyle: true}
	require.Equal(t, "s3.example.com", c.HostHeader())
}

func TestConfig_HostHeader_VirtualHosted(t *testing.T) {
	c := &Config{Endpoint: "s3.us-east-1.amazonaws.com", Bucket: "my-bucket", PathStyle: false}
	require.Equal(t, "my-bucket.s3.us-east-1.amazonaws.com", c.HostHeader())
}

func TestConfig_ObjectKey(t *testing.T) {
	c := &Config{Prefix: "attachments"}
	require.Equal(t, "attachments/file123", c.ObjectKey("file123"))

	c2 := &Config{Prefix: ""}
	require.Equal(t, "file123", c2.ObjectKey("file123"))
}

func TestConfig_ListPrefix(t *testing.T) {
	c := &Config{Prefix: "attachments"}
	require.Equal(t, "attachments/", c.ListPrefix())

	c2 := &Config{Prefix: ""}
	require.Equal(t, "", c2.ListPrefix())
}

// --- Integration tests using mock S3 server ---

func TestClient_PutGetObject(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "")

	ctx := context.Background()

	// Put
	err := client.PutObject(ctx, "test-key", strings.NewReader("hello world"))
	require.Nil(t, err)

	// Get
	reader, size, err := client.GetObject(ctx, "test-key")
	require.Nil(t, err)
	require.Equal(t, int64(11), size)
	data, err := io.ReadAll(reader)
	reader.Close()
	require.Nil(t, err)
	require.Equal(t, "hello world", string(data))
}

func TestClient_PutGetObject_WithPrefix(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "pfx")

	ctx := context.Background()

	err := client.PutObject(ctx, "test-key", strings.NewReader("hello"))
	require.Nil(t, err)

	reader, _, err := client.GetObject(ctx, "test-key")
	require.Nil(t, err)
	data, _ := io.ReadAll(reader)
	reader.Close()
	require.Equal(t, "hello", string(data))
}

func TestClient_GetObject_NotFound(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "")

	_, _, err := client.GetObject(context.Background(), "nonexistent")
	require.Error(t, err)
	var errResp *errorResponse
	require.ErrorAs(t, err, &errResp)
	require.Equal(t, 404, errResp.StatusCode)
	require.Equal(t, "NoSuchKey", errResp.Code)
}

func TestClient_DeleteObjects(t *testing.T) {
	server, mock := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "")

	ctx := context.Background()

	// Put several objects
	for i := 0; i < 5; i++ {
		err := client.PutObject(ctx, fmt.Sprintf("key-%d", i), bytes.NewReader([]byte("data")))
		require.Nil(t, err)
	}
	require.Equal(t, 5, mock.objectCount())

	// Delete some
	err := client.DeleteObjects(ctx, []string{"key-1", "key-3"})
	require.Nil(t, err)
	require.Equal(t, 3, mock.objectCount())

	// Verify deleted ones are gone
	_, _, err = client.GetObject(ctx, "key-1")
	require.Error(t, err)
	_, _, err = client.GetObject(ctx, "key-3")
	require.Error(t, err)

	// Verify remaining ones are still there
	reader, _, err := client.GetObject(ctx, "key-0")
	require.Nil(t, err)
	reader.Close()
}

func TestClient_ListObjects(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()

	ctx := context.Background()

	// Client with prefix "pfx": list should only return objects under pfx/
	client := newTestClient(server, "my-bucket", "pfx")
	for i := 0; i < 3; i++ {
		err := client.PutObject(ctx, fmt.Sprintf("%d", i), bytes.NewReader([]byte("x")))
		require.Nil(t, err)
	}

	// Also put an object outside the prefix using a no-prefix client
	clientNoPrefix := newTestClient(server, "my-bucket", "")
	err := clientNoPrefix.PutObject(ctx, "other", bytes.NewReader([]byte("y")))
	require.Nil(t, err)

	// List with prefix client: should only see 3
	result, err := client.listObjectsV2(ctx, "", 0)
	require.Nil(t, err)
	require.Len(t, result.Contents, 3)
	require.False(t, result.IsTruncated)

	// List with no-prefix client: should see all 4
	result, err = clientNoPrefix.listObjectsV2(ctx, "", 0)
	require.Nil(t, err)
	require.Len(t, result.Contents, 4)
}

func TestClient_ListObjects_Pagination(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "")

	ctx := context.Background()

	// Put 5 objects
	for i := 0; i < 5; i++ {
		err := client.PutObject(ctx, fmt.Sprintf("key-%02d", i), bytes.NewReader([]byte("x")))
		require.Nil(t, err)
	}

	// List with max-keys=2
	result, err := client.listObjectsV2(ctx, "", 2)
	require.Nil(t, err)
	require.Len(t, result.Contents, 2)
	require.True(t, result.IsTruncated)
	require.NotEmpty(t, result.NextContinuationToken)

	// Get next page
	result2, err := client.listObjectsV2(ctx, result.NextContinuationToken, 2)
	require.Nil(t, err)
	require.Len(t, result2.Contents, 2)
	require.True(t, result2.IsTruncated)

	// Get last page
	result3, err := client.listObjectsV2(ctx, result2.NextContinuationToken, 2)
	require.Nil(t, err)
	require.Len(t, result3.Contents, 1)
	require.False(t, result3.IsTruncated)
}

func TestClient_ListAllObjects(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "pfx")

	ctx := context.Background()

	for i := 0; i < 10; i++ {
		err := client.PutObject(ctx, fmt.Sprintf("key-%02d", i), bytes.NewReader([]byte("x")))
		require.Nil(t, err)
	}

	objects, err := client.ListObjectsV2(ctx)
	require.Nil(t, err)
	require.Len(t, objects, 10)
}

func TestClient_PutObject_LargeBody(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "")

	ctx := context.Background()

	// 1 MB object
	data := make([]byte, 1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	err := client.PutObject(ctx, "large", bytes.NewReader(data))
	require.Nil(t, err)

	reader, size, err := client.GetObject(ctx, "large")
	require.Nil(t, err)
	require.Equal(t, int64(1024*1024), size)
	got, err := io.ReadAll(reader)
	reader.Close()
	require.Nil(t, err)
	require.Equal(t, data, got)
}

func TestClient_PutObject_ChunkedUpload(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "")

	ctx := context.Background()

	// 12 MB object, exceeds 5 MB partSize, triggers multipart upload path
	data := make([]byte, 12*1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	err := client.PutObject(ctx, "multipart", bytes.NewReader(data))
	require.Nil(t, err)

	reader, size, err := client.GetObject(ctx, "multipart")
	require.Nil(t, err)
	require.Equal(t, int64(12*1024*1024), size)
	got, err := io.ReadAll(reader)
	reader.Close()
	require.Nil(t, err)
	require.Equal(t, data, got)
}

func TestClient_PutObject_ExactPartSize(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "")

	ctx := context.Background()

	// Exactly 5 MB (partSize), should use the simple put path (ReadFull succeeds fully)
	data := make([]byte, 5*1024*1024)
	for i := range data {
		data[i] = byte(i % 256)
	}
	err := client.PutObject(ctx, "exact", bytes.NewReader(data))
	require.Nil(t, err)

	reader, size, err := client.GetObject(ctx, "exact")
	require.Nil(t, err)
	require.Equal(t, int64(5*1024*1024), size)
	got, err := io.ReadAll(reader)
	reader.Close()
	require.Nil(t, err)
	require.Equal(t, data, got)
}

func TestClient_PutObject_NestedKey(t *testing.T) {
	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "")

	ctx := context.Background()

	err := client.PutObject(ctx, "deep/nested/prefix/file.txt", strings.NewReader("nested"))
	require.Nil(t, err)

	reader, _, err := client.GetObject(ctx, "deep/nested/prefix/file.txt")
	require.Nil(t, err)
	data, _ := io.ReadAll(reader)
	reader.Close()
	require.Equal(t, "nested", string(data))
}

// --- Scale test: 20k objects (ntfy-adjacent) ---

func TestClient_ListAllObjects_20k(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 20k object test in short mode")
	}

	server, _ := newMockS3Server()
	defer server.Close()
	client := newTestClient(server, "my-bucket", "attachments")

	ctx := context.Background()
	const numObjects = 20000
	const batchSize = 500

	// Insert 20k objects in batches to keep it fast
	for batch := 0; batch < numObjects/batchSize; batch++ {
		for i := 0; i < batchSize; i++ {
			idx := batch*batchSize + i
			key := fmt.Sprintf("%08d", idx)
			err := client.PutObject(ctx, key, bytes.NewReader([]byte("x")))
			require.Nil(t, err)
		}
	}

	// List all 20k objects with pagination
	objects, err := client.ListObjectsV2(ctx)
	require.Nil(t, err)
	require.Len(t, objects, numObjects)

	// Verify total size
	var totalSize int64
	for _, obj := range objects {
		totalSize += obj.Size
	}
	require.Equal(t, int64(numObjects), totalSize)

	// Delete 1000 objects (simulating attachment expiry cleanup)
	keys := make([]string, 1000)
	for i := range keys {
		keys[i] = fmt.Sprintf("%08d", i)
	}
	err = client.DeleteObjects(ctx, keys)
	require.Nil(t, err)

	// List again: should have 19000
	objects, err = client.ListObjectsV2(ctx)
	require.Nil(t, err)
	require.Len(t, objects, numObjects-1000)
}

// --- Real S3 integration test ---
//
// Set the following environment variables to run this test against a real S3 bucket:
//
//	S3_ACCESS_KEY, S3_SECRET_KEY, S3_REGION, S3_BUCKET
//
// Optional:
//
//	S3_ENDPOINT: host[:port] for S3-compatible providers (e.g. "nyc3.digitaloceanspaces.com")
//	S3_PATH_STYLE: set to "true" for path-style addressing
//	S3_PREFIX: key prefix to use (default: "ntfy-s3-test")
func TestClient_RealBucket(t *testing.T) {
	accessKey := os.Getenv("S3_ACCESS_KEY")
	secretKey := os.Getenv("S3_SECRET_KEY")
	region := os.Getenv("S3_REGION")
	bucket := os.Getenv("S3_BUCKET")

	if accessKey == "" || secretKey == "" || region == "" || bucket == "" {
		t.Skip("skipping real S3 test: set S3_ACCESS_KEY, S3_SECRET_KEY, S3_REGION, S3_BUCKET")
	}

	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		endpoint = fmt.Sprintf("s3.%s.amazonaws.com", region)
	}
	pathStyle := os.Getenv("S3_PATH_STYLE") == "true"
	prefix := os.Getenv("S3_PREFIX")
	if prefix == "" {
		prefix = "ntfy-s3-test"
	}

	client := New(&Config{
		AccessKey: accessKey,
		SecretKey: secretKey,
		Region:    region,
		Endpoint:  endpoint,
		Bucket:    bucket,
		Prefix:    prefix,
		PathStyle: pathStyle,
	})

	ctx := context.Background()

	// Clean up any leftover objects from previous runs
	existing, err := client.ListObjectsV2(ctx)
	require.Nil(t, err)
	if len(existing) > 0 {
		keys := make([]string, len(existing))
		for i, obj := range existing {
			keys[i] = obj.Key
		}
		// Batch delete in groups of 1000
		for i := 0; i < len(keys); i += 1000 {
			end := i + 1000
			if end > len(keys) {
				end = len(keys)
			}
			err := client.DeleteObjects(ctx, keys[i:end])
			require.Nil(t, err)
		}
	}

	t.Run("PutGetDelete", func(t *testing.T) {
		key := "test-object"
		content := "hello from ntfy s3 test"

		// Put
		err := client.PutObject(ctx, key, strings.NewReader(content))
		require.Nil(t, err)

		// Get
		reader, size, err := client.GetObject(ctx, key)
		require.Nil(t, err)
		require.Equal(t, int64(len(content)), size)
		data, err := io.ReadAll(reader)
		reader.Close()
		require.Nil(t, err)
		require.Equal(t, content, string(data))

		// Delete
		err = client.DeleteObjects(ctx, []string{key})
		require.Nil(t, err)

		// Get after delete should fail
		_, _, err = client.GetObject(ctx, key)
		require.Error(t, err)
		var errResp *errorResponse
		require.ErrorAs(t, err, &errResp)
		require.Equal(t, 404, errResp.StatusCode)
	})

	t.Run("ListObjects", func(t *testing.T) {
		// Use a sub-prefix client for isolation
		listClient := New(&Config{
			AccessKey: accessKey,
			SecretKey: secretKey,
			Region:    region,
			Endpoint:  endpoint,
			Bucket:    bucket,
			Prefix:    prefix + "/list-test",
			PathStyle: pathStyle,
		})

		// Put 10 objects
		for i := 0; i < 10; i++ {
			err := listClient.PutObject(ctx, fmt.Sprintf("%d", i), strings.NewReader("x"))
			require.Nil(t, err)
		}

		// List
		objects, err := listClient.ListObjectsV2(ctx)
		require.Nil(t, err)
		require.Len(t, objects, 10)

		// Clean up
		keys := make([]string, 10)
		for i := range keys {
			keys[i] = fmt.Sprintf("%d", i)
		}
		err = listClient.DeleteObjects(ctx, keys)
		require.Nil(t, err)
	})

	t.Run("LargeObject", func(t *testing.T) {
		key := "large-object"
		data := make([]byte, 5*1024*1024) // 5 MB
		for i := range data {
			data[i] = byte(i % 256)
		}

		err := client.PutObject(ctx, key, bytes.NewReader(data))
		require.Nil(t, err)

		reader, size, err := client.GetObject(ctx, key)
		require.Nil(t, err)
		require.Equal(t, int64(len(data)), size)
		got, err := io.ReadAll(reader)
		reader.Close()
		require.Nil(t, err)
		require.Equal(t, data, got)

		err = client.DeleteObjects(ctx, []string{key})
		require.Nil(t, err)
	})
}

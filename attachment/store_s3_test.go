package attachment

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/s3"
)

func TestS3Store_WriteWithPrefix(t *testing.T) {
	s3URL := os.Getenv("NTFY_TEST_ATTACHMENT_S3_URL")
	if s3URL == "" {
		t.Skip("NTFY_TEST_ATTACHMENT_S3_URL not set")
	}
	cfg, err := s3.ParseURL(s3URL)
	require.Nil(t, err)
	cfg.Prefix = "test-prefix"
	client := s3.New(cfg)
	deleteAllObjects(client)
	backend := newS3Backend(client)
	cache, err := newStore(backend, 10*1024, nil)
	require.Nil(t, err)
	t.Cleanup(func() {
		deleteAllObjects(client)
		cache.Close()
	})

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

func newTestRealS3Store(t *testing.T, totalSizeLimit int64) (*Store, *modTimeOverrideBackend) {
	t.Helper()
	s3URL := os.Getenv("NTFY_TEST_ATTACHMENT_S3_URL")
	if s3URL == "" {
		t.Skip("NTFY_TEST_ATTACHMENT_S3_URL not set")
	}
	cfg, err := s3.ParseURL(s3URL)
	require.Nil(t, err)
	client := s3.New(cfg)
	inner := newS3Backend(client)
	wrapper := &modTimeOverrideBackend{backend: inner, modTimes: make(map[string]time.Time)}
	deleteAllObjects(client)
	store, err := newStore(wrapper, totalSizeLimit, nil)
	require.Nil(t, err)
	t.Cleanup(func() {
		deleteAllObjects(client)
		store.Close()
	})
	return store, wrapper
}

func deleteAllObjects(client *s3.Client) {
	objects, _ := client.ListObjectsV2(context.Background())
	keys := make([]string, 0, len(objects))
	for _, obj := range objects {
		keys = append(keys, obj.Key)
	}
	if len(keys) > 0 {
		client.DeleteObjects(context.Background(), keys) //nolint:errcheck
	}
}

// modTimeOverrideBackend wraps a backend and allows overriding LastModified times returned by List().
// This is used in tests to simulate old objects on backends (like real S3) where
// LastModified cannot be set directly.
type modTimeOverrideBackend struct {
	backend
	mu       sync.Mutex
	modTimes map[string]time.Time // object ID -> override time
}

func (b *modTimeOverrideBackend) List() ([]object, error) {
	objects, err := b.backend.List()
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, obj := range objects {
		if t, ok := b.modTimes[obj.ID]; ok {
			objects[i].LastModified = t
		}
	}
	return objects, nil
}

func (b *modTimeOverrideBackend) setModTime(id string, t time.Time) {
	b.mu.Lock()
	b.modTimes[id] = t
	b.mu.Unlock()
}

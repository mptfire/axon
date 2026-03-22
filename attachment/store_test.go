package attachment

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/util"
)

const testSizeLimit = 10 * 1024

func TestStore_WriteReadRemove(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		// Write
		size, err := s.Write("abcdefghijkl", strings.NewReader("hello world"), 0)
		require.Nil(t, err)
		require.Equal(t, int64(11), size)
		require.Equal(t, int64(11), s.Size())

		// Read back
		reader, readSize, err := s.Read("abcdefghijkl")
		require.Nil(t, err)
		require.Equal(t, int64(11), readSize)
		data, err := io.ReadAll(reader)
		reader.Close()
		require.Nil(t, err)
		require.Equal(t, "hello world", string(data))

		// Remove
		require.Nil(t, s.Remove("abcdefghijkl"))
		require.Equal(t, int64(0), s.Size())

		// Read after remove should fail
		_, _, err = s.Read("abcdefghijkl")
		require.Error(t, err)
	})
}

func TestStore_WriteRemoveMultiple(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		for i := 0; i < 5; i++ {
			_, err := s.Write(fmt.Sprintf("abcdefghijk%d", i), bytes.NewReader(make([]byte, 100)), 0)
			require.Nil(t, err)
		}
		require.Equal(t, int64(500), s.Size())

		require.Nil(t, s.Remove("abcdefghijk1", "abcdefghijk3"))
		require.Equal(t, int64(300), s.Size())

		// Removed files should not be readable
		_, _, err := s.Read("abcdefghijk1")
		require.Error(t, err)
		_, _, err = s.Read("abcdefghijk3")
		require.Error(t, err)

		// Remaining files should still be readable
		for _, id := range []string{"abcdefghijk0", "abcdefghijk2", "abcdefghijk4"} {
			reader, _, err := s.Read(id)
			require.Nil(t, err)
			reader.Close()
		}
	})
}

func TestStore_WriteTotalSizeLimit(t *testing.T) {
	forEachBackend(t, 100, func(t *testing.T, s *Store, _ func(string)) {
		// First write fits
		_, err := s.Write("abcdefghijk0", bytes.NewReader(make([]byte, 80)), 0)
		require.Nil(t, err)
		require.Equal(t, int64(80), s.Size())
		require.Equal(t, int64(20), s.Remaining())

		// Second write exceeds total limit
		_, err = s.Write("abcdefghijk1", bytes.NewReader(make([]byte, 50)), 0)
		require.ErrorIs(t, err, util.ErrLimitReached)
	})
}

func TestStore_WriteAdditionalLimiter(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		_, err := s.Write("abcdefghijkl", bytes.NewReader(make([]byte, 200)), 0, util.NewFixedLimiter(100))
		require.ErrorIs(t, err, util.ErrLimitReached)

		// File should not be readable (was cleaned up)
		_, _, err = s.Read("abcdefghijkl")
		require.Error(t, err)
	})
}

func TestStore_WriteWithLimiter(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		size, err := s.Write("abcdefghijkl", strings.NewReader("normal file"), 0, util.NewFixedLimiter(999))
		require.Nil(t, err)
		require.Equal(t, int64(11), size)
		require.Equal(t, int64(11), s.Size())
	})
}

func TestStore_ReadNotFound(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		_, _, err := s.Read("abcdefghijkl")
		require.Error(t, err)
	})
}

func TestStore_InvalidID(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		_, err := s.Write("bad", strings.NewReader("x"), 0)
		require.Equal(t, errInvalidFileID, err)

		_, _, err = s.Read("bad")
		require.Equal(t, errInvalidFileID, err)

		err = s.Remove("bad")
		require.Equal(t, errInvalidFileID, err)
	})
}

func TestStore_WriteUntrustedLengthExact(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		size, err := s.Write("abcdefghijkl", strings.NewReader("hello world"), 11)
		require.Nil(t, err)
		require.Equal(t, int64(11), size)

		reader, _, err := s.Read("abcdefghijkl")
		require.Nil(t, err)
		data, err := io.ReadAll(reader)
		reader.Close()
		require.Nil(t, err)
		require.Equal(t, "hello world", string(data))
	})
}

func TestStore_WriteUntrustedLengthBodyLonger(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		// Body has 11 bytes, but we claim 5 — only first 5 bytes should be stored
		size, err := s.Write("abcdefghijkl", strings.NewReader("hello world"), 5)
		require.Nil(t, err)
		require.Equal(t, int64(5), size)

		reader, _, err := s.Read("abcdefghijkl")
		require.Nil(t, err)
		data, err := io.ReadAll(reader)
		reader.Close()
		require.Nil(t, err)
		require.Equal(t, "hello", string(data))
	})
}

func TestStore_WriteUntrustedLengthBodyShorter(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		// Body has 5 bytes, but we claim 100 — should fail
		_, err := s.Write("abcdefghijkl", strings.NewReader("hello"), 100)
		require.Error(t, err)

		// File should not be readable (was cleaned up)
		_, _, err = s.Read("abcdefghijkl")
		require.Error(t, err)
	})
}

func TestStore_Sync(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, makeOld func(string)) {
		// Write some files
		_, err := s.Write("abcdefghijk0", strings.NewReader("file0"), 0)
		require.Nil(t, err)
		_, err = s.Write("abcdefghijk1", strings.NewReader("file1"), 0)
		require.Nil(t, err)
		_, err = s.Write("abcdefghijk2", strings.NewReader("file2"), 0)
		require.Nil(t, err)

		require.Equal(t, int64(15), s.Size())

		// Set the ID provider to only know about file 0 and 2
		s.localIDs = func() ([]string, error) {
			return []string{"abcdefghijk0", "abcdefghijk2"}, nil
		}

		// Make file 1 old enough to be cleaned up
		makeOld("abcdefghijk1")

		// Run sync
		require.Nil(t, s.sync())

		// File 1 should be deleted (orphan, old enough)
		_, _, err = s.Read("abcdefghijk1")
		require.Error(t, err)

		// Files 0 and 2 should still be readable
		r, _, err := s.Read("abcdefghijk0")
		require.Nil(t, err)
		r.Close()
		r, _, err = s.Read("abcdefghijk2")
		require.Nil(t, err)
		r.Close()

		// Size should be updated
		require.Equal(t, int64(10), s.Size())
	})
}

func TestStore_Sync_SkipsRecentFiles(t *testing.T) {
	forEachBackend(t, testSizeLimit, func(t *testing.T, s *Store, _ func(string)) {
		// Write a file
		_, err := s.Write("abcdefghijk0", strings.NewReader("file0"), 0)
		require.Nil(t, err)

		// Set the ID provider to return empty (no valid IDs)
		s.localIDs = func() ([]string, error) {
			return []string{}, nil
		}

		// File was just created, so it should NOT be deleted (< 1 hour old)
		require.Nil(t, s.sync())

		// File should still exist
		reader, _, err := s.Read("abcdefghijk0")
		require.Nil(t, err)
		reader.Close()
	})
}

// forEachBackend runs f against both the file and S3 backends. It also provides a makeOld
// callback that makes a specific object's timestamp old enough for orphan cleanup (> 1 hour).
// For the file backend, this uses os.Chtimes; for the S3 backend, it sets the object's
// LastModified time in the mock server. Objects start with recent timestamps by default.
func forEachBackend(t *testing.T, totalSizeLimit int64, f func(t *testing.T, s *Store, makeOld func(string))) {
	t.Run("file", func(t *testing.T) {
		dir, s := newTestFileStore(t, totalSizeLimit)
		makeOld := func(id string) {
			oldTime := time.Unix(1, 0)
			os.Chtimes(filepath.Join(dir, id), oldTime, oldTime)
		}
		f(t, s, makeOld)
	})
	t.Run("s3", func(t *testing.T) {
		server, mock := newMockS3Server()
		defer server.Close()
		s := newTestS3Store(t, server, "my-bucket", "pfx", totalSizeLimit)
		makeOld := func(id string) {
			mock.setModTime("my-bucket/pfx/"+id, time.Unix(1, 0))
		}
		f(t, s, makeOld)
	})
}

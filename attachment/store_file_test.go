package attachment

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/util"
)

var (
	oneKilobyteArray = make([]byte, 1024)
)

func TestFileStore_Write_Success(t *testing.T) {
	dir, c := newTestFileStore(t)
	size, err := c.Write("abcdefghijkl", strings.NewReader("normal file"), util.NewFixedLimiter(999))
	require.Nil(t, err)
	require.Equal(t, int64(11), size)
	require.Equal(t, "normal file", readFile(t, dir+"/abcdefghijkl"))
	require.Equal(t, int64(11), c.Size())
	require.Equal(t, int64(10229), c.Remaining())
}

func TestFileStore_Write_Read_Success(t *testing.T) {
	_, c := newTestFileStore(t)
	size, err := c.Write("abcdefghijkl", strings.NewReader("hello world"))
	require.Nil(t, err)
	require.Equal(t, int64(11), size)

	reader, readSize, err := c.Read("abcdefghijkl")
	require.Nil(t, err)
	require.Equal(t, int64(11), readSize)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.Nil(t, err)
	require.Equal(t, "hello world", string(data))
}

func TestFileStore_Write_Remove_Success(t *testing.T) {
	dir, c := newTestFileStore(t) // max = 10k (10240), each = 1k (1024)
	for i := 0; i < 10; i++ {     // 10x999 = 9990
		size, err := c.Write(fmt.Sprintf("abcdefghijk%d", i), bytes.NewReader(make([]byte, 999)))
		require.Nil(t, err)
		require.Equal(t, int64(999), size)
	}
	require.Equal(t, int64(9990), c.Size())
	require.Equal(t, int64(250), c.Remaining())
	require.FileExists(t, dir+"/abcdefghijk1")
	require.FileExists(t, dir+"/abcdefghijk5")

	require.Nil(t, c.Remove("abcdefghijk1", "abcdefghijk5"))
	require.NoFileExists(t, dir+"/abcdefghijk1")
	require.NoFileExists(t, dir+"/abcdefghijk5")
	require.Equal(t, int64(8*999), c.Size())
	require.Equal(t, int64(10240-8*999), c.Remaining())
}

func TestFileStore_Write_FailedTotalSizeLimit(t *testing.T) {
	dir, c := newTestFileStore(t)
	for i := 0; i < 10; i++ {
		size, err := c.Write(fmt.Sprintf("abcdefghijk%d", i), bytes.NewReader(oneKilobyteArray))
		require.Nil(t, err)
		require.Equal(t, int64(1024), size)
	}
	_, err := c.Write("abcdefghijkX", bytes.NewReader(oneKilobyteArray))
	require.Equal(t, util.ErrLimitReached, err)
	require.NoFileExists(t, dir+"/abcdefghijkX")
}

func TestFileStore_Write_FailedAdditionalLimiter(t *testing.T) {
	dir, c := newTestFileStore(t)
	_, err := c.Write("abcdefghijkl", bytes.NewReader(make([]byte, 1001)), util.NewFixedLimiter(1000))
	require.Equal(t, util.ErrLimitReached, err)
	require.NoFileExists(t, dir+"/abcdefghijkl")
}

func TestFileStore_Read_NotFound(t *testing.T) {
	_, c := newTestFileStore(t)
	_, _, err := c.Read("abcdefghijkl")
	require.Error(t, err)
}

func TestFileStore_Sync(t *testing.T) {
	dir, c := newTestFileStore(t)

	// Write some files
	_, err := c.Write("abcdefghijk0", strings.NewReader("file0"))
	require.Nil(t, err)
	_, err = c.Write("abcdefghijk1", strings.NewReader("file1"))
	require.Nil(t, err)
	_, err = c.Write("abcdefghijk2", strings.NewReader("file2"))
	require.Nil(t, err)

	require.Equal(t, int64(15), c.Size())

	// Set the ID provider to only know about file 0 and 2
	c.localIDs = func() ([]string, error) {
		return []string{"abcdefghijk0", "abcdefghijk2"}, nil
	}

	// Make file 1's mod time old enough to be cleaned up (> 1 hour)
	oldTime := time.Unix(1, 0)
	os.Chtimes(dir+"/abcdefghijk1", oldTime, oldTime)

	// Run sync
	require.Nil(t, c.sync())

	// File 1 should be deleted (orphan, old enough)
	require.NoFileExists(t, dir+"/abcdefghijk1")
	require.FileExists(t, dir+"/abcdefghijk0")
	require.FileExists(t, dir+"/abcdefghijk2")

	// Size should be updated
	require.Equal(t, int64(10), c.Size())
}

func TestFileStore_Sync_SkipsRecentFiles(t *testing.T) {
	dir, c := newTestFileStore(t)

	// Write a file
	_, err := c.Write("abcdefghijk0", strings.NewReader("file0"))
	require.Nil(t, err)

	// Set the ID provider to return empty (no valid IDs)
	c.localIDs = func() ([]string, error) {
		return []string{}, nil
	}

	// File was just created, so it should NOT be deleted (< 1 hour old)
	require.Nil(t, c.sync())
	require.FileExists(t, dir+"/abcdefghijk0")
}

func newTestFileStore(t *testing.T) (dir string, cache *Store) {
	t.Helper()
	dir = t.TempDir()
	cache, err := NewFileStore(dir, 10*1024, nil)
	require.Nil(t, err)
	t.Cleanup(func() { cache.Close() })
	return dir, cache
}

func readFile(t *testing.T, f string) string {
	t.Helper()
	b, err := os.ReadFile(f)
	require.Nil(t, err)
	return string(b)
}

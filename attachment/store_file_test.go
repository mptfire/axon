package attachment

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"heckel.io/ntfy/v2/util"
)

var (
	oneKilobyteArray = make([]byte, 1024)
)

func TestFileStore_Write_Success(t *testing.T) {
	dir, s := newTestFileStore(t)
	size, err := s.Write("abcdefghijkl", strings.NewReader("normal file"), util.NewFixedLimiter(999))
	require.Nil(t, err)
	require.Equal(t, int64(11), size)
	require.Equal(t, "normal file", readFile(t, dir+"/abcdefghijkl"))
	require.Equal(t, int64(11), s.Size())
	require.Equal(t, int64(10229), s.Remaining())
}

func TestFileStore_Write_Read_Success(t *testing.T) {
	_, s := newTestFileStore(t)
	size, err := s.Write("abcdefghijkl", strings.NewReader("hello world"))
	require.Nil(t, err)
	require.Equal(t, int64(11), size)

	reader, readSize, err := s.Read("abcdefghijkl")
	require.Nil(t, err)
	require.Equal(t, int64(11), readSize)
	defer reader.Close()
	data, err := io.ReadAll(reader)
	require.Nil(t, err)
	require.Equal(t, "hello world", string(data))
}

func TestFileStore_Write_Remove_Success(t *testing.T) {
	dir, s := newTestFileStore(t) // max = 10k (10240), each = 1k (1024)
	for i := 0; i < 10; i++ {     // 10x999 = 9990
		size, err := s.Write(fmt.Sprintf("abcdefghijk%d", i), bytes.NewReader(make([]byte, 999)))
		require.Nil(t, err)
		require.Equal(t, int64(999), size)
	}
	require.Equal(t, int64(9990), s.Size())
	require.Equal(t, int64(250), s.Remaining())
	require.FileExists(t, dir+"/abcdefghijk1")
	require.FileExists(t, dir+"/abcdefghijk5")

	require.Nil(t, s.Remove("abcdefghijk1", "abcdefghijk5"))
	require.NoFileExists(t, dir+"/abcdefghijk1")
	require.NoFileExists(t, dir+"/abcdefghijk5")
	require.Equal(t, int64(7992), s.Size())
	require.Equal(t, int64(2248), s.Remaining())
}

func TestFileStore_Write_FailedTotalSizeLimit(t *testing.T) {
	dir, s := newTestFileStore(t)
	for i := 0; i < 10; i++ {
		size, err := s.Write(fmt.Sprintf("abcdefghijk%d", i), bytes.NewReader(oneKilobyteArray))
		require.Nil(t, err)
		require.Equal(t, int64(1024), size)
	}
	_, err := s.Write("abcdefghijkX", bytes.NewReader(oneKilobyteArray))
	require.Equal(t, util.ErrLimitReached, err)
	require.NoFileExists(t, dir+"/abcdefghijkX")
}

func TestFileStore_Write_FailedAdditionalLimiter(t *testing.T) {
	dir, s := newTestFileStore(t)
	_, err := s.Write("abcdefghijkl", bytes.NewReader(make([]byte, 1001)), util.NewFixedLimiter(1000))
	require.Equal(t, util.ErrLimitReached, err)
	require.NoFileExists(t, dir+"/abcdefghijkl")
}

func TestFileStore_Read_NotFound(t *testing.T) {
	_, s := newTestFileStore(t)
	_, _, err := s.Read("abcdefghijkl")
	require.Error(t, err)
}

func newTestFileStore(t *testing.T) (dir string, store Store) {
	dir = t.TempDir()
	store, err := NewFileStore(dir, 10*1024)
	require.Nil(t, err)
	return dir, store
}

func readFile(t *testing.T, f string) string {
	b, err := os.ReadFile(f)
	require.Nil(t, err)
	return string(b)
}

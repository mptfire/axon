package attachment

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/util"
)

const tagFileStore = "file_store"

var errFileExists = errors.New("file exists")

type fileStore struct {
	dir              string
	totalSizeCurrent int64
	totalSizeLimit   int64
	mu               sync.Mutex
}

// NewFileStore creates a new file-system backed attachment store
func NewFileStore(dir string, totalSizeLimit int64) (Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	size, err := dirSize(dir)
	if err != nil {
		return nil, err
	}
	return &fileStore{
		dir:              dir,
		totalSizeCurrent: size,
		totalSizeLimit:   totalSizeLimit,
	}, nil
}

func (c *fileStore) Write(id string, in io.Reader, limiters ...util.Limiter) (int64, error) {
	if !fileIDRegex.MatchString(id) {
		return 0, errInvalidFileID
	}
	log.Tag(tagFileStore).Field("message_id", id).Debug("Writing attachment")
	file := filepath.Join(c.dir, id)
	if _, err := os.Stat(file); err == nil {
		return 0, errFileExists
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	limiters = append(limiters, util.NewFixedLimiter(c.Remaining()))
	limitWriter := util.NewLimitWriter(f, limiters...)
	size, err := io.Copy(limitWriter, in)
	if err != nil {
		os.Remove(file)
		return 0, err
	}
	if err := f.Close(); err != nil {
		os.Remove(file)
		return 0, err
	}
	c.mu.Lock()
	c.totalSizeCurrent += size
	c.mu.Unlock()
	return size, nil
}

func (c *fileStore) Read(id string) (io.ReadCloser, int64, error) {
	if !fileIDRegex.MatchString(id) {
		return nil, 0, errInvalidFileID
	}
	file := filepath.Join(c.dir, id)
	stat, err := os.Stat(file)
	if err != nil {
		return nil, 0, err
	}
	f, err := os.Open(file)
	if err != nil {
		return nil, 0, err
	}
	return f, stat.Size(), nil
}

func (c *fileStore) Remove(ids ...string) error {
	for _, id := range ids {
		if !fileIDRegex.MatchString(id) {
			return errInvalidFileID
		}
		log.Tag(tagFileStore).Field("message_id", id).Debug("Deleting attachment")
		file := filepath.Join(c.dir, id)
		if err := os.Remove(file); err != nil {
			log.Tag(tagFileStore).Field("message_id", id).Err(err).Debug("Error deleting attachment")
		}
	}
	size, err := dirSize(c.dir)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.totalSizeCurrent = size
	c.mu.Unlock()
	return nil
}

func (c *fileStore) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalSizeCurrent
}

func (c *fileStore) Remaining() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.totalSizeLimit - c.totalSizeCurrent
	if remaining < 0 {
		return 0
	}
	return remaining
}

func dirSize(dir string) (int64, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var size int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return 0, err
		}
		size += info.Size()
	}
	return size, nil
}

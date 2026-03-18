package attachment

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"
	"time"

	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/model"
	"heckel.io/ntfy/v2/s3"
	"heckel.io/ntfy/v2/util"
)

const (
	tagStore          = "attachment_cache"
	syncInterval      = 15 * time.Minute // How often to run the background sync loop
	orphanGracePeriod = time.Hour        // Don't delete orphaned objects younger than this to avoid races with in-flight uploads
)

var (
	fileIDRegex      = regexp.MustCompile(fmt.Sprintf(`^[-_A-Za-z0-9]{%d}$`, model.MessageIDLength))
	errInvalidFileID = errors.New("invalid file ID")
)

// Store manages attachment storage with shared logic for size tracking, limiting,
// ID validation, and background sync to reconcile storage with the database.
type Store struct {
	backend          backend
	totalSizeCurrent int64
	totalSizeLimit   int64
	localIDs         func() ([]string, error) // returns IDs that should exist
	closeChan        chan struct{}
	mu               sync.Mutex // Protects totalSizeCurrent
}

// NewFileStore creates a new file-system backed attachment cache
func NewFileStore(dir string, totalSizeLimit int64, localIDsFn func() ([]string, error)) (*Store, error) {
	backend, err := newFileBackend(dir)
	if err != nil {
		return nil, err
	}
	return newStore(backend, totalSizeLimit, localIDsFn)
}

// NewS3Store creates a new S3-backed attachment cache. The s3URL must be in the format:
//
//	s3://ACCESS_KEY:SECRET_KEY@BUCKET[/PREFIX]?region=REGION[&endpoint=ENDPOINT]
func NewS3Store(s3URL string, totalSizeLimit int64, localIDs func() ([]string, error)) (*Store, error) {
	config, err := s3.ParseURL(s3URL)
	if err != nil {
		return nil, err
	}
	return newStore(newS3Backend(s3.New(config)), totalSizeLimit, localIDs)
}

func newStore(backend backend, totalSizeLimit int64, localIDs func() ([]string, error)) (*Store, error) {
	c := &Store{
		backend:        backend,
		totalSizeLimit: totalSizeLimit,
		localIDs:       localIDs,
		closeChan:      make(chan struct{}),
	}
	if localIDs != nil {
		go c.syncLoop()
	}
	return c, nil
}

// Write stores an attachment file. The id is validated, and the write is subject to
// the total size limit and any additional limiters.
func (c *Store) Write(id string, in io.Reader, limiters ...util.Limiter) (int64, error) {
	if !fileIDRegex.MatchString(id) {
		return 0, errInvalidFileID
	}
	log.Tag(tagStore).Field("message_id", id).Debug("Writing attachment")
	limiters = append(limiters, util.NewFixedLimiter(c.Remaining()))
	cr := util.NewCountingReader(in)
	lr := util.NewLimitReader(cr, limiters...)
	if err := c.backend.Put(id, lr); err != nil {
		c.backend.Delete(id) //nolint:errcheck
		return 0, err
	}
	size := cr.Total()
	c.mu.Lock()
	c.totalSizeCurrent += size
	c.mu.Unlock()
	return size, nil
}

// Read retrieves an attachment file by ID
func (c *Store) Read(id string) (io.ReadCloser, int64, error) {
	if !fileIDRegex.MatchString(id) {
		return nil, 0, errInvalidFileID
	}
	return c.backend.Get(id)
}

// Remove deletes attachment files by ID. It does NOT recompute the total size;
// the next sync() call will correct it.
func (c *Store) Remove(ids ...string) error {
	for _, id := range ids {
		if !fileIDRegex.MatchString(id) {
			return errInvalidFileID
		}
	}
	return c.backend.Delete(ids...)
}

// sync reconciles the backend storage with the database. It lists all objects,
// deletes orphans (not in the valid ID set and older than 1 hour), and recomputes
// the total size from the remaining objects.
func (c *Store) sync() error {
	log.Tag(tagStore).Debug("Sync: starting sync loop")
	localIDs, err := c.localIDs()
	if err != nil {
		return fmt.Errorf("attachment sync: failed to get valid IDs: %w", err)
	}
	localIDMap := make(map[string]struct{}, len(localIDs))
	for _, id := range localIDs {
		localIDMap[id] = struct{}{}
	}
	remoteObjects, err := c.backend.List()
	if err != nil {
		return fmt.Errorf("attachment sync: failed to list objects: %w", err)
	}
	// Calculate total cache size and collect orphaned attachments, excluding objects younger
	// than the grace period to account for races, and skipping objects with invalid IDs.
	cutoff := time.Now().Add(-orphanGracePeriod)
	var orphanIDs []string
	var totalSize int64
	for _, obj := range remoteObjects {
		if !fileIDRegex.MatchString(obj.ID) {
			continue
		}
		if _, ok := localIDMap[obj.ID]; !ok && obj.LastModified.Before(cutoff) {
			orphanIDs = append(orphanIDs, obj.ID)
		} else {
			totalSize += obj.Size
		}
	}
	log.Tag(tagStore).Debug("Sync: cache size updated to %s", util.FormatSizeHuman(totalSize))
	c.mu.Lock()
	c.totalSizeCurrent = totalSize
	c.mu.Unlock()
	// Delete orphaned attachments
	if len(orphanIDs) > 0 {
		log.Tag(tagStore).Debug("Sync: deleting %d orphaned attachment(s)", len(orphanIDs))
		if err := c.backend.Delete(orphanIDs...); err != nil {
			return fmt.Errorf("attachment sync: failed to delete orphaned objects: %w", err)
		}
	}
	return nil
}

// Size returns the current total size of all attachments
func (c *Store) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalSizeCurrent
}

// Remaining returns the remaining capacity for attachments
func (c *Store) Remaining() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.totalSizeLimit - c.totalSizeCurrent
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Close stops the background sync goroutine
func (c *Store) Close() {
	close(c.closeChan)
}

func (c *Store) syncLoop() {
	if err := c.sync(); err != nil {
		log.Tag(tagStore).Err(err).Warn("Attachment sync failed")
	}
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if err := c.sync(); err != nil {
				log.Tag(tagStore).Err(err).Warn("Attachment sync failed")
			}
		case <-c.closeChan:
			return
		}
	}
}

package attachment

import (
	"context"
	"fmt"
	"io"
	"sync"

	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/s3"
	"heckel.io/ntfy/v2/util"
)

const (
	tagS3Store = "s3_store"
)

type s3Store struct {
	client           *s3.Client
	totalSizeCurrent int64
	totalSizeLimit   int64
	mu               sync.Mutex
}

// NewS3Store creates a new S3-backed attachment store. The s3URL must be in the format:
//
//	s3://ACCESS_KEY:SECRET_KEY@BUCKET[/PREFIX]?region=REGION[&endpoint=ENDPOINT]
func NewS3Store(s3URL string, totalSizeLimit int64) (Store, error) {
	cfg, err := s3.ParseURL(s3URL)
	if err != nil {
		return nil, err
	}
	store := &s3Store{
		client:         s3.New(cfg),
		totalSizeLimit: totalSizeLimit,
	}
	if totalSizeLimit > 0 {
		size, err := store.computeSize()
		if err != nil {
			return nil, fmt.Errorf("s3 store: failed to compute initial size: %w", err)
		}
		store.totalSizeCurrent = size
	}
	return store, nil
}

func (c *s3Store) Write(id string, in io.Reader, limiters ...util.Limiter) (int64, error) {
	if !fileIDRegex.MatchString(id) {
		return 0, errInvalidFileID
	}
	log.Tag(tagS3Store).Field("message_id", id).Debug("Writing attachment to S3")

	// Stream through limiters via an io.Pipe directly to S3. PutObject supports chunked
	// uploads, so no temp file or Content-Length is needed.
	limiters = append(limiters, util.NewFixedLimiter(c.Remaining()))
	pr, pw := io.Pipe()
	lw := util.NewLimitWriter(pw, limiters...)
	var size int64
	var copyErr error
	done := make(chan struct{})
	go func() {
		defer close(done)
		size, copyErr = io.Copy(lw, in)
		if copyErr != nil {
			pw.CloseWithError(copyErr)
		} else {
			pw.Close()
		}
	}()
	putErr := c.client.PutObject(context.Background(), id, pr)
	pr.Close()
	<-done
	if copyErr != nil {
		return 0, copyErr
	}
	if putErr != nil {
		return 0, putErr
	}
	c.mu.Lock()
	c.totalSizeCurrent += size
	c.mu.Unlock()
	return size, nil
}

func (c *s3Store) Read(id string) (io.ReadCloser, int64, error) {
	if !fileIDRegex.MatchString(id) {
		return nil, 0, errInvalidFileID
	}
	return c.client.GetObject(context.Background(), id)
}

func (c *s3Store) Remove(ids ...string) error {
	for _, id := range ids {
		if !fileIDRegex.MatchString(id) {
			return errInvalidFileID
		}
	}
	// S3 DeleteObjects supports up to 1000 keys per call
	for i := 0; i < len(ids); i += 1000 {
		end := i + 1000
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		for _, id := range batch {
			log.Tag(tagS3Store).Field("message_id", id).Debug("Deleting attachment from S3")
		}
		if err := c.client.DeleteObjects(context.Background(), batch); err != nil {
			return err
		}
	}
	// Recalculate totalSizeCurrent via ListObjectsV2 (matches fileStore's dirSize rescan pattern)
	size, err := c.computeSize()
	if err != nil {
		return fmt.Errorf("s3 store: failed to compute size after remove: %w", err)
	}
	c.mu.Lock()
	c.totalSizeCurrent = size
	c.mu.Unlock()
	return nil
}

func (c *s3Store) Size() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.totalSizeCurrent
}

func (c *s3Store) Remaining() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.totalSizeLimit - c.totalSizeCurrent
	if remaining < 0 {
		return 0
	}
	return remaining
}

// computeSize uses ListAllObjects to sum up the total size of all objects with our prefix.
func (c *s3Store) computeSize() (int64, error) {
	objects, err := c.client.ListAllObjects(context.Background())
	if err != nil {
		return 0, err
	}
	var totalSize int64
	for _, obj := range objects {
		totalSize += obj.Size
	}
	return totalSize, nil
}

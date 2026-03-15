package attachment

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/util"
)

const tagS3Store = "s3_store"

type s3Store struct {
	client           *s3.Client
	bucket           string
	prefix           string
	totalSizeCurrent int64
	totalSizeLimit   int64
	mu               sync.Mutex
}

// NewS3Store creates a new S3-backed attachment store. The s3URL must be in the format:
//
//	s3://ACCESS_KEY:SECRET_KEY@BUCKET[/PREFIX]?region=REGION[&endpoint=ENDPOINT]
func NewS3Store(s3URL string, totalSizeLimit int64) (Store, error) {
	bucket, prefix, client, err := parseS3URL(s3URL)
	if err != nil {
		return nil, err
	}
	store := &s3Store{
		client:         client,
		bucket:         bucket,
		prefix:         prefix,
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

func parseS3URL(s3URL string) (bucket string, prefix string, client *s3.Client, err error) {
	u, err := url.Parse(s3URL)
	if err != nil {
		return "", "", nil, fmt.Errorf("s3 store: invalid URL: %w", err)
	}
	if u.Scheme != "s3" {
		return "", "", nil, fmt.Errorf("s3 store: URL scheme must be 's3', got '%s'", u.Scheme)
	}
	if u.Host == "" {
		return "", "", nil, fmt.Errorf("s3 store: bucket name must be specified as host")
	}
	bucket = u.Host
	prefix = strings.TrimPrefix(u.Path, "/")

	accessKey := u.User.Username()
	secretKey, _ := u.User.Password()
	if accessKey == "" || secretKey == "" {
		return "", "", nil, fmt.Errorf("s3 store: access key and secret key must be specified in URL")
	}

	region := u.Query().Get("region")
	if region == "" {
		return "", "", nil, fmt.Errorf("s3 store: region query parameter is required")
	}
	endpoint := u.Query().Get("endpoint")

	cfg := aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}
	var opts []func(*s3.Options)
	if endpoint != "" {
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		})
	}
	client = s3.NewFromConfig(cfg, opts...)
	return bucket, prefix, client, nil
}

func (c *s3Store) objectKey(id string) string {
	if c.prefix != "" {
		return c.prefix + "/" + id
	}
	return id
}

func (c *s3Store) Write(id string, in io.Reader, limiters ...util.Limiter) (int64, error) {
	if !fileIDRegex.MatchString(id) {
		return 0, errInvalidFileID
	}
	log.Tag(tagS3Store).Field("message_id", id).Debug("Writing attachment to S3")

	// Use io.Pipe so we can apply limiters while streaming to S3
	pr, pw := io.Pipe()
	var writeErr error
	var size int64

	limiters = append(limiters, util.NewFixedLimiter(c.Remaining()))
	go func() {
		limitWriter := util.NewLimitWriter(pw, limiters...)
		size, writeErr = io.Copy(limitWriter, in)
		if writeErr != nil {
			pw.CloseWithError(writeErr)
		} else {
			pw.Close()
		}
	}()

	key := c.objectKey(id)
	_, err := c.client.PutObject(context.Background(), &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   pr,
	})
	if err != nil {
		// If the limiter caused the error, return the original write error
		if writeErr != nil {
			return 0, writeErr
		}
		return 0, fmt.Errorf("s3 store: PutObject failed: %w", err)
	}
	if writeErr != nil {
		// The write goroutine failed but PutObject somehow succeeded; clean up
		_, _ = c.client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
			Bucket: aws.String(c.bucket),
			Key:    aws.String(key),
		})
		return 0, writeErr
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
	key := c.objectKey(id)
	resp, err := c.client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("s3 store: GetObject failed: %w", err)
	}
	var size int64
	if resp.ContentLength != nil {
		size = *resp.ContentLength
	}
	return resp.Body, size, nil
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
		objects := make([]s3types.ObjectIdentifier, len(batch))
		for j, id := range batch {
			log.Tag(tagS3Store).Field("message_id", id).Debug("Deleting attachment from S3")
			key := c.objectKey(id)
			objects[j] = s3types.ObjectIdentifier{
				Key: aws.String(key),
			}
		}
		_, err := c.client.DeleteObjects(context.Background(), &s3.DeleteObjectsInput{
			Bucket: aws.String(c.bucket),
			Delete: &s3types.Delete{
				Objects: objects,
				Quiet:   aws.Bool(true),
			},
		})
		if err != nil {
			return fmt.Errorf("s3 store: DeleteObjects failed: %w", err)
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

func (c *s3Store) computeSize() (int64, error) {
	var size int64
	paginator := s3.NewListObjectsV2Paginator(c.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(c.bucket),
		Prefix: aws.String(c.prefixForList()),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(context.Background())
		if err != nil {
			return 0, err
		}
		for _, obj := range page.Contents {
			if obj.Size != nil {
				size += *obj.Size
			}
		}
	}
	return size, nil
}

func (c *s3Store) prefixForList() string {
	if c.prefix != "" {
		return c.prefix + "/"
	}
	return ""
}

package attachment

import (
	"context"
	"io"
	"strings"

	"heckel.io/ntfy/v2/s3"

	"heckel.io/ntfy/v2/log"
)

const tagS3Backend = "s3_backend"

type s3Backend struct {
	client *s3.Client
}

var _ backend = (*s3Backend)(nil)

func newS3Backend(client *s3.Client) *s3Backend {
	return &s3Backend{client: client}
}

func (b *s3Backend) Put(id string, in io.Reader) error {
	return b.client.PutObject(context.Background(), id, in)
}

func (b *s3Backend) Get(id string) (io.ReadCloser, int64, error) {
	return b.client.GetObject(context.Background(), id)
}

func (b *s3Backend) Delete(ids ...string) error {
	// S3 DeleteObjects supports up to 1000 keys per call
	for i := 0; i < len(ids); i += 1000 {
		end := i + 1000
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		for _, id := range batch {
			log.Tag(tagS3Backend).Field("message_id", id).Debug("Deleting attachment from S3")
		}
		if err := b.client.DeleteObjects(context.Background(), batch); err != nil {
			return err
		}
	}
	return nil
}

func (b *s3Backend) List() ([]object, error) {
	objects, err := b.client.ListAllObjects(context.Background())
	if err != nil {
		return nil, err
	}
	prefix := b.client.Prefix
	result := make([]object, 0, len(objects))
	for _, obj := range objects {
		id := obj.Key
		if prefix != "" {
			id = strings.TrimPrefix(id, prefix+"/")
		}
		result = append(result, object{
			ID:           id,
			Size:         obj.Size,
			LastModified: obj.LastModified,
		})
	}
	return result, nil
}

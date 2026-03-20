// Package s3 provides a minimal S3-compatible client that works with AWS S3, DigitalOcean Spaces,
// GCP Cloud Storage, MinIO, Backblaze B2, and other S3-compatible providers. It uses raw HTTP
// requests with AWS Signature V4 signing, no AWS SDK dependency required.
package s3

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // MD5 is required by the S3 protocol for Content-MD5 headers
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"heckel.io/ntfy/v2/log"
)

const (
	tagS3Client = "s3_client"
)

// Client is a minimal S3-compatible client. It supports PutObject, GetObject, DeleteObjects,
// and ListObjectsV2 operations using AWS Signature V4 signing. The bucket and optional key prefix
// are fixed at construction time. All operations target the same bucket and prefix.
//
// Fields must not be modified after the Client is passed to any method or goroutine.
type Client struct {
	config *Config
	http   *http.Client
}

// New creates a new S3 client from the given Config.
func New(config *Config) *Client {
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		config: config,
		http:   httpClient,
	}
}

// PutObject uploads body to the given key. The key is automatically prefixed with the client's
// configured prefix. The body size does not need to be known in advance.
//
// If the entire body fits in a single part (5 MB), it is uploaded with a simple PUT request.
// Otherwise, the body is uploaded using S3 multipart upload, reading one part at a time
// into memory.
func (c *Client) PutObject(ctx context.Context, key string, body io.Reader) error {
	first := make([]byte, partSize)
	n, err := io.ReadFull(body, first)
	if errors.Is(err, io.ErrUnexpectedEOF) || err == io.EOF {
		log.Tag(tagS3Client).Debug("PutObject key=%s size=%d (simple)", key, n)
		return c.putObject(ctx, key, bytes.NewReader(first[:n]), int64(n))
	}
	if err != nil {
		return fmt.Errorf("s3: PutObject read: %w", err)
	}
	log.Tag(tagS3Client).Debug("PutObject key=%s (multipart)", key)
	combined := io.MultiReader(bytes.NewReader(first), body)
	return c.putObjectMultipart(ctx, key, combined)
}

// GetObject downloads an object. The key is automatically prefixed with the client's configured
// prefix. The caller must close the returned ReadCloser.
func (c *Client) GetObject(ctx context.Context, key string) (io.ReadCloser, int64, error) {
	log.Tag(tagS3Client).Debug("GetObject key=%s", key)
	fullKey := c.objectKey(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.objectURL(fullKey), nil)
	if err != nil {
		return nil, 0, fmt.Errorf("s3: GetObject request: %w", err)
	}
	c.signV4(req, emptyPayloadHash)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("s3: GetObject: %w", err)
	}
	if !isHTTPSuccess(resp) {
		err := parseError(resp)
		resp.Body.Close()
		return nil, 0, err
	}
	return resp.Body, resp.ContentLength, nil
}

// DeleteObjects removes multiple objects in a single batch request. Keys are automatically
// prefixed with the client's configured prefix. S3 supports up to 1000 keys per call; the
// caller is responsible for batching if needed.
//
// Even when S3 returns HTTP 200, individual keys may fail. If any per-key errors are present
// in the response, they are returned as a combined error.
func (c *Client) DeleteObjects(ctx context.Context, keys []string) error {
	log.Tag(tagS3Client).Debug("DeleteObjects keys=%d", len(keys))
	var body bytes.Buffer
	body.WriteString("<Delete><Quiet>true</Quiet>")
	for _, key := range keys {
		body.WriteString("<Object><Key>")
		xml.EscapeText(&body, []byte(c.objectKey(key)))
		body.WriteString("</Key></Object>")
	}
	body.WriteString("</Delete>")
	bodyBytes := body.Bytes()

	// Content-MD5 is required by the S3 protocol for DeleteObjects requests.
	md5Sum := md5.Sum(bodyBytes) //nolint:gosec
	contentMD5 := base64.StdEncoding.EncodeToString(md5Sum[:])

	respBody, err := c.doWithBodyAndHeaders(ctx, http.MethodPost, c.config.BucketURL()+"?delete=", bodyBytes,
		map[string]string{"Content-MD5": contentMD5}, "DeleteObjects")
	if err != nil {
		return err
	}

	// S3 may return HTTP 200 with per-key errors in the response body
	var result deleteResult
	if err := xml.Unmarshal(respBody, &result); err != nil {
		return nil // If we can't parse, assume success (Quiet mode returns empty body on success)
	}
	if len(result.Errors) > 0 {
		var msgs []string
		for _, e := range result.Errors {
			msgs = append(msgs, fmt.Sprintf("%s: %s", e.Key, e.Message))
		}
		return fmt.Errorf("s3: DeleteObjects partial failure: %s", strings.Join(msgs, "; "))
	}
	return nil
}

// listObjects performs a single ListObjectsV2 request using the client's configured prefix.
// Use continuationToken for pagination. Set maxKeys to 0 for the server default (typically 1000).
func (c *Client) listObjects(ctx context.Context, continuationToken string, maxKeys int) (*listResult, error) {
	log.Tag(tagS3Client).Debug("ListObjects continuation=%s maxKeys=%d", continuationToken, maxKeys)
	query := url.Values{"list-type": {"2"}}
	if prefix := c.prefixForList(); prefix != "" {
		query.Set("prefix", prefix)
	}
	if continuationToken != "" {
		query.Set("continuation-token", continuationToken)
	}
	if maxKeys > 0 {
		query.Set("max-keys", strconv.Itoa(maxKeys))
	}
	respBody, err := c.do(ctx, http.MethodGet, c.config.BucketURL()+"?"+query.Encode(), nil, "ListObjects")
	if err != nil {
		return nil, err
	}
	var result listObjectsV2Response
	if err := xml.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("s3: ListObjects XML: %w", err)
	}
	objects := make([]Object, len(result.Contents))
	for i, obj := range result.Contents {
		var lastModified time.Time
		if obj.LastModified != "" {
			lastModified, _ = time.Parse(time.RFC3339, obj.LastModified)
		}
		objects[i] = Object{
			Key:          obj.Key,
			Size:         obj.Size,
			LastModified: lastModified,
		}
	}
	return &listResult{
		Objects:               objects,
		IsTruncated:           result.IsTruncated,
		NextContinuationToken: result.NextContinuationToken,
	}, nil
}

// ListAllObjects returns all objects under the client's configured prefix by paginating through
// ListObjectsV2 results automatically. Keys in the returned objects have the prefix stripped,
// so they match the keys used with PutObject/GetObject/DeleteObjects. It stops after 10,000
// pages as a safety valve.
func (c *Client) ListAllObjects(ctx context.Context) ([]Object, error) {
	var all []Object
	var token string
	for page := 0; page < maxPages; page++ {
		result, err := c.listObjects(ctx, token, 0)
		if err != nil {
			return nil, err
		}
		for _, obj := range result.Objects {
			obj.Key = c.stripPrefix(obj.Key)
			all = append(all, obj)
		}
		if !result.IsTruncated {
			return all, nil
		}
		token = result.NextContinuationToken
	}
	return nil, fmt.Errorf("s3: ListAllObjects exceeded %d pages", maxPages)
}

// putObject uploads a body with known size using a simple PUT with UNSIGNED-PAYLOAD.
func (c *Client) putObject(ctx context.Context, key string, body io.Reader, size int64) error {
	fullKey := c.objectKey(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.objectURL(fullKey), body)
	if err != nil {
		return fmt.Errorf("s3: PutObject request: %w", err)
	}
	req.ContentLength = size
	c.signV4(req, unsignedPayload)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("s3: PutObject: %w", err)
	}
	resp.Body.Close()
	if !isHTTPSuccess(resp) {
		return parseError(resp)
	}
	return nil
}

// do creates a request, signs it with an empty payload, executes it, reads the response body,
// and checks for errors. It is used for bodiless GET/POST requests.
func (c *Client) do(ctx context.Context, method, reqURL string, body io.Reader, op string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, body)
	if err != nil {
		return nil, fmt.Errorf("s3: %s request: %w", op, err)
	}
	if body == nil {
		req.ContentLength = 0
	}
	c.signV4(req, emptyPayloadHash)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3: %s: %w", op, err)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("s3: %s read: %w", op, err)
	}
	if !isHTTPSuccess(resp) {
		return nil, parseErrorFromBytes(resp.StatusCode, respBody)
	}
	return respBody, nil
}

// doWithBody is like do, but sends a body with a computed SHA-256 payload hash and Content-Type: application/xml.
func (c *Client) doWithBody(ctx context.Context, method, reqURL string, bodyBytes []byte, op string) ([]byte, error) {
	return c.doWithBodyAndHeaders(ctx, method, reqURL, bodyBytes, nil, op)
}

// doWithBodyAndHeaders is like doWithBody, but allows setting additional headers (e.g. Content-MD5).
func (c *Client) doWithBodyAndHeaders(ctx context.Context, method, reqURL string, bodyBytes []byte, headers map[string]string, op string) ([]byte, error) {
	payloadHash := sha256Hex(bodyBytes)
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("s3: %s request: %w", op, err)
	}
	req.ContentLength = int64(len(bodyBytes))
	req.Header.Set("Content-Type", "application/xml")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c.signV4(req, payloadHash)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3: %s: %w", op, err)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("s3: %s read: %w", op, err)
	}
	if !isHTTPSuccess(resp) {
		return nil, parseErrorFromBytes(resp.StatusCode, respBody)
	}
	return respBody, nil
}

// objectKey prepends the configured prefix to the given key.
func (c *Client) objectKey(key string) string {
	if c.config.Prefix != "" {
		return c.config.Prefix + "/" + key
	}
	return key
}

// prefixForList returns the prefix to use in ListObjectsV2 requests,
// with a trailing slash so that only objects under the prefix directory are returned.
func (c *Client) prefixForList() string {
	if c.config.Prefix != "" {
		return c.config.Prefix + "/"
	}
	return ""
}

// stripPrefix removes the configured prefix from a key returned by ListObjectsV2,
// so keys match what was passed to PutObject/GetObject/DeleteObjects.
func (c *Client) stripPrefix(key string) string {
	if c.config.Prefix != "" {
		return strings.TrimPrefix(key, c.config.Prefix+"/")
	}
	return key
}

// objectURL returns the full URL for an object (key should already include the prefix).
// Each path segment is URI-encoded to handle special characters in keys.
func (c *Client) objectURL(key string) string {
	segments := strings.Split(key, "/")
	for i, seg := range segments {
		segments[i] = uriEncode(seg)
	}
	return c.config.BucketURL() + "/" + strings.Join(segments, "/")
}

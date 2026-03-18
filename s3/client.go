// Package s3 provides a minimal S3-compatible client that works with AWS S3, DigitalOcean Spaces,
// GCP Cloud Storage, MinIO, Backblaze B2, and other S3-compatible providers. It uses raw HTTP
// requests with AWS Signature V4 signing, no AWS SDK dependency required.
package s3

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // MD5 is required by the S3 protocol for Content-MD5 headers
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	AccessKey  string       // AWS access key ID
	SecretKey  string       // AWS secret access key
	Region     string       // e.g. "us-east-1"
	Endpoint   string       // host[:port] only, e.g. "s3.amazonaws.com" or "nyc3.digitaloceanspaces.com"
	Bucket     string       // S3 bucket name
	Prefix     string       // optional key prefix (e.g. "attachments"); prepended to all keys automatically
	PathStyle  bool         // if true, use path-style addressing; otherwise virtual-hosted-style
	HTTPClient *http.Client // if nil, http.DefaultClient is used
}

// New creates a new S3 client from the given Config.
func New(config *Config) *Client {
	return &Client{
		AccessKey: config.AccessKey,
		SecretKey: config.SecretKey,
		Region:    config.Region,
		Endpoint:  config.Endpoint,
		Bucket:    config.Bucket,
		Prefix:    config.Prefix,
		PathStyle: config.PathStyle,
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
	resp, err := c.httpClient().Do(req)
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
	payloadHash := sha256Hex(bodyBytes)

	// Content-MD5 is required by the S3 protocol for DeleteObjects requests.
	md5Sum := md5.Sum(bodyBytes) //nolint:gosec
	contentMD5 := base64.StdEncoding.EncodeToString(md5Sum[:])

	reqURL := c.bucketURL() + "?delete="
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("s3: DeleteObjects request: %w", err)
	}
	req.ContentLength = int64(len(bodyBytes))
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("Content-MD5", contentMD5)
	c.signV4(req, payloadHash)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("s3: DeleteObjects: %w", err)
	}
	defer resp.Body.Close()
	if !isHTTPSuccess(resp) {
		return parseError(resp)
	}

	// S3 may return HTTP 200 with per-key errors in the response body
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("s3: DeleteObjects read response: %w", err)
	}
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

// ListObjects performs a single ListObjectsV2 request using the client's configured prefix.
// Use continuationToken for pagination. Set maxKeys to 0 for the server default (typically 1000).
func (c *Client) ListObjects(ctx context.Context, continuationToken string, maxKeys int) (*ListResult, error) {
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.bucketURL()+"?"+query.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("s3: ListObjects request: %w", err)
	}
	c.signV4(req, emptyPayloadHash)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3: ListObjects: %w", err)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("s3: ListObjects read: %w", err)
	}
	if !isHTTPSuccess(resp) {
		return nil, parseErrorFromBytes(resp.StatusCode, respBody)
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
	return &ListResult{
		Objects:               objects,
		IsTruncated:           result.IsTruncated,
		NextContinuationToken: result.NextContinuationToken,
	}, nil
}

// ListAllObjects returns all objects under the client's configured prefix by paginating through
// ListObjectsV2 results automatically. It stops after 10,000 pages as a safety valve.
func (c *Client) ListAllObjects(ctx context.Context) ([]Object, error) {
	var all []Object
	var token string
	for page := 0; page < maxPages; page++ {
		result, err := c.ListObjects(ctx, token, 0)
		if err != nil {
			return nil, err
		}
		all = append(all, result.Objects...)
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
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("s3: PutObject: %w", err)
	}
	defer resp.Body.Close()
	if !isHTTPSuccess(resp) {
		return parseError(resp)
	}
	return nil
}

// putObjectMultipart uploads body using S3 multipart upload. It reads the body in partSize
// chunks, uploading each as a separate part. This allows uploading without knowing the total
// body size in advance.
func (c *Client) putObjectMultipart(ctx context.Context, key string, body io.Reader) error {
	fullKey := c.objectKey(key)

	// Step 1: Initiate multipart upload
	uploadID, err := c.initiateMultipartUpload(ctx, fullKey)
	if err != nil {
		return err
	}

	// Step 2: Upload parts
	var parts []completedPart
	buf := make([]byte, partSize)
	partNumber := 1
	for {
		n, err := io.ReadFull(body, buf)
		if n > 0 {
			etag, uploadErr := c.uploadPart(ctx, fullKey, uploadID, partNumber, buf[:n])
			if uploadErr != nil {
				c.abortMultipartUpload(ctx, fullKey, uploadID)
				return uploadErr
			}
			parts = append(parts, completedPart{PartNumber: partNumber, ETag: etag})
			partNumber++
		}
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			break
		}
		if err != nil {
			c.abortMultipartUpload(ctx, fullKey, uploadID)
			return fmt.Errorf("s3: PutObject read: %w", err)
		}
	}

	// Step 3: Complete multipart upload
	return c.completeMultipartUpload(ctx, fullKey, uploadID, parts)
}

// initiateMultipartUpload starts a new multipart upload and returns the upload ID.
func (c *Client) initiateMultipartUpload(ctx context.Context, fullKey string) (string, error) {
	reqURL := c.objectURL(fullKey) + "?uploads"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return "", fmt.Errorf("s3: InitiateMultipartUpload request: %w", err)
	}
	req.ContentLength = 0
	c.signV4(req, emptyPayloadHash)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("s3: InitiateMultipartUpload: %w", err)
	}
	defer resp.Body.Close()
	if !isHTTPSuccess(resp) {
		return "", parseError(resp)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("s3: InitiateMultipartUpload read: %w", err)
	}
	var result initiateMultipartUploadResult
	if err := xml.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("s3: InitiateMultipartUpload XML: %w", err)
	}
	log.Tag(tagS3Client).Debug("InitiateMultipartUpload key=%s uploadId=%s", fullKey, result.UploadID)
	return result.UploadID, nil
}

// uploadPart uploads a single part of a multipart upload and returns the ETag.
func (c *Client) uploadPart(ctx context.Context, fullKey, uploadID string, partNumber int, data []byte) (string, error) {
	log.Tag(tagS3Client).Debug("UploadPart key=%s part=%d size=%d", fullKey, partNumber, len(data))
	reqURL := fmt.Sprintf("%s?partNumber=%d&uploadId=%s", c.objectURL(fullKey), partNumber, url.QueryEscape(uploadID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, reqURL, bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("s3: UploadPart request: %w", err)
	}
	req.ContentLength = int64(len(data))
	c.signV4(req, unsignedPayload)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("s3: UploadPart: %w", err)
	}
	defer resp.Body.Close()
	if !isHTTPSuccess(resp) {
		return "", parseError(resp)
	}
	etag := resp.Header.Get("ETag")
	return etag, nil
}

// completeMultipartUpload finalizes a multipart upload with the given parts.
func (c *Client) completeMultipartUpload(ctx context.Context, fullKey, uploadID string, parts []completedPart) error {
	log.Tag(tagS3Client).Debug("CompleteMultipartUpload key=%s uploadId=%s parts=%d", fullKey, uploadID, len(parts))
	var body bytes.Buffer
	body.WriteString("<CompleteMultipartUpload>")
	for _, p := range parts {
		fmt.Fprintf(&body, "<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>", p.PartNumber, p.ETag)
	}
	body.WriteString("</CompleteMultipartUpload>")
	bodyBytes := body.Bytes()
	payloadHash := sha256Hex(bodyBytes)

	reqURL := fmt.Sprintf("%s?uploadId=%s", c.objectURL(fullKey), url.QueryEscape(uploadID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("s3: CompleteMultipartUpload request: %w", err)
	}
	req.ContentLength = int64(len(bodyBytes))
	req.Header.Set("Content-Type", "application/xml")
	c.signV4(req, payloadHash)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("s3: CompleteMultipartUpload: %w", err)
	}
	defer resp.Body.Close()
	if !isHTTPSuccess(resp) {
		return parseError(resp)
	}
	// Read response body to check for errors (S3 can return 200 with an error body)
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("s3: CompleteMultipartUpload read: %w", err)
	}
	// Check if the response contains an error
	var errResp ErrorResponse
	if xml.Unmarshal(respBody, &errResp) == nil && errResp.Code != "" {
		errResp.StatusCode = resp.StatusCode
		return &errResp
	}
	return nil
}

// abortMultipartUpload cancels an in-progress multipart upload. Called on error to clean up.
func (c *Client) abortMultipartUpload(ctx context.Context, fullKey, uploadID string) {
	log.Tag(tagS3Client).Debug("AbortMultipartUpload key=%s uploadId=%s", fullKey, uploadID)
	reqURL := fmt.Sprintf("%s?uploadId=%s", c.objectURL(fullKey), url.QueryEscape(uploadID))
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, reqURL, nil)
	if err != nil {
		return
	}
	c.signV4(req, emptyPayloadHash)
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// signV4 signs req in place using AWS Signature V4. payloadHash is the hex-encoded SHA-256
// of the request body, or the literal string "UNSIGNED-PAYLOAD" for streaming uploads.
func (c *Client) signV4(req *http.Request, payloadHash string) {
	now := time.Now().UTC()
	datestamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	// Required headers
	req.Header.Set("Host", c.hostHeader())
	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)

	// Canonical headers (all headers we set, sorted by lowercase key)
	signedKeys := make([]string, 0, len(req.Header))
	canonHeaders := make(map[string]string, len(req.Header))
	for k := range req.Header {
		lk := strings.ToLower(k)
		signedKeys = append(signedKeys, lk)
		canonHeaders[lk] = strings.TrimSpace(req.Header.Get(k))
	}
	sort.Strings(signedKeys)
	signedHeadersStr := strings.Join(signedKeys, ";")
	var chBuf strings.Builder
	for _, k := range signedKeys {
		chBuf.WriteString(k)
		chBuf.WriteByte(':')
		chBuf.WriteString(canonHeaders[k])
		chBuf.WriteByte('\n')
	}

	// Canonical request
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL),
		canonicalQueryString(req.URL.Query()),
		chBuf.String(),
		signedHeadersStr,
		payloadHash,
	}, "\n")

	// String to sign
	credentialScope := datestamp + "/" + c.Region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + credentialScope + "\n" + sha256Hex([]byte(canonicalRequest))

	// Signing key
	signingKey := hmacSHA256(hmacSHA256(hmacSHA256(hmacSHA256(
		[]byte("AWS4"+c.SecretKey), []byte(datestamp)),
		[]byte(c.Region)),
		[]byte("s3")),
		[]byte("aws4_request"))

	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		c.AccessKey, credentialScope, signedHeadersStr, signature,
	))
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// objectKey prepends the configured prefix to the given key.
func (c *Client) objectKey(key string) string {
	if c.Prefix != "" {
		return c.Prefix + "/" + key
	}
	return key
}

// prefixForList returns the prefix to use in ListObjectsV2 requests,
// with a trailing slash so that only objects under the prefix directory are returned.
func (c *Client) prefixForList() string {
	if c.Prefix != "" {
		return c.Prefix + "/"
	}
	return ""
}

// bucketURL returns the base URL for bucket-level operations.
func (c *Client) bucketURL() string {
	if c.PathStyle {
		return fmt.Sprintf("https://%s/%s", c.Endpoint, c.Bucket)
	}
	return fmt.Sprintf("https://%s.%s", c.Bucket, c.Endpoint)
}

// objectURL returns the full URL for an object (key should already include the prefix).
// Each path segment is URI-encoded to handle special characters in keys.
func (c *Client) objectURL(key string) string {
	segments := strings.Split(key, "/")
	for i, seg := range segments {
		segments[i] = uriEncode(seg)
	}
	return c.bucketURL() + "/" + strings.Join(segments, "/")
}

// hostHeader returns the value for the Host header.
func (c *Client) hostHeader() string {
	if c.PathStyle {
		return c.Endpoint
	}
	return c.Bucket + "." + c.Endpoint
}

package s3

import "fmt"

// Config holds the parsed fields from an S3 URL. Use ParseURL to create one from a URL string.
type Config struct {
	Endpoint  string // host[:port] only, e.g. "s3.us-east-1.amazonaws.com"
	PathStyle bool
	Bucket    string
	Prefix    string
	Region    string
	AccessKey string
	SecretKey string
}

// Object represents an S3 object returned by list operations.
type Object struct {
	Key  string
	Size int64
}

// ListResult holds the response from a ListObjectsV2 call.
type ListResult struct {
	Objects               []Object
	IsTruncated           bool
	NextContinuationToken string
}

// ErrorResponse is returned when S3 responds with a non-2xx status code.
type ErrorResponse struct {
	StatusCode int
	Code       string `xml:"Code"`
	Message    string `xml:"Message"`
	Body       string `xml:"-"` // raw response body
}

func (e *ErrorResponse) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("s3: %s (HTTP %d): %s", e.Code, e.StatusCode, e.Message)
	}
	return fmt.Sprintf("s3: HTTP %d: %s", e.StatusCode, e.Body)
}

// listObjectsV2Response is the XML response from S3 ListObjectsV2
type listObjectsV2Response struct {
	Contents              []listObject `xml:"Contents"`
	IsTruncated           bool         `xml:"IsTruncated"`
	NextContinuationToken string       `xml:"NextContinuationToken"`
}

type listObject struct {
	Key  string `xml:"Key"`
	Size int64  `xml:"Size"`
}

// deleteResult is the XML response from S3 DeleteObjects
type deleteResult struct {
	Errors []deleteError `xml:"Error"`
}

type deleteError struct {
	Key     string `xml:"Key"`
	Code    string `xml:"Code"`
	Message string `xml:"Message"`
}

// initiateMultipartUploadResult is the XML response from S3 InitiateMultipartUpload
type initiateMultipartUploadResult struct {
	UploadID string `xml:"UploadId"`
}

// completedPart represents a successfully uploaded part for CompleteMultipartUpload
type completedPart struct {
	PartNumber int
	ETag       string
}

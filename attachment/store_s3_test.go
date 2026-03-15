package attachment

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseS3URL_Success(t *testing.T) {
	bucket, prefix, client, err := parseS3URL("s3://AKID:SECRET@my-bucket/attachments?region=us-east-1")
	require.Nil(t, err)
	require.Equal(t, "my-bucket", bucket)
	require.Equal(t, "attachments", prefix)
	require.NotNil(t, client)
}

func TestParseS3URL_NoPrefix(t *testing.T) {
	bucket, prefix, client, err := parseS3URL("s3://AKID:SECRET@my-bucket?region=us-east-1")
	require.Nil(t, err)
	require.Equal(t, "my-bucket", bucket)
	require.Equal(t, "", prefix)
	require.NotNil(t, client)
}

func TestParseS3URL_WithEndpoint(t *testing.T) {
	bucket, prefix, client, err := parseS3URL("s3://AKID:SECRET@my-bucket/prefix?region=us-east-1&endpoint=https://s3.example.com")
	require.Nil(t, err)
	require.Equal(t, "my-bucket", bucket)
	require.Equal(t, "prefix", prefix)
	require.NotNil(t, client)
}

func TestParseS3URL_NestedPrefix(t *testing.T) {
	bucket, prefix, _, err := parseS3URL("s3://AKID:SECRET@my-bucket/a/b/c?region=us-east-1")
	require.Nil(t, err)
	require.Equal(t, "my-bucket", bucket)
	require.Equal(t, "a/b/c", prefix)
}

func TestParseS3URL_MissingRegion(t *testing.T) {
	_, _, _, err := parseS3URL("s3://AKID:SECRET@my-bucket")
	require.Error(t, err)
	require.Contains(t, err.Error(), "region")
}

func TestParseS3URL_MissingCredentials(t *testing.T) {
	_, _, _, err := parseS3URL("s3://my-bucket?region=us-east-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "access key")
}

func TestParseS3URL_MissingSecretKey(t *testing.T) {
	_, _, _, err := parseS3URL("s3://AKID@my-bucket?region=us-east-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "secret key")
}

func TestParseS3URL_WrongScheme(t *testing.T) {
	_, _, _, err := parseS3URL("http://AKID:SECRET@my-bucket?region=us-east-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "scheme")
}

func TestParseS3URL_EmptyBucket(t *testing.T) {
	_, _, _, err := parseS3URL("s3://AKID:SECRET@?region=us-east-1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "bucket")
}

func TestS3Store_ObjectKey(t *testing.T) {
	s := &s3Store{prefix: "attachments"}
	require.Equal(t, "attachments/abcdefghijkl", s.objectKey("abcdefghijkl"))

	s2 := &s3Store{prefix: ""}
	require.Equal(t, "abcdefghijkl", s2.objectKey("abcdefghijkl"))
}

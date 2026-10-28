// Package objectstore stores files in an S3-compatible bucket (Cloudflare R2). It knows
// nothing of the modules: callers give it a key and bytes, and get back a public URL.
package objectstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// operationTimeout bounds each call to the bucket.
const operationTimeout = 15 * time.Second

// Config says how to reach the bucket. The bucket stays private: callers read objects through Get.
type Config struct {
	Endpoint        string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
}

// Client is a bucket.
type Client struct {
	s3     *s3.Client
	bucket string
}

// New builds the client. It does not connect: the first call does.
func New(cfg Config) *Client {
	return &Client{
		s3: s3.New(s3.Options{
			Region:       "auto", // R2 has no regions.
			BaseEndpoint: aws.String(cfg.Endpoint),
			Credentials:  credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
			UsePathStyle: true,
			// R2 does not accept the optional checksums newer SDKs add by default.
			RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
			ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
		}),
		bucket: cfg.Bucket,
	}
}

// Put stores data under key. Objects are immutable: a new upload gets a new key.
func (c *Client) Put(ctx context.Context, key, contentType string, data []byte) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	_, err := c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// Delete removes key. Deleting a missing key is not an error.
func (c *Client) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, operationTimeout)
	defer cancel()
	if _, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}); err != nil {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

// Object is a stored file being read; the caller closes Body.
type Object struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

// ErrNotFound: there is no object under that key.
var ErrNotFound = errors.New("object not found")

// Get opens key for reading. The read is bounded by ctx, which the caller keeps open
// while it copies Body.
func (c *Client) Get(ctx context.Context, key string) (Object, error) {
	out, err := c.s3.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	var missing *types.NoSuchKey
	if errors.As(err, &missing) {
		return Object{}, ErrNotFound
	}
	if err != nil {
		return Object{}, fmt.Errorf("get object: %w", err)
	}
	return Object{Body: out.Body, ContentType: aws.ToString(out.ContentType), Size: aws.ToInt64(out.ContentLength)}, nil
}

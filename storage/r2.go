package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type R2 struct {
	client *s3.Client
	bucket string
}

var (
	defaultOnce sync.Once
	defaultR2   *R2
	defaultErr  error
)

func Default() (*R2, error) {
	defaultOnce.Do(func() {
		defaultR2, defaultErr = newR2FromEnv()
	})
	if defaultErr != nil {
		return nil, defaultErr
	}
	return defaultR2, nil
}

func PublicBaseURL() string {
	baseURL := strings.TrimRight(strings.TrimSpace(os.Getenv("R2_PUBLIC_BASE_URL")), "/")
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return baseURL
}

func (r2 *R2) PublicURL(key string) string {
	baseURL := PublicBaseURL()
	if baseURL == "" || !validKey(key) {
		return ""
	}
	segments := strings.Split(key, "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	return baseURL + "/" + strings.Join(segments, "/")
}

func (r2 *R2) Put(ctx context.Context, key string, body io.Reader, contentType string) error {
	if !validKey(key) {
		return errors.New("invalid R2 object key")
	}
	_, err := r2.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(r2.bucket),
		Key:         aws.String(key),
		Body:        body,
		ContentType: aws.String(contentType),
		CacheControl: aws.String(
			"public, max-age=31536000, immutable",
		),
	})
	if err != nil {
		return fmt.Errorf("upload R2 object: %w", err)
	}
	return nil
}

func (r2 *R2) Get(ctx context.Context, key string) (io.ReadCloser, string, error) {
	if !validKey(key) {
		return nil, "", errors.New("invalid R2 object key")
	}
	output, err := r2.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(r2.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", fmt.Errorf("get R2 object: %w", err)
	}
	contentType := aws.ToString(output.ContentType)
	return output.Body, contentType, nil
}

func (r2 *R2) Exists(ctx context.Context, key string) error {
	if !validKey(key) {
		return errors.New("invalid R2 object key")
	}
	_, err := r2.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(r2.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("check R2 object: %w", err)
	}
	return nil
}

func (r2 *R2) Delete(ctx context.Context, key string) error {
	if !validKey(key) {
		return errors.New("invalid R2 object key")
	}
	if _, err := r2.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(r2.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("delete R2 object: %w", err)
	}
	return nil
}

func (r2 *R2) DeleteIfExists(ctx context.Context, key string) error {
	if err := r2.Exists(ctx, key); err != nil {
		if IsNotFound(err) {
			return nil
		}
		return err
	}
	return r2.Delete(ctx, key)
}

func IsNotFound(err error) bool {
	var apiError smithy.APIError
	return errors.As(err, &apiError) &&
		(apiError.ErrorCode() == "NoSuchKey" || apiError.ErrorCode() == "NotFound")
}

func newR2FromEnv() (*R2, error) {
	accountID := strings.TrimSpace(os.Getenv("R2_ACCOUNT_ID"))
	accessKeyID := strings.TrimSpace(os.Getenv("R2_ACCESS_KEY_ID"))
	secretAccessKey := strings.TrimSpace(os.Getenv("R2_SECRET_ACCESS_KEY"))
	bucket := strings.TrimSpace(os.Getenv("R2_BUCKET"))
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("R2_ENDPOINT_URL")), "/")
	if accountID == "" || accessKeyID == "" || secretAccessKey == "" || bucket == "" {
		return nil, errors.New("R2 is not configured: set R2_ACCOUNT_ID, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY, and R2_BUCKET")
	}
	if endpoint == "" {
		endpoint = "https://" + accountID + ".r2.cloudflarestorage.com"
	}
	parsedEndpoint, err := url.Parse(endpoint)
	if err != nil || parsedEndpoint.Scheme != "https" || parsedEndpoint.Host == "" || parsedEndpoint.User != nil {
		return nil, errors.New("R2_ENDPOINT_URL must be a valid HTTPS URL")
	}

	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(endpoint),
		Credentials: credentials.NewStaticCredentialsProvider(
			accessKeyID,
			secretAccessKey,
			"",
		),
		UsePathStyle: true,
	})
	return &R2{client: client, bucket: bucket}, nil
}

func validKey(key string) bool {
	if key == "" || strings.ContainsAny(key, "\\\x00:") ||
		path.IsAbs(key) || path.Clean(key) != key {
		return false
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

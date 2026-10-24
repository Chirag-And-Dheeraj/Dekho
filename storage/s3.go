package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"video-streaming-server/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func NewS3Client(ctx context.Context) (*s3.Client, error) {
	cfg := config.AppConfig
	if cfg.AppwriteS3Endpoint == "" || cfg.AppwriteProjectID == "" || cfg.AppwriteKey == "" {
		return nil, fmt.Errorf("APPWRITE_S3_ENDPOINT, APPWRITE_PROJECT_ID, and APPWRITE_KEY must be configured")
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.AppwriteS3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AppwriteProjectID, cfg.AppwriteKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load S3 client configuration: %w", err)
	}
	return s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(cfg.AppwriteS3Endpoint)
		options.UsePathStyle = true
	}), nil
}

const MultipartPartSize int64 = 10 * 1024 * 1024

func CreateMultipartUpload(ctx context.Context, bucket, key, contentType string) (string, error) {
	client, err := NewS3Client(ctx)
	if err != nil {
		return "", err
	}
	result, err := client.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String(key), ContentType: aws.String(contentType),
	})
	if err != nil {
		return "", fmt.Errorf("create S3 multipart upload: %w", err)
	}
	if result.UploadId == nil || *result.UploadId == "" {
		return "", fmt.Errorf("create S3 multipart upload: empty upload ID")
	}
	return *result.UploadId, nil
}

func PresignUploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int32) (string, error) {
	client, err := NewS3Client(ctx)
	if err != nil {
		return "", err
	}
	request, err := s3.NewPresignClient(client).PresignUploadPart(ctx, &s3.UploadPartInput{
		Bucket: aws.String(bucket), Key: aws.String(key), UploadId: aws.String(uploadID), PartNumber: aws.Int32(partNumber),
	}, func(options *s3.PresignOptions) {
		options.Expires = 2 * time.Hour
	})
	if err != nil {
		return "", fmt.Errorf("presign S3 upload part: %w", err)
	}
	return request.URL, nil
}

func AbortMultipartUpload(ctx context.Context, client *s3.Client, bucket, key, uploadID string) error {
	_, err := client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket: aws.String(bucket), Key: aws.String(key), UploadId: aws.String(uploadID),
	})
	if err != nil {
		return fmt.Errorf("abort S3 multipart upload: %w", err)
	}
	return nil
}

func PresignGet(ctx context.Context, bucket, key string) (string, error) {
	client, err := NewS3Client(ctx)
	if err != nil {
		return "", err
	}
	request, err := s3.NewPresignClient(client).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key),
	}, func(options *s3.PresignOptions) {
		options.Expires = 5 * time.Minute
	})
	if err != nil {
		return "", fmt.Errorf("presign S3 download: %w", err)
	}
	return request.URL, nil
}

func PutObject(ctx context.Context, bucket, key, contentType string, body io.Reader, size int64) error {
	client, err := NewS3Client(ctx)
	if err != nil {
		return err
	}
	_, err = client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket), Key: aws.String(key), ContentType: aws.String(contentType),
		Body: body, ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("put S3 object %q: %w", key, err)
	}
	return nil
}

func GetObject(ctx context.Context, bucket, key string) (*s3.GetObjectOutput, error) {
	client, err := NewS3Client(ctx)
	if err != nil {
		return nil, err
	}
	object, err := client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("get S3 object %q: %w", key, err)
	}
	return object, nil
}

func GetObjectSize(ctx context.Context, client *s3.Client, bucket, key string) (int64, error) {
	result, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket), Prefix: aws.String(key), MaxKeys: aws.Int32(1),
	})
	if err != nil {
		return 0, fmt.Errorf("list S3 object %q: %w", key, err)
	}
	for _, object := range result.Contents {
		if object.Key != nil && *object.Key == key && object.Size != nil {
			return *object.Size, nil
		}
	}
	return 0, fmt.Errorf("S3 object %q was not found", key)
}

package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"sendtome/internal/config"
)

// S3 speaks the S3-compatible protocol. One implementation covers the major
// object-storage vendors; provider presets only fill in endpoint and addressing
// defaults. Reads are proxied by the application so download limits cannot be
// bypassed with a direct bucket URL.
type S3 struct {
	client    *s3.Client
	bucket    string
	prefix    string
	provider  string
	endpoint  string
	region    string
	pathStyle bool
}

type s3ProviderPreset struct {
	defaultRegion  string
	pathStyle      bool
	requireRegion  bool
	requireAccount bool
	requireHost    bool
	displayName    string
}

var s3Providers = map[string]s3ProviderPreset{
	"aws": {
		defaultRegion: "us-east-1",
		pathStyle:     false,
		displayName:   "Amazon S3",
	},
	"r2": {
		defaultRegion:  "auto",
		pathStyle:      true,
		requireAccount: true,
		displayName:    "Cloudflare R2",
	},
	"aliyun": {
		pathStyle:     false,
		requireRegion: true,
		displayName:   "Alibaba Cloud OSS",
	},
	"tencent": {
		pathStyle:     true,
		requireRegion: true,
		displayName:   "Tencent Cloud COS",
	},
	"huawei": {
		pathStyle:     true,
		requireRegion: true,
		displayName:   "Huawei Cloud OBS",
	},
	"qiniu": {
		pathStyle:     true,
		requireRegion: true,
		displayName:   "Qiniu Kodo",
	},
	"backblaze": {
		pathStyle:     true,
		requireRegion: true,
		displayName:   "Backblaze B2",
	},
	"minio": {
		defaultRegion: "us-east-1",
		pathStyle:     true,
		requireHost:   true,
		displayName:   "MinIO",
	},
	"custom": {
		defaultRegion: "us-east-1",
		pathStyle:     true,
		requireHost:   true,
		displayName:   "Custom S3-compatible storage",
	},
}

func NewS3(cfg config.StorageConfig) (*S3, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.S3Provider))
	if provider == "" {
		provider = "aws"
	}
	preset, ok := s3Providers[provider]
	if !ok {
		return nil, fmt.Errorf("unsupported S3_PROVIDER %q (use aws, r2, aliyun, tencent, huawei, qiniu, backblaze, minio or custom)", provider)
	}
	if strings.TrimSpace(cfg.S3Bucket) == "" {
		return nil, fmt.Errorf("S3_BUCKET is required when STORAGE_DRIVER=s3")
	}

	region := strings.TrimSpace(cfg.S3Region)
	if region == "" {
		region = preset.defaultRegion
	}
	if preset.requireRegion && region == "" {
		return nil, fmt.Errorf("S3_REGION is required for %s", preset.displayName)
	}
	if region == "" {
		region = "us-east-1"
	}

	endpoint := normalizeEndpoint(cfg.S3Endpoint)
	if endpoint == "" {
		derived, err := deriveEndpoint(provider, region, cfg.S3AccountID)
		if err != nil {
			return nil, err
		}
		endpoint = derived
	}
	if endpoint == "" && preset.requireHost {
		return nil, fmt.Errorf("S3_ENDPOINT is required for %s", preset.displayName)
	}
	if endpoint != "" {
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return nil, fmt.Errorf("S3_ENDPOINT must be a valid URL, for example https://s3.example.com")
		}
	}

	if (cfg.S3AccessKeyID == "") != (cfg.S3SecretAccessKey == "") {
		return nil, fmt.Errorf("S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY must be provided together")
	}
	if provider != "aws" && cfg.S3AccessKeyID == "" {
		return nil, fmt.Errorf("S3_ACCESS_KEY_ID and S3_SECRET_ACCESS_KEY are required for %s", preset.displayName)
	}

	loadOptions := []func(*awsconfig.LoadOptions) error{awsconfig.WithRegion(region)}
	if cfg.S3AccessKeyID != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.S3AccessKeyID, cfg.S3SecretAccessKey, cfg.S3SessionToken),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(), loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load S3 configuration: %w", err)
	}

	pathStyle := preset.pathStyle
	if cfg.S3ForcePathStyle != nil {
		pathStyle = *cfg.S3ForcePathStyle
	}

	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		if endpoint != "" {
			options.BaseEndpoint = aws.String(endpoint)
		}
		options.UsePathStyle = pathStyle
		// Several S3-compatible vendors reject the newer automatic checksum
		// headers. Only calculate them when an operation requires it.
		options.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		options.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	})

	return &S3{
		client:    client,
		bucket:    cfg.S3Bucket,
		prefix:    strings.Trim(cfg.S3Prefix, "/"),
		provider:  provider,
		endpoint:  endpoint,
		region:    region,
		pathStyle: pathStyle,
	}, nil
}

func (s *S3) Put(ctx context.Context, key string, content io.Reader, size int64, contentType string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("invalid object size")
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(s.objectKey(key)),
		Body:          content,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put S3 object: %w", err)
	}
	return nil
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(key)),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get S3 object: %w", err)
	}
	return object.Body, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(s.objectKey(key)),
	})
	if err != nil && !isS3NotFound(err) {
		return fmt.Errorf("delete S3 object: %w", err)
	}
	return nil
}

func (s *S3) objectKey(key string) string {
	if s.prefix == "" {
		return key
	}
	return s.prefix + "/" + key
}

func deriveEndpoint(provider, region, accountID string) (string, error) {
	switch provider {
	case "aws", "minio", "custom":
		return "", nil
	case "r2":
		accountID = strings.TrimSpace(accountID)
		if accountID == "" {
			return "", fmt.Errorf("S3_ACCOUNT_ID (or S3_ENDPOINT) is required for Cloudflare R2")
		}
		return "https://" + accountID + ".r2.cloudflarestorage.com", nil
	case "aliyun":
		if strings.HasPrefix(region, "oss-") {
			return "https://s3." + region + ".aliyuncs.com", nil
		}
		return "https://s3.oss-" + region + ".aliyuncs.com", nil
	case "tencent":
		return "https://cos." + region + ".myqcloud.com", nil
	case "huawei":
		return "https://obs." + region + ".myhuaweicloud.com", nil
	case "qiniu":
		return "https://s3." + region + ".qiniucs.com", nil
	case "backblaze":
		return "https://s3." + region + ".backblazeb2.com", nil
	default:
		return "", fmt.Errorf("unsupported S3 provider %q", provider)
	}
}

func normalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(strings.TrimRight(endpoint, "/"))
	if endpoint == "" {
		return ""
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	return endpoint
}

func isS3NotFound(err error) bool {
	var apiError smithy.APIError
	if errors.As(err, &apiError) {
		switch apiError.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return true
		}
	}
	var responseError *smithyhttp.ResponseError
	return errors.As(err, &responseError) && responseError.HTTPStatusCode() == 404
}

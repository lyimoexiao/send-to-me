package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"sendtome/internal/config"
)

func baseS3Config(provider string) config.StorageConfig {
	return config.StorageConfig{
		Driver:            "s3",
		S3Provider:        provider,
		S3Bucket:          "test-bucket",
		S3AccessKeyID:     "test-key",
		S3SecretAccessKey: "test-secret",
		S3Prefix:          "send-to-me",
	}
}

func TestS3ProviderPresets(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		region       string
		accountID    string
		endpoint     string
		wantEndpoint string
		wantRegion   string
		wantPath     bool
	}{
		{
			name:         "aws",
			provider:     "aws",
			region:       "us-west-2",
			wantEndpoint: "",
			wantRegion:   "us-west-2",
			wantPath:     false,
		},
		{
			name:         "cloudflare r2",
			provider:     "r2",
			accountID:    "0123456789abcdef",
			wantEndpoint: "https://0123456789abcdef.r2.cloudflarestorage.com",
			wantRegion:   "auto",
			wantPath:     true,
		},
		{
			name:         "alibaba cloud oss",
			provider:     "aliyun",
			region:       "cn-hangzhou",
			wantEndpoint: "https://s3.oss-cn-hangzhou.aliyuncs.com",
			wantRegion:   "cn-hangzhou",
			wantPath:     false,
		},
		{
			name:         "tencent cos",
			provider:     "tencent",
			region:       "ap-guangzhou",
			wantEndpoint: "https://cos.ap-guangzhou.myqcloud.com",
			wantRegion:   "ap-guangzhou",
			wantPath:     true,
		},
		{
			name:         "huawei obs",
			provider:     "huawei",
			region:       "cn-north-4",
			wantEndpoint: "https://obs.cn-north-4.myhuaweicloud.com",
			wantRegion:   "cn-north-4",
			wantPath:     true,
		},
		{
			name:         "qiniu kodo",
			provider:     "qiniu",
			region:       "cn-east-1",
			wantEndpoint: "https://s3.cn-east-1.qiniucs.com",
			wantRegion:   "cn-east-1",
			wantPath:     true,
		},
		{
			name:         "backblaze b2",
			provider:     "backblaze",
			region:       "us-west-004",
			wantEndpoint: "https://s3.us-west-004.backblazeb2.com",
			wantRegion:   "us-west-004",
			wantPath:     true,
		},
		{
			name:         "minio",
			provider:     "minio",
			endpoint:     "http://127.0.0.1:9000",
			wantEndpoint: "http://127.0.0.1:9000",
			wantRegion:   "us-east-1",
			wantPath:     true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := baseS3Config(test.provider)
			cfg.S3Region = test.region
			cfg.S3AccountID = test.accountID
			cfg.S3Endpoint = test.endpoint
			backend, err := NewS3(cfg)
			if err != nil {
				t.Fatalf("NewS3: %v", err)
			}
			if backend.endpoint != test.wantEndpoint {
				t.Fatalf("endpoint = %q, want %q", backend.endpoint, test.wantEndpoint)
			}
			if backend.region != test.wantRegion {
				t.Fatalf("region = %q, want %q", backend.region, test.wantRegion)
			}
			if backend.pathStyle != test.wantPath {
				t.Fatalf("path style = %v, want %v", backend.pathStyle, test.wantPath)
			}
		})
	}
}

func TestS3ProviderValidation(t *testing.T) {
	t.Run("unknown provider", func(t *testing.T) {
		cfg := baseS3Config("nope")
		if _, err := NewS3(cfg); err == nil || !strings.Contains(err.Error(), "unsupported S3_PROVIDER") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing region", func(t *testing.T) {
		cfg := baseS3Config("aliyun")
		if _, err := NewS3(cfg); err == nil || !strings.Contains(err.Error(), "S3_REGION") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing r2 account", func(t *testing.T) {
		cfg := baseS3Config("r2")
		if _, err := NewS3(cfg); err == nil || !strings.Contains(err.Error(), "S3_ACCOUNT_ID") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing minio endpoint", func(t *testing.T) {
		cfg := baseS3Config("minio")
		if _, err := NewS3(cfg); err == nil || !strings.Contains(err.Error(), "S3_ENDPOINT") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("missing credentials", func(t *testing.T) {
		cfg := baseS3Config("tencent")
		cfg.S3Region = "ap-guangzhou"
		cfg.S3AccessKeyID = ""
		cfg.S3SecretAccessKey = ""
		if _, err := NewS3(cfg); err == nil || !strings.Contains(err.Error(), "S3_ACCESS_KEY_ID") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("path style override", func(t *testing.T) {
		cfg := baseS3Config("custom")
		cfg.S3Endpoint = "s3.example.com"
		force := false
		cfg.S3ForcePathStyle = &force
		backend, err := NewS3(cfg)
		if err != nil {
			t.Fatalf("NewS3: %v", err)
		}
		if backend.endpoint != "https://s3.example.com" {
			t.Fatalf("endpoint = %q", backend.endpoint)
		}
		if backend.pathStyle {
			t.Fatalf("path style override was ignored")
		}
	})
}

type fakeS3Server struct {
	mu          sync.Mutex
	objects     map[string][]byte
	lastType    string
	lastPath    string
	deleteCalls int
}

func (f *fakeS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/")
	switch r.Method {
	case http.MethodPut:
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		f.objects[path] = body
		f.lastType = r.Header.Get("Content-Type")
		f.lastPath = path
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		content, ok := f.objects[path]
		if !ok {
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<Error><Code>NoSuchKey</Code><Message>missing</Message></Error>`))
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	case http.MethodDelete:
		f.deleteCalls++
		delete(f.objects, path)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func TestS3PutGetDelete(t *testing.T) {
	fake := &fakeS3Server{objects: make(map[string][]byte)}
	httpServer := httptest.NewServer(fake)
	defer httpServer.Close()

	cfg := baseS3Config("custom")
	cfg.S3Endpoint = httpServer.URL
	cfg.S3Region = "us-east-1"
	cfg.S3Bucket = "test-bucket"
	forcePathStyle := true
	cfg.S3ForcePathStyle = &forcePathStyle

	backend, err := NewS3(cfg)
	if err != nil {
		t.Fatalf("NewS3: %v", err)
	}

	payload := []byte("s3 payload")
	if err := backend.Put(context.Background(), "shares/s1/f1", bytes.NewReader(payload), int64(len(payload)), "text/plain"); err != nil {
		t.Fatalf("put: %v", err)
	}
	fake.mu.Lock()
	lastPath := fake.lastPath
	lastType := fake.lastType
	fake.mu.Unlock()
	if lastPath != "test-bucket/send-to-me/shares/s1/f1" {
		t.Fatalf("path style path = %q", lastPath)
	}
	if lastType != "text/plain" {
		t.Fatalf("content type = %q", lastType)
	}

	body, err := backend.Get(context.Background(), "shares/s1/f1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload = %q, want %q", got, payload)
	}

	if err := backend.Delete(context.Background(), "shares/s1/f1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := backend.Get(context.Background(), "shares/s1/f1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete error = %v, want ErrNotFound", err)
	}
}

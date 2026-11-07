package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	"sendtome/internal/config"
)

// ErrNotFound is returned when an object is missing from the backend.
var ErrNotFound = errors.New("object not found")

// Storage is the payload backend contract. Downloads are proxied through the
// application in both implementations so download limits cannot be bypassed
// with a direct object-storage URL.
type Storage interface {
	Put(ctx context.Context, key string, content io.Reader, size int64, contentType string) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// New builds the configured storage backend.
func New(cfg config.StorageConfig) (Storage, error) {
	switch cfg.Driver {
	case "local", "":
		return NewLocal(cfg.LocalDir)
	case "s3":
		return NewS3(cfg)
	default:
		return nil, fmt.Errorf("unsupported storage driver %q", cfg.Driver)
	}
}

func validateKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.ContainsRune(key, '\\') {
		return fmt.Errorf("invalid storage key")
	}
	cleaned := path.Clean(key)
	if cleaned != key || cleaned == "." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return fmt.Errorf("invalid storage key")
	}
	for _, segment := range strings.Split(cleaned, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return fmt.Errorf("invalid storage key")
		}
	}
	return nil
}

type countingReader struct {
	reader io.Reader
	bytes  int64
}

func (r *countingReader) Read(buf []byte) (int, error) {
	n, err := r.reader.Read(buf)
	r.bytes += int64(n)
	return n, err
}

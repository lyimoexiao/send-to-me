package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Local stores objects below a single directory. Writes go to a temporary
// file and are renamed into place so a partial upload never becomes visible.
type Local struct {
	root string
}

func NewLocal(root string) (*Local, error) {
	if root == "" {
		return nil, fmt.Errorf("local storage directory is empty")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local storage directory: %w", err)
	}
	if err := os.MkdirAll(absolute, 0o750); err != nil {
		return nil, fmt.Errorf("create local storage directory: %w", err)
	}
	return &Local{root: absolute}, nil
}

func (l *Local) Put(ctx context.Context, key string, content io.Reader, size int64, _ string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("invalid object size")
	}

	target := l.path(key)
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("create object directory: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".upload-*")
	if err != nil {
		return fmt.Errorf("create temporary object: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}

	counter := &countingReader{reader: content}
	if _, err := io.Copy(tmp, counter); err != nil {
		cleanup()
		return fmt.Errorf("write object: %w", err)
	}
	if counter.bytes != size {
		cleanup()
		return fmt.Errorf("object size mismatch: expected %d bytes, got %d", size, counter.bytes)
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return fmt.Errorf("sync object: %w", err)
	}
	if err := tmp.Chmod(0o640); err != nil {
		cleanup()
		return fmt.Errorf("set object permissions: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close object: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("publish object: %w", err)
	}
	return nil
}

func (l *Local) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateKey(key); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(l.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open object: %w", err)
	}
	return file, nil
}

func (l *Local) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	err := os.Remove(l.path(key))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func (l *Local) path(key string) string {
	return filepath.Join(l.root, filepath.FromSlash(key))
}

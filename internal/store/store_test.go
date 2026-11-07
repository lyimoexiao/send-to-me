package store

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sendtome/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := Open(filepath.Join(t.TempDir(), "metadata.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestConsumeNeverExceedsLimit(t *testing.T) {
	st := newTestStore(t)
	share := &model.Share{
		ID:           "share-1",
		Code:         "LIMIT001",
		Kind:         model.KindFiles,
		MaxDownloads: 5,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	}
	file := model.File{
		ID:          "file-1",
		Name:        "hello.txt",
		StorageKey:  "shares/share-1/file-1",
		Size:        5,
		ContentType: "text/plain",
		CreatedAt:   time.Now().UTC(),
	}
	if _, err := st.CreateShare(context.Background(), share, []model.File{file}); err != nil {
		t.Fatalf("create share: %v", err)
	}

	const workers = 20
	var successes atomic.Int32
	var exhausted atomic.Int32
	var failures atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _, _, err := st.Consume(context.Background(), share.Code, model.KindFiles, "")
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrExhausted):
				exhausted.Add(1)
			default:
				failures.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := successes.Load(); got != 5 {
		t.Fatalf("successful claims = %d, want 5", got)
	}
	if got := exhausted.Load(); got != workers-5 {
		t.Fatalf("exhausted claims = %d, want %d", got, workers-5)
	}
	if got := failures.Load(); got != 0 {
		t.Fatalf("unexpected failures = %d", got)
	}

	loaded, _, err := st.GetByCode(context.Background(), share.Code)
	if err != nil {
		t.Fatalf("get share: %v", err)
	}
	if loaded.DownloadCount != 5 {
		t.Fatalf("download count = %d, want 5", loaded.DownloadCount)
	}
}

func TestConsumeRejectsExpiredShare(t *testing.T) {
	st := newTestStore(t)
	share := &model.Share{
		ID:           "share-expired",
		Code:         "EXPIRE01",
		Kind:         model.KindText,
		Text:         "expired",
		MaxDownloads: 10,
		CreatedAt:    time.Now().UTC().Add(-2 * time.Hour),
		ExpiresAt:    time.Now().UTC().Add(-time.Hour),
	}
	if _, err := st.CreateShare(context.Background(), share, nil); err != nil {
		t.Fatalf("create share: %v", err)
	}
	if _, _, _, err := st.Consume(context.Background(), share.Code, model.KindText, ""); !errors.Is(err, ErrExpired) {
		t.Fatalf("consume error = %v, want ErrExpired", err)
	}
	loaded, _, err := st.GetByCode(context.Background(), share.Code)
	if err != nil {
		t.Fatalf("get share: %v", err)
	}
	if loaded.DownloadCount != 0 {
		t.Fatalf("expired share download count = %d, want 0", loaded.DownloadCount)
	}
}

func TestConsumeCountsPerDevice(t *testing.T) {
	st := newTestStore(t)
	share := &model.Share{
		ID:           "share-device",
		Code:         "DEVICE01",
		Kind:         model.KindFiles,
		MaxDownloads: 1,
		CreatedAt:    time.Now().UTC(),
		ExpiresAt:    time.Now().UTC().Add(time.Hour),
	}
	if _, err := st.CreateShare(context.Background(), share, nil); err != nil {
		t.Fatalf("create share: %v", err)
	}
	ctx := context.Background()

	_, _, consumed, err := st.Consume(ctx, share.Code, model.KindFiles, "device-a")
	if err != nil || !consumed {
		t.Fatalf("first claim = consumed %v, err %v", consumed, err)
	}
	// Same terminal: allowed again, counter untouched.
	for i := 0; i < 3; i++ {
		_, _, consumed, err = st.Consume(ctx, share.Code, model.KindFiles, "device-a")
		if err != nil {
			t.Fatalf("repeat claim %d: %v", i, err)
		}
		if consumed {
			t.Fatalf("repeat claim %d consumed another download", i)
		}
	}
	loaded, _, err := st.GetByCode(ctx, share.Code)
	if err != nil {
		t.Fatalf("get share: %v", err)
	}
	if loaded.DownloadCount != 1 {
		t.Fatalf("download count = %d, want 1", loaded.DownloadCount)
	}

	// A different terminal hits the limit.
	if _, _, _, err := st.Consume(ctx, share.Code, model.KindFiles, "device-b"); !errors.Is(err, ErrExhausted) {
		t.Fatalf("second device error = %v, want ErrExhausted", err)
	}
	loaded, _, err = st.GetByCode(ctx, share.Code)
	if err != nil {
		t.Fatalf("get share: %v", err)
	}
	if loaded.DownloadCount != 1 {
		t.Fatalf("download count after rejected device = %d, want 1", loaded.DownloadCount)
	}

	consumed, err = st.ConsumedByDevice(ctx, share.ID, "device-a")
	if err != nil || !consumed {
		t.Fatalf("device-a claim = %v, err %v", consumed, err)
	}
	consumed, err = st.ConsumedByDevice(ctx, share.ID, "device-b")
	if err != nil || consumed {
		t.Fatalf("device-b claim = %v, err %v", consumed, err)
	}
}

func TestExpiredShareReleasesStorageKeys(t *testing.T) {
	st := newTestStore(t)
	share := &model.Share{
		ID:           "share-old",
		Code:         "OLD00001",
		Kind:         model.KindFiles,
		MaxDownloads: 1,
		CreatedAt:    time.Now().UTC().Add(-48 * time.Hour),
		ExpiresAt:    time.Now().UTC().Add(-24 * time.Hour),
	}
	files := []model.File{
		{ID: "f1", Name: "a.txt", StorageKey: "shares/share-old/f1", Size: 1, ContentType: "text/plain"},
		{ID: "f2", Name: "b.txt", StorageKey: "shares/share-old/f2", Size: 1, ContentType: "text/plain"},
	}
	if _, err := st.CreateShare(context.Background(), share, files); err != nil {
		t.Fatalf("create share: %v", err)
	}
	candidates, err := st.ExpiredBefore(context.Background(), time.Now().Unix())
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	keys, err := st.DeleteShare(context.Background(), share.ID)
	if err != nil {
		t.Fatalf("delete share: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("released keys = %d, want 2", len(keys))
	}
	if _, _, err := st.GetByCode(context.Background(), share.Code); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted share error = %v, want ErrNotFound", err)
	}
}

func TestCreateShareReusesIdenticalPayload(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	digest := strings.Repeat("ab", 32)
	now := time.Now().UTC()

	shareA := &model.Share{ID: "share-dedup-a", Code: "DEDUP0A", Kind: model.KindFiles, CreatedAt: now}
	filesA := []model.File{{
		ID: "file-a", Name: "one.txt", SHA256: digest,
		StorageKey: "objects/ab/original", Size: 12, ContentType: "text/plain", CreatedAt: now,
	}}
	if _, err := st.CreateShare(ctx, shareA, filesA); err != nil {
		t.Fatalf("create first share: %v", err)
	}

	key, err := st.FindObjectKey(ctx, digest)
	if err != nil {
		t.Fatalf("find object: %v", err)
	}
	if key != "objects/ab/original" {
		t.Fatalf("object key = %q", key)
	}

	shareB := &model.Share{ID: "share-dedup-b", Code: "DEDUP0B", Kind: model.KindFiles, CreatedAt: now}
	filesB := []model.File{{
		ID: "file-b", Name: "renamed.txt", SHA256: digest,
		StorageKey: "", Size: 12, ContentType: "text/plain", CreatedAt: now,
	}}
	orphaned, err := st.CreateShare(ctx, shareB, filesB)
	if err != nil {
		t.Fatalf("create reused share: %v", err)
	}
	if len(orphaned) != 0 {
		t.Fatalf("orphaned = %v, want none", orphaned)
	}
	_, storedFiles, err := st.GetByCode(ctx, shareB.Code)
	if err != nil {
		t.Fatalf("load second share: %v", err)
	}
	if storedFiles[0].StorageKey != "objects/ab/original" {
		t.Fatalf("reused key = %q", storedFiles[0].StorageKey)
	}
	if storedFiles[0].Name != "renamed.txt" {
		t.Fatalf("file metadata should stay per share, got %q", storedFiles[0].Name)
	}

	removed, err := st.DeleteShare(ctx, shareA.ID)
	if err != nil {
		t.Fatalf("delete first share: %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("removed keys = %v, want none while the second share lives", removed)
	}
	if _, err := st.FindObjectKey(ctx, digest); err != nil {
		t.Fatalf("object should survive: %v", err)
	}

	removed, err = st.DeleteShare(ctx, shareB.ID)
	if err != nil {
		t.Fatalf("delete second share: %v", err)
	}
	if len(removed) != 1 || removed[0] != "objects/ab/original" {
		t.Fatalf("removed keys = %v", removed)
	}
	if _, err := st.FindObjectKey(ctx, digest); !errors.Is(err, ErrNotFound) {
		t.Fatalf("object after final delete error = %v, want ErrNotFound", err)
	}
}

func TestCreateShareDiscardsConcurrentDuplicateUpload(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	digest := strings.Repeat("cd", 32)
	now := time.Now().UTC()

	shareA := &model.Share{ID: "share-race-a", Code: "RACE0A", Kind: model.KindFiles, CreatedAt: now}
	if _, err := st.CreateShare(ctx, shareA, []model.File{{
		ID: "file-race-a", Name: "a.bin", SHA256: digest,
		StorageKey: "objects/cd/first", Size: 9, ContentType: "application/octet-stream", CreatedAt: now,
	}}); err != nil {
		t.Fatalf("create first share: %v", err)
	}

	shareB := &model.Share{ID: "share-race-b", Code: "RACE0B", Kind: model.KindFiles, CreatedAt: now}
	orphaned, err := st.CreateShare(ctx, shareB, []model.File{{
		ID: "file-race-b", Name: "b.bin", SHA256: digest,
		StorageKey: "objects/cd/second", Size: 9, ContentType: "application/octet-stream", CreatedAt: now,
	}})
	if err != nil {
		t.Fatalf("create second share: %v", err)
	}
	if len(orphaned) != 1 || orphaned[0] != "objects/cd/second" {
		t.Fatalf("orphaned = %v, want the duplicate upload", orphaned)
	}
	_, storedFiles, err := st.GetByCode(ctx, shareB.Code)
	if err != nil {
		t.Fatalf("load second share: %v", err)
	}
	if storedFiles[0].StorageKey != "objects/cd/first" {
		t.Fatalf("file bound to %q, want the first payload", storedFiles[0].StorageKey)
	}
}

func TestCreateShareRejectsStaleReuse(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	share := &model.Share{ID: "share-stale", Code: "STALE0", Kind: model.KindFiles, CreatedAt: time.Now().UTC()}
	_, err := st.CreateShare(ctx, share, []model.File{{
		ID: "file-stale", Name: "gone.bin", SHA256: strings.Repeat("ef", 32),
		StorageKey: "", Size: 4, ContentType: "application/octet-stream", CreatedAt: time.Now().UTC(),
	}})
	if !errors.Is(err, ErrStaleObject) {
		t.Fatalf("error = %v, want ErrStaleObject", err)
	}
	if _, _, err := st.GetByCode(ctx, share.Code); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale share should not exist, got %v", err)
	}
}

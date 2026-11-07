package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"sendtome/internal/model"
)

var (
	ErrNotFound     = errors.New("share not found")
	ErrExpired      = errors.New("share expired")
	ErrExhausted    = errors.New("share exhausted")
	ErrCodeConflict = errors.New("pickup code already exists")
	ErrKindMismatch = errors.New("share has a different kind")
)

// Store persists share metadata in SQLite. Only one connection is opened so
// SQLite writes stay serialized; each transaction is short and payload bytes
// never travel through this database.
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("configure database (%s): %w", pragma, err)
		}
	}

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	// The metadata database contains sender IPs and text payloads; keep it
	// private to the service account.
	if err := os.Chmod(path, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("secure database permissions: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS shares (
			id              TEXT PRIMARY KEY,
			code            TEXT NOT NULL UNIQUE,
			kind            TEXT NOT NULL CHECK (kind IN ('files', 'text')),
			text_content    TEXT NOT NULL DEFAULT '',
			max_downloads   INTEGER NOT NULL DEFAULT 0,
			download_count  INTEGER NOT NULL DEFAULT 0,
			sender_ip       TEXT NOT NULL DEFAULT '',
			sender_agent    TEXT NOT NULL DEFAULT '',
			created_at      INTEGER NOT NULL,
			expires_at      INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_shares_expires_at ON shares(expires_at)`,
		`CREATE TABLE IF NOT EXISTS files (
			id           TEXT PRIMARY KEY,
			share_id     TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
			name         TEXT NOT NULL,
			storage_key  TEXT NOT NULL,
			size         INTEGER NOT NULL,
			content_type TEXT NOT NULL,
			created_at   INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_files_share_id ON files(share_id)`,
		`CREATE TABLE IF NOT EXISTS share_devices (
			share_id   TEXT NOT NULL REFERENCES shares(id) ON DELETE CASCADE,
			device_id  TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY (share_id, device_id)
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key        TEXT PRIMARY KEY,
			value      TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		// objects tracks one stored payload per distinct content hash and how
		// many share files point at it. hash is NULL for rows created before
		// content deduplication existed.
		`CREATE TABLE IF NOT EXISTS objects (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			hash        TEXT UNIQUE,
			storage_key TEXT NOT NULL UNIQUE,
			size        INTEGER NOT NULL,
			refs        INTEGER NOT NULL DEFAULT 0,
			created_at  INTEGER NOT NULL
		)`,
	}
	for _, statement := range statements {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}

	// The digest column arrived with content deduplication; older databases
	// need it appended before the lookup index can be built.
	hasDigest, err := s.hasColumn("files", "sha256")
	if err != nil {
		return err
	}
	if !hasDigest {
		if _, err := s.db.Exec(`ALTER TABLE files ADD COLUMN sha256 TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS idx_files_sha256 ON files(sha256)`,
		`INSERT INTO objects (hash, storage_key, size, refs, created_at)
			SELECT NULL, storage_key, MAX(size), COUNT(*), MIN(created_at)
			FROM files WHERE 1 = 1 GROUP BY storage_key
			ON CONFLICT(storage_key) DO NOTHING`,
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("migrate database: %w", err)
		}
	}
	return nil
}

func (s *Store) hasColumn(table, column string) (bool, error) {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &primaryKey); err != nil {
			return false, fmt.Errorf("inspect table %s: %w", table, err)
		}
		if name == column {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("inspect table %s: %w", table, err)
	}
	return false, nil
}

// LoadSettings returns the raw JSON of the admin settings row, or nil when the
// instance still uses environment defaults.
func (s *Store) LoadSettings(ctx context.Context) ([]byte, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'app'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load settings: %w", err)
	}
	return []byte(value), nil
}

// SaveSettings replaces the settings row atomically.
func (s *Store) SaveSettings(ctx context.Context, raw []byte) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES ('app', ?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		string(raw), time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

// FindObjectKey returns the stored payload currently holding the given content
// digest. Callers use it to skip uploading identical bytes again.
func (s *Store) FindObjectKey(ctx context.Context, hash string) (string, error) {
	if hash == "" {
		return "", ErrNotFound
	}
	var key string
	err := s.db.QueryRowContext(ctx, `SELECT storage_key FROM objects WHERE hash = ?`, hash).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find object: %w", err)
	}
	return key, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// ErrStaleObject means a reused payload disappeared before the share was
// committed; the caller should re-upload the spooled bytes and try again.
var ErrStaleObject = errors.New("deduplicated object is gone")

// CreateShare stores the share and binds each file to a shared payload. Files
// with the same SHA256 reuse one storage key, no matter which share uploaded
// it first. A file whose StorageKey is empty is a reused payload held only by
// the lookup; ErrStaleObject asks the caller to upload it again. The returned
// keys were fresh uploads that lost a concurrent race and can be deleted.
func (s *Store) CreateShare(ctx context.Context, share *model.Share, files []model.File) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create share: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx, `INSERT INTO shares (
		id, code, kind, text_content, max_downloads, download_count,
		sender_ip, sender_agent, created_at, expires_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		share.ID, share.Code, share.Kind, share.Text, share.MaxDownloads,
		share.DownloadCount, share.SenderIP, share.SenderAgent,
		toUnix(share.CreatedAt), toUnix(share.ExpiresAt),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return nil, ErrCodeConflict
		}
		return nil, fmt.Errorf("insert share: %w", err)
	}

	var orphaned []string
	createdAt := toUnix(share.CreatedAt)
	for i := range files {
		file := &files[i]
		if file.SHA256 != "" {
			var existing string
			lookupErr := tx.QueryRowContext(ctx, `SELECT storage_key FROM objects WHERE hash = ?`, file.SHA256).Scan(&existing)
			switch {
			case lookupErr == nil:
				if file.StorageKey != "" && file.StorageKey != existing {
					orphaned = append(orphaned, file.StorageKey)
				}
				file.StorageKey = existing
				if _, err := tx.ExecContext(ctx, `UPDATE objects SET refs = refs + 1 WHERE hash = ?`, file.SHA256); err != nil {
					return nil, fmt.Errorf("reference object: %w", err)
				}
			case errors.Is(lookupErr, sql.ErrNoRows):
				if file.StorageKey == "" {
					return nil, ErrStaleObject
				}
				if _, err := tx.ExecContext(ctx, `INSERT INTO objects (
					hash, storage_key, size, refs, created_at
				) VALUES (?, ?, ?, 1, ?)`,
					file.SHA256, file.StorageKey, file.Size, createdAt,
				); err != nil {
					return nil, fmt.Errorf("register object: %w", err)
				}
			default:
				return nil, fmt.Errorf("lookup object: %w", lookupErr)
			}
		} else {
			// Legacy callers without a digest still get a reference row so
			// cleanup can reclaim the payload once every share is gone.
			if _, err := tx.ExecContext(ctx, `INSERT INTO objects (
				hash, storage_key, size, refs, created_at
			) VALUES (NULL, ?, ?, 1, ?)
			ON CONFLICT(storage_key) DO UPDATE SET refs = refs + 1`,
				file.StorageKey, file.Size, createdAt,
			); err != nil {
				return nil, fmt.Errorf("register object: %w", err)
			}
		}

		if _, err := tx.ExecContext(ctx, `INSERT INTO files (
			id, share_id, name, sha256, storage_key, size, content_type, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			file.ID, share.ID, file.Name, file.SHA256, file.StorageKey, file.Size,
			file.ContentType, createdAt,
		); err != nil {
			return nil, fmt.Errorf("insert file: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit share: %w", err)
	}
	return orphaned, nil
}

// GetByCode loads a share without touching its download counter.
func (s *Store) GetByCode(ctx context.Context, code string) (*model.Share, []model.File, error) {
	share, err := scanShare(s.db.QueryRowContext(ctx, shareSelect+` WHERE code = ?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get share: %w", err)
	}

	files, err := s.filesForShare(ctx, share.ID)
	if err != nil {
		return nil, nil, err
	}
	return share, files, nil
}

// ConsumedByDevice reports whether this terminal already claimed the share.
func (s *Store) ConsumedByDevice(ctx context.Context, shareID, deviceID string) (bool, error) {
	if deviceID == "" {
		return false, nil
	}
	var marker int
	err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM share_devices WHERE share_id = ? AND device_id = ?`, shareID, deviceID,
	).Scan(&marker)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check device claim: %w", err)
	}
	return true, nil
}

// Consume atomically claims one download (or text view) for a terminal device.
// Repeated claims from the same device return the payload without touching the
// counter, even after the share is exhausted. The limit and expiry checks live
// in the UPDATE statement so concurrent claims can never exceed the limit.
func (s *Store) Consume(ctx context.Context, code, kind, deviceID string) (*model.Share, []model.File, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, false, fmt.Errorf("begin consume: %w", err)
	}
	defer tx.Rollback()

	now := time.Now()
	share, err := scanShare(tx.QueryRowContext(ctx, shareSelect+` WHERE code = ?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, false, ErrNotFound
	}
	if err != nil {
		return nil, nil, false, fmt.Errorf("inspect share: %w", err)
	}
	switch {
	case share.Kind != kind:
		return nil, nil, false, ErrKindMismatch
	case share.Expired(now):
		return nil, nil, false, ErrExpired
	}

	consumed := false
	if deviceID == "" {
		// Legacy clients without a device cookie always count as a new pickup.
		consumed, err = claimDownload(ctx, tx, share)
		if err != nil {
			return nil, nil, false, err
		}
	} else {
		inserted, insertErr := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO share_devices (share_id, device_id, created_at) VALUES (?, ?, ?)`,
			share.ID, deviceID, now.Unix(),
		)
		if insertErr != nil {
			return nil, nil, false, fmt.Errorf("record device claim: %w", insertErr)
		}
		rows, rowsErr := inserted.RowsAffected()
		if rowsErr != nil {
			return nil, nil, false, fmt.Errorf("read device claim: %w", rowsErr)
		}
		if rows == 1 {
			consumed, err = claimDownload(ctx, tx, share)
			if err != nil {
				return nil, nil, false, err
			}
		}
	}

	share, err = scanShare(tx.QueryRowContext(ctx, shareSelect+` WHERE code = ?`, code))
	if err != nil {
		return nil, nil, false, fmt.Errorf("reload share: %w", err)
	}

	var files []model.File
	if kind == model.KindFiles {
		files, err = filesForShareTx(ctx, tx, share.ID)
		if err != nil {
			return nil, nil, false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, false, fmt.Errorf("commit consume: %w", err)
	}
	return share, files, consumed, nil
}

// claimDownload increments the counter under the limit guard. It returns
// ErrExhausted when the share can no longer be claimed.
func claimDownload(ctx context.Context, tx *sql.Tx, share *model.Share) (bool, error) {
	result, err := tx.ExecContext(ctx, `UPDATE shares
		SET download_count = download_count + 1
		WHERE id = ?
		  AND (max_downloads = 0 OR download_count < max_downloads)`,
		share.ID,
	)
	if err != nil {
		return false, fmt.Errorf("claim download: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read claimed rows: %w", err)
	}
	if affected == 0 {
		return false, ErrExhausted
	}
	return true, nil
}

func (s *Store) filesForShare(ctx context.Context, shareID string) ([]model.File, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, storage_key, size, content_type, created_at
		FROM files WHERE share_id = ? ORDER BY rowid`, shareID)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()
	return scanFiles(rows)
}

func filesForShareTx(ctx context.Context, tx *sql.Tx, shareID string) ([]model.File, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, name, storage_key, size, content_type, created_at
		FROM files WHERE share_id = ? ORDER BY rowid`, shareID)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()
	return scanFiles(rows)
}

func scanFiles(rows *sql.Rows) ([]model.File, error) {
	var files []model.File
	for rows.Next() {
		var file model.File
		var createdAt int64
		if err := rows.Scan(&file.ID, &file.Name, &file.StorageKey, &file.Size, &file.ContentType, &createdAt); err != nil {
			return nil, fmt.Errorf("scan file: %w", err)
		}
		file.CreatedAt = fromUnix(createdAt)
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate files: %w", err)
	}
	return files, nil
}

const shareSelect = `SELECT id, code, kind, text_content, max_downloads, download_count,
	sender_ip, sender_agent, created_at, expires_at FROM shares`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanShare(row rowScanner) (*model.Share, error) {
	var share model.Share
	var createdAt, expiresAt int64
	err := row.Scan(
		&share.ID, &share.Code, &share.Kind, &share.Text, &share.MaxDownloads,
		&share.DownloadCount, &share.SenderIP, &share.SenderAgent, &createdAt, &expiresAt,
	)
	if err != nil {
		return nil, err
	}
	share.CreatedAt = fromUnix(createdAt)
	share.ExpiresAt = fromUnix(expiresAt)
	return &share, nil
}

// PurgeCandidate is a share that can be removed after its expiry grace period.
type PurgeCandidate struct {
	ShareID string
}

// ExpiredBefore lists expired shares, including shares with no files.
func (s *Store) ExpiredBefore(ctx context.Context, cutoffUnix int64) ([]PurgeCandidate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM shares
		WHERE expires_at > 0 AND expires_at <= ?
		ORDER BY id`, cutoffUnix)
	if err != nil {
		return nil, fmt.Errorf("list expired shares: %w", err)
	}
	defer rows.Close()

	var candidates []PurgeCandidate
	for rows.Next() {
		var shareID string
		if err := rows.Scan(&shareID); err != nil {
			return nil, fmt.Errorf("scan expired share: %w", err)
		}
		candidates = append(candidates, PurgeCandidate{ShareID: shareID})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expired shares: %w", err)
	}
	return candidates, nil
}

// DeleteShare removes a share and returns the payload keys that are no longer
// referenced by any remaining file. Callers delete those keys from storage
// after this transaction commits; keys still shared with another share stay.
func (s *Store) DeleteShare(ctx context.Context, shareID string) ([]string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin delete share: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `SELECT storage_key FROM files WHERE share_id = ? GROUP BY storage_key`, shareID)
	if err != nil {
		return nil, fmt.Errorf("list share payloads: %w", err)
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan share payload: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterate share payloads: %w", err)
	}
	rows.Close()

	if _, err := tx.ExecContext(ctx, `DELETE FROM shares WHERE id = ?`, shareID); err != nil {
		return nil, fmt.Errorf("delete share: %w", err)
	}

	var removed []string
	for _, key := range keys {
		var remaining int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM files WHERE storage_key = ?`, key).Scan(&remaining); err != nil {
			return nil, fmt.Errorf("count payload references: %w", err)
		}
		if remaining > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE objects SET refs = ? WHERE storage_key = ?`, remaining, key); err != nil {
				return nil, fmt.Errorf("update payload references: %w", err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM objects WHERE storage_key = ?`, key); err != nil {
			return nil, fmt.Errorf("release payload: %w", err)
		}
		removed = append(removed, key)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit delete share: %w", err)
	}
	return removed, nil
}

func toUnix(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.Unix()
}

func fromUnix(value int64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	return time.Unix(value, 0).UTC()
}

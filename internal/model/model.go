package model

import "time"

const (
	KindFiles = "files"
	KindText  = "text"
)

const (
	StatusActive    = "active"
	StatusExpired   = "expired"
	StatusExhausted = "exhausted"
)

// Share is the durable metadata of one drop. The payload itself lives in the
// storage backend (or, for text drops, in the Text field).
type Share struct {
	ID            string
	Code          string
	Kind          string
	Text          string
	MaxDownloads  int
	DownloadCount int
	SenderIP      string
	SenderAgent   string
	CreatedAt     time.Time
	ExpiresAt     time.Time
}

// File describes one payload file. StorageKey is never exposed through the
// public API.
type File struct {
	ID          string
	Name        string
	SHA256      string
	StorageKey  string
	Size        int64
	ContentType string
	CreatedAt   time.Time
}

func (s Share) Expired(now time.Time) bool {
	return !s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt)
}

func (s Share) Exhausted() bool {
	return s.MaxDownloads > 0 && s.DownloadCount >= s.MaxDownloads
}

func (s Share) Status(now time.Time) string {
	if s.Expired(now) {
		return StatusExpired
	}
	if s.Exhausted() {
		return StatusExhausted
	}
	return StatusActive
}

// Remaining returns nil when the share has no download limit.
func (s Share) Remaining() *int {
	if s.MaxDownloads <= 0 {
		return nil
	}
	remaining := s.MaxDownloads - s.DownloadCount
	if remaining < 0 {
		remaining = 0
	}
	return &remaining
}

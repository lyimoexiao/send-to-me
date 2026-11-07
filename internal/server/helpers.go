package server

import (
	"crypto/rand"
	"errors"
	"mime"
	"net"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"sendtome/internal/model"
	"sendtome/internal/store"
)

const pickupAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randomCode(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	for i := range buf {
		buf[i] = pickupAlphabet[int(buf[i])%len(pickupAlphabet)]
	}
	return string(buf), nil
}

func randomHex(byteCount int) (string, error) {
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	const digits = "0123456789abcdef"
	encoded := make([]byte, byteCount*2)
	for i, value := range buf {
		encoded[i*2] = digits[value>>4]
		encoded[i*2+1] = digits[value&0x0f]
	}
	return string(encoded), nil
}

func normalizeCode(raw string) string {
	var builder strings.Builder
	for _, char := range strings.ToUpper(strings.TrimSpace(raw)) {
		switch {
		case char >= 'A' && char <= 'Z', char >= '0' && char <= '9':
			builder.WriteRune(char)
		case char == '-', char == '_', unicode.IsSpace(char):
			continue
		default:
			return ""
		}
	}
	code := builder.String()
	if len(code) < 4 || len(code) > 32 {
		return ""
	}
	return code
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			for _, candidate := range strings.Split(forwarded, ",") {
				if ip := net.ParseIP(strings.TrimSpace(candidate)); ip != nil {
					return ip.String()
				}
			}
		}
		if ip := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (s *Server) publicBaseURL(r *http.Request) string {
	if s.cfg.PublicBaseURL != "" {
		return strings.TrimRight(s.cfg.PublicBaseURL, "/")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if s.cfg.TrustProxy {
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0]); forwarded == "http" || forwarded == "https" {
			scheme = forwarded
		}
	}
	host := r.Host
	if host == "" {
		host = "localhost"
	}
	return scheme + "://" + host
}

func (s *Server) shareLink(r *http.Request, code string) string {
	return s.publicBaseURL(r) + "/p/" + code
}

const deviceCookieName = "stm_device"

// deviceID returns the terminal identifier carried by the request, issuing a
// long-lived cookie when the client does not have one yet. Downloads are
// counted per device, so the cookie must survive pickups and reloads.
func (s *Server) deviceID(w http.ResponseWriter, r *http.Request) string {
	if cookie, err := r.Cookie(deviceCookieName); err == nil && isDeviceID(cookie.Value) {
		return cookie.Value
	}
	id, err := randomHex(16)
	if err != nil {
		s.logger.Error("generate device id failed", "error", err)
		return ""
	}
	http.SetCookie(w, &http.Cookie{
		Name:     deviceCookieName,
		Value:    id,
		Path:     "/",
		MaxAge:   365 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   requestIsHTTPS(r),
	})
	return id
}

func isDeviceID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func requestIsHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

type shareFileDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
}

type shareDTO struct {
	Code               string         `json:"code"`
	Kind               string         `json:"kind"`
	Files              []shareFileDTO `json:"files,omitempty"`
	TextLength         int            `json:"textLength"`
	CreatedAt          time.Time      `json:"createdAt"`
	ExpiresAt          *time.Time     `json:"expiresAt"`
	MaxDownloads       int            `json:"maxDownloads"`
	DownloadCount      int            `json:"downloadCount"`
	RemainingDownloads *int           `json:"remainingDownloads"`
	Status             string         `json:"status"`
	DeviceConsumed     bool           `json:"deviceConsumed"`
}

func newShareDTO(share *model.Share, files []model.File, now time.Time, deviceConsumed bool) shareDTO {
	status := share.Status(now)
	dto := shareDTO{
		Code:               share.Code,
		Kind:               share.Kind,
		TextLength:         utf8.RuneCountInString(share.Text),
		CreatedAt:          share.CreatedAt,
		MaxDownloads:       share.MaxDownloads,
		DownloadCount:      share.DownloadCount,
		RemainingDownloads: share.Remaining(),
		Status:             status,
		DeviceConsumed:     deviceConsumed,
	}
	if !share.ExpiresAt.IsZero() {
		expires := share.ExpiresAt
		dto.ExpiresAt = &expires
	}
	// A device that already claimed this drop keeps access even when the
	// global limit is reached; everyone else gets no payload metadata.
	if (status == model.StatusActive || deviceConsumed) && status != model.StatusExpired {
		if share.Kind == model.KindFiles {
			dto.Files = make([]shareFileDTO, 0, len(files))
			for _, file := range files {
				dto.Files = append(dto.Files, shareFileDTO{
					ID:          file.ID,
					Name:        file.Name,
					Size:        file.Size,
					ContentType: file.ContentType,
				})
			}
		}
	} else {
		// Do not disclose payload metadata once a drop can no longer be picked up.
		dto.TextLength = 0
	}
	return dto
}

type createdShareDTO struct {
	Code         string     `json:"code"`
	Link         string     `json:"link"`
	Kind         string     `json:"kind"`
	FileCount    int        `json:"fileCount"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	MaxDownloads int        `json:"maxDownloads"`
}

func (s *Server) writeConsumeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrExpired):
		writeAPIError(w, http.StatusGone, "expired", "这份分享已经过期")
	case errors.Is(err, store.ErrExhausted):
		writeAPIError(w, http.StatusGone, "exhausted", "这份分享的下载次数已用完")
	case errors.Is(err, store.ErrNotFound), errors.Is(err, store.ErrKindMismatch):
		writeAPIError(w, http.StatusNotFound, "not_found", "取件码不存在或已失效")
	default:
		s.logger.Error("consume share failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "取件失败，请稍后再试")
	}
}

func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.Map(func(char rune) rune {
		if unicode.IsControl(char) {
			return -1
		}
		return char
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." || name == "/" {
		name = "未命名文件"
	}
	runes := []rune(name)
	if len(runes) > 180 {
		name = string(runes[:180])
	}
	return name
}

func detectContentType(declared, filename string, head []byte) string {
	if mediaType, _, err := mime.ParseMediaType(declared); err == nil && mediaType != "" && mediaType != "application/octet-stream" {
		return mediaType
	}
	if byExtension := mime.TypeByExtension(filepath.Ext(filename)); byExtension != "" {
		if mediaType, _, err := mime.ParseMediaType(byExtension); err == nil && mediaType != "" {
			return mediaType
		}
	}
	if len(head) > 0 {
		sniffed := http.DetectContentType(head)
		if mediaType, _, err := mime.ParseMediaType(sniffed); err == nil && mediaType != "" {
			return mediaType
		}
	}
	return "application/octet-stream"
}

func attachmentHeader(filename string) string {
	header := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	if header == "" {
		return "attachment"
	}
	return header
}

func uniqueArchiveName(used map[string]int, filename string) string {
	extension := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, extension)
	candidate := filename
	for {
		key := strings.ToLower(candidate)
		if used[key] == 0 {
			used[key] = 1
			return candidate
		}
		used[key]++
		candidate = base + " (" + itoa(used[key]) + ")" + extension
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buf [20]byte
	position := len(buf)
	for value > 0 {
		position--
		buf[position] = byte('0' + value%10)
		value /= 10
	}
	return string(buf[position:])
}

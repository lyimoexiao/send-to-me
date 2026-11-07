package server

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"sendtome/internal/settings"
)

const (
	adminCookieName  = "stm_admin"
	adminSessionTTL  = 12 * time.Hour
	adminBodyLimit   = 8 << 10
	maxBackgroundMB  = 8
	loginMaxFailures = 8
	loginWindow      = 10 * time.Minute
)

var backgroundNamePattern = regexp.MustCompile(`^[0-9a-f]{32}\.(?:jpg|png|webp|gif)$`)

// loginGuard throttles password guesses per client address.
type loginGuard struct {
	mu      sync.Mutex
	entries map[string]*loginAttempt
}

type loginAttempt struct {
	failures int
	expires  time.Time
}

func newLoginGuard() *loginGuard {
	return &loginGuard{entries: make(map[string]*loginAttempt)}
}

func (g *loginGuard) blocked(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	entry, ok := g.entries[key]
	if !ok {
		return false
	}
	if time.Now().After(entry.expires) {
		delete(g.entries, key)
		return false
	}
	return entry.failures >= loginMaxFailures
}

func (g *loginGuard) fail(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := time.Now()
	entry, ok := g.entries[key]
	if !ok || now.After(entry.expires) {
		if len(g.entries) > 1024 {
			for candidate, existing := range g.entries {
				if now.After(existing.expires) {
					delete(g.entries, candidate)
				}
			}
		}
		g.entries[key] = &loginAttempt{failures: 1, expires: now.Add(loginWindow)}
		return
	}
	entry.failures++
}

func (g *loginGuard) reset(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.entries, key)
}

func (s *Server) settingsSnapshot() settings.Settings {
	s.settingsMu.RLock()
	defer s.settingsMu.RUnlock()
	snapshot := s.runtime
	snapshot.Backgrounds = make([]settings.Background, len(s.runtime.Backgrounds))
	copy(snapshot.Backgrounds, s.runtime.Backgrounds)
	return snapshot
}

// loadStoredSettings replaces the environment defaults with the persisted row.
func (s *Server) loadStoredSettings() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := s.store.LoadSettings(ctx)
	if err != nil {
		s.logger.Error("load stored settings failed", "error", err)
		return
	}
	if len(raw) == 0 {
		return
	}
	var stored settings.Settings
	if err := json.Unmarshal(raw, &stored); err != nil {
		s.logger.Error("decode stored settings failed", "error", err)
		return
	}
	stored.Normalize()
	s.settingsMu.Lock()
	s.runtime = stored
	s.settingsMu.Unlock()
}

func (s *Server) saveSettings(ctx context.Context, next settings.Settings) error {
	raw, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if err := s.store.SaveSettings(ctx, raw); err != nil {
		return err
	}
	s.settingsMu.Lock()
	s.runtime = next
	s.settingsMu.Unlock()
	return nil
}

func (s *Server) signAdminToken(expires int64) string {
	payload := strconv.FormatInt(expires, 10)
	mac := hmac.New(sha256.New, s.adminKey)
	mac.Write([]byte("admin:" + payload))
	return payload + "." + hex.EncodeToString(mac.Sum(nil))
}

func (s *Server) verifyAdminToken(token string) (int64, bool) {
	payload, signature, ok := strings.Cut(token, ".")
	if !ok {
		return 0, false
	}
	expires, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || expires <= 0 {
		return 0, false
	}
	expected, err := hex.DecodeString(signature)
	if err != nil {
		return 0, false
	}
	mac := hmac.New(sha256.New, s.adminKey)
	mac.Write([]byte("admin:" + payload))
	if !hmac.Equal(expected, mac.Sum(nil)) {
		return 0, false
	}
	return expires, true
}

func (s *Server) adminAuthenticated(r *http.Request) bool {
	if s.adminPassword == "" {
		return false
	}
	cookie, err := r.Cookie(adminCookieName)
	if err != nil {
		return false
	}
	expires, ok := s.verifyAdminToken(cookie.Value)
	if !ok || time.Now().Unix() >= expires {
		return false
	}
	s.revokedMu.Lock()
	defer s.revokedMu.Unlock()
	_, revoked := s.revoked[cookie.Value]
	return !revoked
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(parsed.Host, r.Host)
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.adminPassword == "" {
			writeAPIError(w, http.StatusServiceUnavailable, "admin_disabled", "管理后台未开启，请先设置 ADMIN_PASSWORD")
			return
		}
		if r.Method != http.MethodGet && !sameOrigin(r) {
			writeAPIError(w, http.StatusForbidden, "forbidden", "请求来源无效")
			return
		}
		if !s.adminAuthenticated(r) {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "请先登录管理后台")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleAdminSession(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       s.adminPassword != "",
		"authenticated": s.adminAuthenticated(r),
	})
}

func (s *Server) handleAdminLogin(w http.ResponseWriter, r *http.Request) {
	if s.adminPassword == "" {
		writeAPIError(w, http.StatusServiceUnavailable, "admin_disabled", "管理后台未开启，请先设置 ADMIN_PASSWORD")
		return
	}
	if !sameOrigin(r) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "请求来源无效")
		return
	}
	client := clientIP(r, s.cfg.TrustProxy)
	if s.loginGuard.blocked(client) {
		writeAPIError(w, http.StatusTooManyRequests, "too_many_attempts", "尝试次数过多，请稍后再试")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, adminBodyLimit)
	var payload struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "请求内容无效")
		return
	}

	given := sha256.Sum256([]byte(payload.Password))
	expected := sha256.Sum256([]byte(s.adminPassword))
	if subtle.ConstantTimeCompare(given[:], expected[:]) != 1 {
		s.loginGuard.fail(client)
		writeAPIError(w, http.StatusUnauthorized, "invalid_password", "密码不正确")
		return
	}

	s.loginGuard.reset(client)
	expires := time.Now().Add(adminSessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    s.signAdminToken(expires.Unix()),
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   s.secureAdminCookie(r),
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (s *Server) handleAdminLogout(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "请求来源无效")
		return
	}
	if cookie, err := r.Cookie(adminCookieName); err == nil {
		if expires, ok := s.verifyAdminToken(cookie.Value); ok {
			s.revokeAdminToken(cookie.Value, expires)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secureAdminCookie(r),
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": false})
}

func (s *Server) secureAdminCookie(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return s.cfg.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) revokeAdminToken(token string, expires int64) {
	now := time.Now()
	s.revokedMu.Lock()
	defer s.revokedMu.Unlock()
	if len(s.revoked) > 1024 {
		for value, expiry := range s.revoked {
			if !now.Before(expiry) {
				delete(s.revoked, value)
			}
		}
	}
	s.revoked[token] = time.Unix(expires, 0)
}

func (s *Server) handleAdminGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settingsSnapshot())
}

func (s *Server) handleAdminPutSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var next settings.Settings
	if err := json.NewDecoder(r.Body).Decode(&next); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "配置内容无效")
		return
	}
	if err := next.Validate(); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_settings", err.Error())
		return
	}
	previous := s.settingsSnapshot()
	if err := s.saveSettings(r.Context(), next); err != nil {
		s.logger.Error("save settings failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "保存配置失败，请稍后再试")
		return
	}
	s.removeOrphanBackgroundFiles(previous, next)
	writeJSON(w, http.StatusOK, s.settingsSnapshot())
}

func (s *Server) handleAdminAddBackground(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, adminBodyLimit)
	var payload struct {
		Kind   string `json:"kind"`
		URL    string `json:"url"`
		Label  string `json:"label"`
		Credit string `json:"credit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "请求内容无效")
		return
	}
	payload.Kind = strings.TrimSpace(payload.Kind)
	switch payload.Kind {
	case "bing", "picsum", "url":
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid_background", "背景图类型无效")
		return
	}
	if payload.Kind == "url" && strings.TrimSpace(payload.URL) == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_background", "请填写背景图链接")
		return
	}

	id, err := randomHex(6)
	if err != nil {
		s.logger.Error("generate background id failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "添加背景失败，请稍后再试")
		return
	}

	next := s.settingsSnapshot()
	next.Backgrounds = append(next.Backgrounds, settings.Background{
		ID:     id,
		Kind:   payload.Kind,
		URL:    strings.TrimSpace(payload.URL),
		Label:  payload.Label,
		Credit: payload.Credit,
	})
	if err := next.Validate(); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_background", err.Error())
		return
	}
	if err := s.saveSettings(r.Context(), next); err != nil {
		s.logger.Error("save background failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "添加背景失败，请稍后再试")
		return
	}
	writeJSON(w, http.StatusOK, s.settingsSnapshot())
}

func (s *Server) handleAdminUploadBackground(w http.ResponseWriter, r *http.Request) {
	maxBody := int64(maxBackgroundMB<<20) + 1<<20
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "too_large", "背景图不能超过 8 MB")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "请使用 multipart/form-data 上传背景图")
		return
	}
	defer r.MultipartForm.RemoveAll()

	file, header, err := r.FormFile("file")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "请选择要上传的图片")
		return
	}
	defer file.Close()

	head := make([]byte, 16)
	n, readErr := io.ReadFull(file, head)
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) && !errors.Is(readErr, io.EOF) {
		writeAPIError(w, http.StatusBadRequest, "invalid_background", "图片读取失败")
		return
	}
	head = head[:n]
	extension, ok := backgroundImageExt(head)
	if !ok {
		writeAPIError(w, http.StatusUnsupportedMediaType, "unsupported_media", "仅支持 JPG、PNG、WebP、GIF 图片")
		return
	}

	temp, err := os.CreateTemp(s.backgroundsDir, ".upload-*")
	if err != nil {
		s.logger.Error("create background temp file failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "保存背景图失败，请稍后再试")
		return
	}
	tempPath := temp.Name()
	written, copyErr := io.Copy(temp, io.LimitReader(io.MultiReader(bytes.NewReader(head), file), (maxBackgroundMB<<20)+1))
	closeErr := temp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tempPath)
		s.logger.Error("write background file failed", "error", errors.Join(copyErr, closeErr))
		writeAPIError(w, http.StatusInternalServerError, "server_error", "保存背景图失败，请稍后再试")
		return
	}
	if written > maxBackgroundMB<<20 {
		_ = os.Remove(tempPath)
		writeAPIError(w, http.StatusRequestEntityTooLarge, "too_large", "背景图不能超过 8 MB")
		return
	}

	name, err := randomHex(16)
	if err != nil {
		_ = os.Remove(tempPath)
		s.logger.Error("generate background file name failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "保存背景图失败，请稍后再试")
		return
	}
	finalName := name + extension
	if err := os.Rename(tempPath, filepath.Join(s.backgroundsDir, finalName)); err != nil {
		_ = os.Remove(tempPath)
		s.logger.Error("store background file failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "保存背景图失败，请稍后再试")
		return
	}

	label := strings.TrimSpace(r.FormValue("label"))
	if label == "" {
		label = strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	}
	if label == "" {
		label = "本地上传"
	}

	id, err := randomHex(6)
	if err == nil {
		next := s.settingsSnapshot()
		next.Backgrounds = append(next.Backgrounds, settings.Background{
			ID:    id,
			Kind:  "upload",
			URL:   "/media/backgrounds/" + finalName,
			Label: label,
		})
		err = next.Validate()
		if err == nil {
			err = s.saveSettings(r.Context(), next)
		}
	}
	if err != nil {
		_ = os.Remove(filepath.Join(s.backgroundsDir, finalName))
		s.logger.Error("register uploaded background failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "保存背景图失败，请稍后再试")
		return
	}
	writeJSON(w, http.StatusOK, s.settingsSnapshot())
}

func (s *Server) handleAdminDeleteBackground(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	next := s.settingsSnapshot()
	var removed settings.Background
	found := false
	kept := next.Backgrounds[:0]
	for _, bg := range next.Backgrounds {
		if bg.ID == id {
			removed = bg
			found = true
			continue
		}
		kept = append(kept, bg)
	}
	if !found {
		writeAPIError(w, http.StatusNotFound, "not_found", "背景图不存在")
		return
	}
	next.Backgrounds = kept
	if err := s.saveSettings(r.Context(), next); err != nil {
		s.logger.Error("delete background failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "删除背景失败，请稍后再试")
		return
	}
	s.removeBackgroundFile(removed)
	writeJSON(w, http.StatusOK, s.settingsSnapshot())
}

func (s *Server) handleBackgroundMedia(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !backgroundNamePattern.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(filepath.Join(s.backgroundsDir, name))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	http.ServeContent(w, r, name, info.ModTime(), file)
}

// removeOrphanBackgroundFiles deletes uploaded files dropped by a settings save.
func (s *Server) removeOrphanBackgroundFiles(previous, next settings.Settings) {
	kept := make(map[string]bool, len(next.Backgrounds))
	for _, bg := range next.Backgrounds {
		if bg.Kind == "upload" {
			kept[bg.URL] = true
		}
	}
	for _, bg := range previous.Backgrounds {
		if bg.Kind == "upload" && !kept[bg.URL] {
			s.removeBackgroundFile(bg)
		}
	}
}

func (s *Server) removeBackgroundFile(bg settings.Background) {
	if bg.Kind != "upload" {
		return
	}
	name := strings.TrimPrefix(bg.URL, "/media/backgrounds/")
	if !backgroundNamePattern.MatchString(name) {
		return
	}
	if err := os.Remove(filepath.Join(s.backgroundsDir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		s.logger.Warn("remove background file failed", "name", name, "error", err)
	}
}

func backgroundImageExt(head []byte) (string, bool) {
	switch {
	case len(head) >= 3 && head[0] == 0xFF && head[1] == 0xD8 && head[2] == 0xFF:
		return ".jpg", true
	case len(head) >= 8 && string(head[:8]) == "\x89PNG\r\n\x1a\n":
		return ".png", true
	case len(head) >= 6 && (string(head[:6]) == "GIF87a" || string(head[:6]) == "GIF89a"):
		return ".gif", true
	case len(head) >= 12 && string(head[:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return ".webp", true
	}
	return "", false
}

package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sendtome/internal/config"
	"sendtome/internal/model"
	"sendtome/internal/storage"
	"sendtome/internal/store"
)

type testFile struct {
	name        string
	contentType string
	body        []byte
}

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	return newTestServerWith(t, nil)
}

func newTestServerWith(t *testing.T, mutate func(*config.Config)) (*Server, *store.Store) {
	t.Helper()
	root := t.TempDir()
	cfg := config.Config{
		Addr:           ":0",
		DataDir:        root,
		DBPath:         filepath.Join(root, "metadata.db"),
		MaxUploadBytes: 10 * 1024 * 1024,
		MaxFiles:       5,
		MaxTextBytes:   1024,
		DefaultExpiry:  7 * 24 * time.Hour,
		MaxExpiry:      30 * 24 * time.Hour,
		CleanupGrace:   24 * time.Hour,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	payloads, err := storage.NewLocal(filepath.Join(root, "files"))
	if err != nil {
		t.Fatalf("open local storage: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(cfg, st, payloads, logger, nil), st
}

func multipartRequest(t *testing.T, fields map[string]string, files []testFile) (*bytes.Buffer, string) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	for _, file := range files {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="files"; filename="%s"`, escapeQuotes(file.name)))
		contentType := file.contentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		header.Set("Content-Type", contentType)
		part, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := part.Write(file.body); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart: %v", err)
	}
	return body, writer.FormDataContentType()
}

func escapeQuotes(value string) string {
	return strings.NewReplacer("\\", "\\\\", `"`, "\\\"").Replace(value)
}

func createShareRequest(t *testing.T, srv *Server, fields map[string]string, files []testFile) *httptest.ResponseRecorder {
	t.Helper()
	body, contentType := multipartRequest(t, fields, files)
	request := httptest.NewRequest(http.MethodPost, "/api/shares", body)
	request.Header.Set("Content-Type", contentType)
	request.RemoteAddr = "203.0.113.9:41234"
	request.Header.Set("User-Agent", "send-to-me-test/1.0")
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	return recorder
}

func decodeJSON[T any](t *testing.T, recorder *httptest.ResponseRecorder) T {
	t.Helper()
	var value T
	if err := json.NewDecoder(recorder.Body).Decode(&value); err != nil {
		t.Fatalf("decode response %q: %v", recorder.Body.String(), err)
	}
	return value
}

func TestFileShareLifecycle(t *testing.T) {
	srv, st := newTestServer(t)
	createdRecorder := createShareRequest(t, srv, map[string]string{
		"kind":          "files",
		"expires_in":    "3600",
		"max_downloads": "2",
	}, []testFile{{name: "hello.txt", contentType: "text/plain; charset=utf-8", body: []byte("hello world")}})
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	created := decodeJSON[createdShareDTO](t, createdRecorder)
	if len(created.Code) != 6 {
		t.Fatalf("pickup code = %q, want 6 characters", created.Code)
	}
	if !strings.Contains(created.Link, "/p/"+created.Code) {
		t.Fatalf("link = %q, want pickup path", created.Link)
	}
	if created.FileCount != 1 || created.Kind != model.KindFiles {
		t.Fatalf("created share = %+v", created)
	}

	stored, storedFiles, err := st.GetByCode(context.Background(), created.Code)
	if err != nil {
		t.Fatalf("load stored share: %v", err)
	}
	if stored.SenderIP != "203.0.113.9" {
		t.Fatalf("sender IP = %q, want 203.0.113.9", stored.SenderIP)
	}
	if stored.SenderAgent != "send-to-me-test/1.0" {
		t.Fatalf("sender agent = %q", stored.SenderAgent)
	}
	if len(storedFiles) != 1 || storedFiles[0].Name != "hello.txt" {
		t.Fatalf("stored files = %+v", storedFiles)
	}

	metaRecorder := getRequest(t, srv, "/api/shares/"+created.Code, http.MethodGet)
	if metaRecorder.Code != http.StatusOK {
		t.Fatalf("metadata status = %d", metaRecorder.Code)
	}
	meta := decodeJSON[shareDTO](t, metaRecorder)
	if meta.Status != model.StatusActive || len(meta.Files) != 1 || meta.DownloadCount != 0 {
		t.Fatalf("metadata = %+v", meta)
	}

	headRecorder := getRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodHead)
	if headRecorder.Code != http.StatusOK {
		t.Fatalf("preflight status = %d", headRecorder.Code)
	}
	if headRecorder.Header().Get("X-Share-Remaining") != "2" {
		t.Fatalf("preflight remaining = %q, want 2", headRecorder.Header().Get("X-Share-Remaining"))
	}
	afterHead, _, err := st.GetByCode(context.Background(), created.Code)
	if err != nil {
		t.Fatalf("reload after HEAD: %v", err)
	}
	if afterHead.DownloadCount != 0 {
		t.Fatalf("HEAD consumed a download: count = %d", afterHead.DownloadCount)
	}

	first := getRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodGet)
	if first.Code != http.StatusOK {
		t.Fatalf("first download status = %d, body = %s", first.Code, first.Body.String())
	}
	if first.Body.String() != "hello world" {
		t.Fatalf("download body = %q", first.Body.String())
	}
	if !strings.Contains(first.Header().Get("Content-Disposition"), "hello.txt") {
		t.Fatalf("content disposition = %q", first.Header().Get("Content-Disposition"))
	}
	if first.Header().Get("X-Share-Remaining") != "1" {
		t.Fatalf("remaining after first = %q", first.Header().Get("X-Share-Remaining"))
	}

	second := getRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodGet)
	if second.Code != http.StatusOK {
		t.Fatalf("second download status = %d", second.Code)
	}

	third := getRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodGet)
	if third.Code != http.StatusGone {
		t.Fatalf("third download status = %d, want 410", third.Code)
	}
	errorBody := decodeJSON[errorEnvelope](t, third)
	if errorBody.Error.Code != "exhausted" {
		t.Fatalf("third download error = %+v", errorBody.Error)
	}
	headAfter := getRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodHead)
	if headAfter.Code != http.StatusGone {
		t.Fatalf("exhausted preflight status = %d, want 410", headAfter.Code)
	}

	afterMetaRecorder := getRequest(t, srv, "/api/shares/"+created.Code, http.MethodGet)
	afterMeta := decodeJSON[shareDTO](t, afterMetaRecorder)
	if afterMeta.Status != model.StatusExhausted {
		t.Fatalf("status after limit = %q, want exhausted", afterMeta.Status)
	}
	if len(afterMeta.Files) != 0 {
		t.Fatalf("exhausted share leaked files: %+v", afterMeta.Files)
	}
}

func TestTextShareLifecycle(t *testing.T) {
	srv, st := newTestServer(t)
	recorder := createShareRequest(t, srv, map[string]string{
		"kind":          "text",
		"text":          "这是一段秘密。",
		"expires_in":    "3600",
		"max_downloads": "1",
	}, nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	created := decodeJSON[createdShareDTO](t, recorder)
	if created.Kind != model.KindText {
		t.Fatalf("kind = %q, want text", created.Kind)
	}

	metaRecorder := getRequest(t, srv, "/api/shares/"+created.Code, http.MethodGet)
	meta := decodeJSON[shareDTO](t, metaRecorder)
	if meta.Kind != model.KindText || meta.TextLength != 7 || meta.Status != model.StatusActive {
		t.Fatalf("text metadata = %+v", meta)
	}

	reveal := getRequest(t, srv, "/api/shares/"+created.Code+"/text", http.MethodGet)
	if reveal.Code != http.StatusOK {
		t.Fatalf("reveal status = %d, body = %s", reveal.Code, reveal.Body.String())
	}
	var revealed struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(reveal.Body).Decode(&revealed); err != nil {
		t.Fatalf("decode revealed text: %v", err)
	}
	if revealed.Text != "这是一段秘密。" {
		t.Fatalf("revealed text = %q", revealed.Text)
	}

	second := getRequest(t, srv, "/api/shares/"+created.Code+"/text", http.MethodGet)
	if second.Code != http.StatusGone {
		t.Fatalf("second reveal status = %d, want 410", second.Code)
	}

	stored, _, err := st.GetByCode(context.Background(), created.Code)
	if err != nil {
		t.Fatalf("load stored share: %v", err)
	}
	if stored.DownloadCount != 1 {
		t.Fatalf("text download count = %d, want 1", stored.DownloadCount)
	}
}

func TestMultipleFilesDownloadAsZip(t *testing.T) {
	srv, _ := newTestServer(t)
	recorder := createShareRequest(t, srv, map[string]string{
		"kind":          "files",
		"expires_in":    "3600",
		"max_downloads": "3",
	}, []testFile{
		{name: "a.txt", contentType: "text/plain", body: []byte("alpha")},
		{name: "b.txt", contentType: "text/plain", body: []byte("beta")},
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	created := decodeJSON[createdShareDTO](t, recorder)

	download := getRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodGet)
	if download.Code != http.StatusOK {
		t.Fatalf("download status = %d, body = %s", download.Code, download.Body.String())
	}
	if download.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("content type = %q, want application/zip", download.Header().Get("Content-Type"))
	}
	archive, err := zip.NewReader(bytes.NewReader(download.Body.Bytes()), int64(download.Body.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	if len(archive.File) != 2 {
		t.Fatalf("zip entries = %d, want 2", len(archive.File))
	}
	got := make(map[string]string)
	for _, entry := range archive.File {
		reader, err := entry.Open()
		if err != nil {
			t.Fatalf("open zip entry: %v", err)
		}
		content, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatalf("read zip entry: %v", err)
		}
		got[entry.Name] = string(content)
	}
	if got["a.txt"] != "alpha" || got["b.txt"] != "beta" {
		t.Fatalf("zip contents = %+v", got)
	}
}

func TestExpiredShareCannotBePickedUp(t *testing.T) {
	srv, st := newTestServer(t)
	share := &model.Share{
		ID:           "expired-share",
		Code:         "EXPIRE01",
		Kind:         model.KindFiles,
		MaxDownloads: 5,
		SenderIP:     "198.51.100.8",
		CreatedAt:    time.Now().UTC().Add(-2 * time.Hour),
		ExpiresAt:    time.Now().UTC().Add(-time.Hour),
	}
	if _, err := st.CreateShare(context.Background(), share, nil); err != nil {
		t.Fatalf("create expired share: %v", err)
	}

	metaRecorder := getRequest(t, srv, "/api/shares/"+share.Code, http.MethodGet)
	if metaRecorder.Code != http.StatusOK {
		t.Fatalf("metadata status = %d", metaRecorder.Code)
	}
	meta := decodeJSON[shareDTO](t, metaRecorder)
	if meta.Status != model.StatusExpired || len(meta.Files) != 0 {
		t.Fatalf("expired metadata = %+v", meta)
	}

	head := getRequest(t, srv, "/api/shares/"+share.Code+"/download", http.MethodHead)
	if head.Code != http.StatusGone {
		t.Fatalf("expired HEAD status = %d, want 410", head.Code)
	}
	download := getRequest(t, srv, "/api/shares/"+share.Code+"/download", http.MethodGet)
	if download.Code != http.StatusGone {
		t.Fatalf("expired download status = %d, want 410", download.Code)
	}
	errorBody := decodeJSON[errorEnvelope](t, download)
	if errorBody.Error.Code != "expired" {
		t.Fatalf("expired error = %+v", errorBody.Error)
	}
}

func TestUploadSizeLimit(t *testing.T) {
	srv, _ := newTestServerWith(t, func(cfg *config.Config) {
		cfg.MaxUploadBytes = 4
	})
	recorder := createShareRequest(t, srv, map[string]string{
		"kind":          "files",
		"expires_in":    "3600",
		"max_downloads": "1",
	}, []testFile{{name: "big.bin", contentType: "application/octet-stream", body: []byte("12345")}})
	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413, body = %s", recorder.Code, recorder.Body.String())
	}
}

func getRequest(t *testing.T, srv *Server, path, method string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	return recorder
}

func deviceRequest(t *testing.T, srv *Server, path, method, deviceID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	if deviceID != "" {
		request.AddCookie(&http.Cookie{Name: deviceCookieName, Value: deviceID})
	}
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	return recorder
}

// Downloads are counted per terminal: the same device can download repeatedly
// while each new device spends one of the limited pickups.
func TestDownloadCountsPerDevice(t *testing.T) {
	srv, _ := newTestServer(t)
	createdRecorder := createShareRequest(t, srv, map[string]string{
		"kind":          "files",
		"expires_in":    "3600",
		"max_downloads": "1",
	}, []testFile{{name: "note.txt", contentType: "text/plain; charset=utf-8", body: []byte("per device")}})
	if createdRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createdRecorder.Code, createdRecorder.Body.String())
	}
	created := decodeJSON[createdShareDTO](t, createdRecorder)

	// The first metadata request issues the device cookie.
	anonymous := getRequest(t, srv, "/api/shares/"+created.Code, http.MethodGet)
	if anonymous.Code != http.StatusOK {
		t.Fatalf("metadata status = %d", anonymous.Code)
	}
	cookie := anonymous.Result().Cookies()
	if len(cookie) != 1 || cookie[0].Name != deviceCookieName || cookie[0].Value == "" {
		t.Fatalf("device cookie = %+v", cookie)
	}
	if !cookie[0].HttpOnly || cookie[0].Path != "/" {
		t.Fatalf("device cookie attributes = %+v", cookie[0])
	}
	device := cookie[0].Value

	first := deviceRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodGet, device)
	if first.Code != http.StatusOK || first.Body.String() != "per device" {
		t.Fatalf("first download = %d, body %q", first.Code, first.Body.String())
	}
	if first.Header().Get("X-Share-Remaining") != "0" {
		t.Fatalf("remaining after first = %q", first.Header().Get("X-Share-Remaining"))
	}

	for attempt := 0; attempt < 2; attempt++ {
		repeat := deviceRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodGet, device)
		if repeat.Code != http.StatusOK {
			t.Fatalf("repeat download %d status = %d, body = %s", attempt, repeat.Code, repeat.Body.String())
		}
		if repeat.Body.String() != "per device" {
			t.Fatalf("repeat download %d body = %q", attempt, repeat.Body.String())
		}
		if repeat.Header().Get("X-Share-Remaining") != "0" {
			t.Fatalf("repeat download %d remaining = %q", attempt, repeat.Header().Get("X-Share-Remaining"))
		}
	}

	head := deviceRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodHead, device)
	if head.Code != http.StatusOK {
		t.Fatalf("known device preflight = %d, want 200", head.Code)
	}

	metaRecorder := deviceRequest(t, srv, "/api/shares/"+created.Code, http.MethodGet, device)
	meta := decodeJSON[shareDTO](t, metaRecorder)
	if !meta.DeviceConsumed || meta.DownloadCount != 1 || meta.Status != model.StatusExhausted {
		t.Fatalf("known device metadata = %+v", meta)
	}
	if len(meta.Files) != 1 {
		t.Fatalf("known device lost file list: %+v", meta.Files)
	}

	otherHead := deviceRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodHead, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if otherHead.Code != http.StatusGone {
		t.Fatalf("new device preflight = %d, want 410", otherHead.Code)
	}
	other := deviceRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodGet, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	if other.Code != http.StatusGone {
		t.Fatalf("new device download = %d, want 410", other.Code)
	}
	otherMetaRecorder := deviceRequest(t, srv, "/api/shares/"+created.Code, http.MethodGet, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	otherMeta := decodeJSON[shareDTO](t, otherMetaRecorder)
	if otherMeta.DeviceConsumed || len(otherMeta.Files) != 0 {
		t.Fatalf("new device metadata leaked payload: %+v", otherMeta)
	}
}

func TestTextRevealCountsPerDevice(t *testing.T) {
	srv, _ := newTestServer(t)
	recorder := createShareRequest(t, srv, map[string]string{
		"kind":          "text",
		"text":          "只算一次",
		"expires_in":    "3600",
		"max_downloads": "1",
	}, nil)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	created := decodeJSON[createdShareDTO](t, recorder)

	type revealDTO struct {
		Text     string `json:"text"`
		Consumed bool   `json:"consumed"`
		Count    int    `json:"downloadCount"`
	}
	first := deviceRequest(t, srv, "/api/shares/"+created.Code+"/text", http.MethodGet, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	revealed := decodeJSON[revealDTO](t, first)
	if revealed.Text != "只算一次" || !revealed.Consumed || revealed.Count != 1 {
		t.Fatalf("first reveal = %+v", revealed)
	}

	again := deviceRequest(t, srv, "/api/shares/"+created.Code+"/text", http.MethodGet, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	repeat := decodeJSON[revealDTO](t, again)
	if again.Code != http.StatusOK || repeat.Text != "只算一次" || repeat.Consumed || repeat.Count != 1 {
		t.Fatalf("repeat reveal status = %d, payload = %+v", again.Code, repeat)
	}

	stranger := deviceRequest(t, srv, "/api/shares/"+created.Code+"/text", http.MethodGet, "cccccccccccccccccccccccccccccccc")
	if stranger.Code != http.StatusGone {
		t.Fatalf("new device reveal = %d, want 410", stranger.Code)
	}
}

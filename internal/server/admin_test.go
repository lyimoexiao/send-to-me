package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"

	"sendtome/internal/config"
	"sendtome/internal/settings"
)

func adminRequest(t *testing.T, srv *Server, method, path string, payload any, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var body *bytes.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal admin payload: %v", err)
		}
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}
	request := httptest.NewRequest(method, path, body)
	request.RemoteAddr = "203.0.113.20:51234"
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	return recorder
}

func loginAdmin(t *testing.T, srv *Server, password string) *http.Cookie {
	t.Helper()
	recorder := adminRequest(t, srv, http.MethodPost, "/api/admin/login", map[string]string{"password": password}, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("login did not set a cookie")
	}
	return cookies[0]
}

func adminTestServer(t *testing.T) *Server {
	t.Helper()
	srv, _ := newTestServerWith(t, func(cfg *config.Config) {
		cfg.AdminPassword = "s3cret"
	})
	return srv
}

func TestAdminDisabledWithoutPassword(t *testing.T) {
	srv, _ := newTestServer(t)
	recorder := adminRequest(t, srv, http.MethodGet, "/api/admin/session", nil, nil)
	session := decodeJSON[map[string]bool](t, recorder)
	if session["enabled"] || session["authenticated"] {
		t.Fatalf("session = %+v, want disabled", session)
	}
	if recorder := adminRequest(t, srv, http.MethodPost, "/api/admin/login", map[string]string{"password": "x"}, nil); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("login status = %d, want 503", recorder.Code)
	}
	if recorder := adminRequest(t, srv, http.MethodGet, "/api/admin/settings", nil, nil); recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("settings status = %d, want 503", recorder.Code)
	}
}

func TestAdminLoginThrottlesAndRejects(t *testing.T) {
	srv := adminTestServer(t)
	if recorder := adminRequest(t, srv, http.MethodGet, "/api/admin/settings", nil, nil); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous settings status = %d, want 401", recorder.Code)
	}

	for i := 0; i < loginMaxFailures; i++ {
		recorder := adminRequest(t, srv, http.MethodPost, "/api/admin/login", map[string]string{"password": "wrong"}, nil)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, recorder.Code)
		}
	}
	recorder := adminRequest(t, srv, http.MethodPost, "/api/admin/login", map[string]string{"password": "s3cret"}, nil)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked login status = %d, want 429", recorder.Code)
	}

	if recorder := adminRequest(t, srv, http.MethodPost, "/api/admin/login", map[string]string{"password": "s3cret"}, nil); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked login status = %d, want 429", recorder.Code)
	}
}

func TestAdminSettingsRoundTrip(t *testing.T) {
	srv := adminTestServer(t)
	cookie := loginAdmin(t, srv, "s3cret")

	recorder := adminRequest(t, srv, http.MethodGet, "/api/admin/settings", nil, cookie)
	current := decodeJSON[settings.Settings](t, recorder)
	if current.MaxFiles != 5 || current.DefaultExpirySeconds != 7*24*60*60 {
		t.Fatalf("initial settings = %+v", current)
	}

	current.MaxFiles = 3
	current.MaxUploadBytes = 2 << 20
	current.DefaultExpirySeconds = 3600
	current.MaxExpirySeconds = 86400
	current.DefaultMaxDownloads = 2
	recorder = adminRequest(t, srv, http.MethodPut, "/api/admin/settings", current, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("save status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	saved := decodeJSON[settings.Settings](t, recorder)
	if saved.MaxFiles != 3 || saved.DefaultMaxDownloads != 2 {
		t.Fatalf("saved settings = %+v", saved)
	}

	document := decodeJSON[map[string]any](t, adminRequest(t, srv, http.MethodGet, "/api/config", nil, nil))
	if document["maxFiles"].(float64) != 3 || document["defaultMaxDownloads"].(float64) != 2 {
		t.Fatalf("public config = %+v", document)
	}

	raw, err := srv.store.LoadSettings(t.Context())
	if err != nil || len(raw) == 0 {
		t.Fatalf("stored settings = %q, err = %v", raw, err)
	}

	files := make([]testFile, 4)
	for i := range files {
		files[i] = testFile{name: "note.txt", body: []byte("hello")}
	}
	if recorder := createShareRequest(t, srv, map[string]string{"kind": "files"}, files); recorder.Code != http.StatusBadRequest {
		t.Fatalf("create with 4 files status = %d, want 400", recorder.Code)
	}

	adminRequest(t, srv, http.MethodPost, "/api/admin/logout", nil, cookie)
	if recorder := adminRequest(t, srv, http.MethodGet, "/api/admin/settings", nil, cookie); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("settings after logout status = %d, want 401", recorder.Code)
	}
}

func TestAdminRejectsInvalidSettings(t *testing.T) {
	srv := adminTestServer(t)
	cookie := loginAdmin(t, srv, "s3cret")
	current := decodeJSON[settings.Settings](t, adminRequest(t, srv, http.MethodGet, "/api/admin/settings", nil, cookie))
	current.MaxFiles = 0
	recorder := adminRequest(t, srv, http.MethodPut, "/api/admin/settings", current, cookie)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid settings status = %d, want 400", recorder.Code)
	}
}

func TestAdminRejectsCrossOriginMutations(t *testing.T) {
	srv := adminTestServer(t)
	raw, _ := json.Marshal(map[string]string{"password": "s3cret"})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/login", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	request.RemoteAddr = "203.0.113.21:51234"
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin login status = %d, want 403", recorder.Code)
	}

	cookie := loginAdmin(t, srv, "s3cret")
	raw, _ = json.Marshal(settings.Settings{MaxUploadBytes: 1 << 20, MaxFiles: 2, MaxTextBytes: 1024, DefaultExpirySeconds: 3600, MaxExpirySeconds: 86400})
	request = httptest.NewRequest(http.MethodPut, "/api/admin/settings", bytes.NewReader(raw))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://evil.example")
	request.AddCookie(cookie)
	recorder = httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("cross-origin settings status = %d, want 403", recorder.Code)
	}
}

func uploadBackground(t *testing.T, srv *Server, cookie *http.Cookie, name string, content []byte, label string) *httptest.ResponseRecorder {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	if label != "" {
		if err := writer.WriteField("label", label); err != nil {
			t.Fatalf("write label: %v", err)
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	header.Set("Content-Type", "application/octet-stream")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatalf("create upload part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write upload: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close upload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/admin/backgrounds/upload", body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.AddCookie(cookie)
	request.RemoteAddr = "203.0.113.22:51234"
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	return recorder
}

func fetchPath(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	recorder := httptest.NewRecorder()
	srv.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestAdminBackgrounds(t *testing.T) {
	srv := adminTestServer(t)
	cookie := loginAdmin(t, srv, "s3cret")

	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0x00}, 64)...)
	recorder := uploadBackground(t, srv, cookie, "summer.png", png, "夏日")
	if recorder.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	current := decodeJSON[settings.Settings](t, recorder)
	if len(current.Backgrounds) != 1 {
		t.Fatalf("backgrounds = %+v", current.Backgrounds)
	}
	uploaded := current.Backgrounds[0]
	if uploaded.Kind != "upload" || uploaded.Label != "夏日" || !strings.HasPrefix(uploaded.URL, "/media/backgrounds/") {
		t.Fatalf("uploaded background = %+v", uploaded)
	}

	media := fetchPath(t, srv, uploaded.URL)
	if media.Code != http.StatusOK {
		t.Fatalf("media status = %d", media.Code)
	}
	if got := media.Header().Get("Content-Type"); !strings.HasPrefix(got, "image/png") {
		t.Fatalf("media content type = %q", got)
	}
	if !bytes.Equal(media.Body.Bytes(), png) {
		t.Fatalf("served background bytes differ from upload: got %d bytes %q, want %d bytes", media.Body.Len(), media.Body.Bytes(), len(png))
	}

	if recorder := uploadBackground(t, srv, cookie, "notes.txt", []byte("not an image at all"), ""); recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text upload status = %d, want 415", recorder.Code)
	}

	recorder = adminRequest(t, srv, http.MethodPost, "/api/admin/backgrounds", map[string]string{
		"kind":  "url",
		"url":   "https://images.example.net/wide.jpg",
		"label": "远程图",
	}, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("add url status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	current = decodeJSON[settings.Settings](t, recorder)
	if len(current.Backgrounds) != 2 {
		t.Fatalf("backgrounds after url add = %+v", current.Backgrounds)
	}
	if policy := srv.contentSecurityPolicy(); !strings.Contains(policy, "https://images.example.net") {
		t.Fatalf("csp = %q, want remote image host", policy)
	}

	if recorder := adminRequest(t, srv, http.MethodPost, "/api/admin/backgrounds", map[string]string{"kind": "url", "url": "javascript:alert(1)"}, cookie); recorder.Code != http.StatusBadRequest {
		t.Fatalf("bad url status = %d, want 400", recorder.Code)
	}
	if recorder := adminRequest(t, srv, http.MethodDelete, "/api/admin/backgrounds/missing", nil, cookie); recorder.Code != http.StatusNotFound {
		t.Fatalf("delete missing status = %d, want 404", recorder.Code)
	}

	recorder = adminRequest(t, srv, http.MethodDelete, "/api/admin/backgrounds/"+uploaded.ID, nil, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	current = decodeJSON[settings.Settings](t, recorder)
	if len(current.Backgrounds) != 1 || current.Backgrounds[0].Kind != "url" {
		t.Fatalf("backgrounds after delete = %+v", current.Backgrounds)
	}
	if media := fetchPath(t, srv, uploaded.URL); media.Code != http.StatusNotFound {
		t.Fatalf("deleted media status = %d, want 404", media.Code)
	}
}

func TestAdminBackgroundsAppearInPublicConfig(t *testing.T) {
	srv := adminTestServer(t)
	cookie := loginAdmin(t, srv, "s3cret")
	recorder := adminRequest(t, srv, http.MethodPost, "/api/admin/backgrounds", map[string]string{
		"kind":  "picsum",
		"label": "山野",
	}, cookie)
	if recorder.Code != http.StatusOK {
		t.Fatalf("add background status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	document := decodeJSON[map[string]any](t, adminRequest(t, srv, http.MethodGet, "/api/config", nil, nil))
	backgrounds, ok := document["backgrounds"].([]any)
	if !ok || len(backgrounds) != 1 {
		t.Fatalf("public backgrounds = %+v", document["backgrounds"])
	}
	first := backgrounds[0].(map[string]any)
	if first["kind"] != "picsum" || first["label"] != "山野" {
		t.Fatalf("public background = %+v", first)
	}
}

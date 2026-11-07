package server

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func createFileShare(t *testing.T, srv *Server, name string, body []byte) string {
	t.Helper()
	recorder := createShareRequest(t, srv, map[string]string{"kind": "files"}, []testFile{{
		name:        name,
		contentType: "text/plain; charset=utf-8",
		body:        body,
	}})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create %s status = %d, body = %s", name, recorder.Code, recorder.Body.String())
	}
	return decodeJSON[createdShareDTO](t, recorder).Code
}

func TestIdenticalFilesReuseOnePayload(t *testing.T) {
	srv, st := newTestServer(t)
	body := []byte("exactly the same bytes")

	firstCode := createFileShare(t, srv, "first.txt", body)
	secondCode := createFileShare(t, srv, "renamed.txt", body)

	firstShare, firstFiles, err := st.GetByCode(context.Background(), firstCode)
	if err != nil {
		t.Fatalf("load first share: %v", err)
	}
	_, secondFiles, err := st.GetByCode(context.Background(), secondCode)
	if err != nil {
		t.Fatalf("load second share: %v", err)
	}
	if firstFiles[0].StorageKey != secondFiles[0].StorageKey {
		t.Fatalf("keys differ: %q vs %q", firstFiles[0].StorageKey, secondFiles[0].StorageKey)
	}
	if !strings.HasPrefix(firstFiles[0].StorageKey, "objects/") {
		t.Fatalf("storage key = %q, want a content object", firstFiles[0].StorageKey)
	}
	if firstFiles[0].Name != "first.txt" || secondFiles[0].Name != "renamed.txt" {
		t.Fatalf("filenames should stay per share: %q / %q", firstFiles[0].Name, secondFiles[0].Name)
	}

	for _, code := range []string{firstCode, secondCode} {
		download := getRequest(t, srv, "/api/shares/"+code+"/download", http.MethodGet)
		if download.Code != http.StatusOK {
			t.Fatalf("download %s status = %d", code, download.Code)
		}
		if download.Body.String() != string(body) {
			t.Fatalf("download %s body = %q", code, download.Body.String())
		}
	}

	released, err := st.DeleteShare(context.Background(), firstShare.ID)
	if err != nil {
		t.Fatalf("delete first share: %v", err)
	}
	if len(released) != 0 {
		t.Fatalf("released keys = %v, want none while the other share lives", released)
	}
	if download := getRequest(t, srv, "/api/shares/"+secondCode+"/download", http.MethodGet); download.Code != http.StatusOK {
		t.Fatalf("second share after first deletion status = %d", download.Code)
	}
}

func TestDuplicateFilesInOneShare(t *testing.T) {
	srv, st := newTestServer(t)
	body := []byte("attached twice")
	recorder := createShareRequest(t, srv, map[string]string{"kind": "files"}, []testFile{
		{name: "copy.txt", contentType: "text/plain", body: body},
		{name: "copy-again.txt", contentType: "text/plain", body: body},
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	created := decodeJSON[createdShareDTO](t, recorder)
	if created.FileCount != 2 {
		t.Fatalf("file count = %d", created.FileCount)
	}
	_, files, err := st.GetByCode(context.Background(), created.Code)
	if err != nil {
		t.Fatalf("load share: %v", err)
	}
	if len(files) != 2 || files[0].StorageKey != files[1].StorageKey {
		t.Fatalf("duplicate files should share a key: %+v", files)
	}
	download := getRequest(t, srv, "/api/shares/"+created.Code+"/download", http.MethodGet)
	if download.Code != http.StatusOK {
		t.Fatalf("zip download status = %d", download.Code)
	}
	if !strings.HasPrefix(download.Header().Get("Content-Type"), "application/zip") {
		t.Fatalf("content type = %q", download.Header().Get("Content-Type"))
	}
}

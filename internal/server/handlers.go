package server

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"sendtome/internal/model"
	"sendtome/internal/store"
)

const maxFieldBytes = 4096

var errFieldTooLarge = errors.New("form field too large")

type pendingUpload struct {
	file     model.File
	tempPath string
	digest   string
}

func (s *Server) handleCreateShare(w http.ResponseWriter, r *http.Request) {
	limits := s.settingsSnapshot()
	maxBody := limits.MaxUploadBytes + 1024*1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)

	reader, err := r.MultipartReader()
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "请使用 multipart/form-data 上传内容")
		return
	}

	shareID, err := randomHex(16)
	if err != nil {
		s.logger.Error("generate share id failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "创建分享失败，请稍后再试")
		return
	}

	fields := make(map[string]string)
	var pending []pendingUpload
	var tempPaths []string
	var storedKeys []string
	var totalBytes int64
	committed := false

	defer func() {
		for _, tempPath := range tempPaths {
			_ = os.Remove(tempPath)
		}
		if committed {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, key := range storedKeys {
			if err := s.storage.Delete(cleanupCtx, key); err != nil {
				s.logger.Warn("clean up rejected upload failed", "key", key, "error", err)
			}
		}
	}()

	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			s.writeUploadReadError(w, partErr)
			return
		}

		fieldName := part.FormName()
		if fieldName == "files" {
			filename := part.FileName()
			if filename == "" {
				part.Close()
				continue
			}
			if len(pending) >= limits.MaxFiles {
				part.Close()
				writeAPIError(w, http.StatusBadRequest, "too_many_files", "文件数量超过上限")
				return
			}

			fileID, idErr := randomHex(16)
			if idErr != nil {
				part.Close()
				s.logger.Error("generate file id failed", "error", idErr)
				writeAPIError(w, http.StatusInternalServerError, "server_error", "创建分享失败，请稍后再试")
				return
			}
			displayName := sanitizeFilename(filename)
			buffered := bufio.NewReaderSize(part, 512)
			head, _ := buffered.Peek(512)
			contentType := detectContentType(part.Header.Get("Content-Type"), displayName, head)

			tempFile, tempErr := os.CreateTemp("", "send-to-me-upload-*")
			if tempErr != nil {
				part.Close()
				s.logger.Error("create upload spool failed", "error", tempErr)
				writeAPIError(w, http.StatusInternalServerError, "server_error", "文件暂存失败，请稍后再试")
				return
			}
			tempPaths = append(tempPaths, tempFile.Name())

			remaining := limits.MaxUploadBytes - totalBytes
			hasher := sha256.New()
			size, copyErr := io.Copy(tempFile, io.TeeReader(io.LimitReader(buffered, remaining+1), hasher))
			closeErr := tempFile.Close()
			part.Close()
			if copyErr != nil {
				s.writeUploadReadError(w, copyErr)
				return
			}
			if closeErr != nil {
				s.logger.Error("close upload spool failed", "error", closeErr)
				writeAPIError(w, http.StatusInternalServerError, "server_error", "文件暂存失败，请稍后再试")
				return
			}
			if size > remaining {
				writeAPIError(w, http.StatusRequestEntityTooLarge, "too_large", "文件总大小超过上传上限")
				return
			}
			totalBytes += size

			pending = append(pending, pendingUpload{
				file: model.File{
					ID:          fileID,
					Name:        displayName,
					Size:        size,
					ContentType: contentType,
				},
				tempPath: tempFile.Name(),
				digest:   hex.EncodeToString(hasher.Sum(nil)),
			})
			continue
		}

		limit := int64(maxFieldBytes)
		if fieldName == "text" {
			limit = int64(limits.MaxTextBytes)
		}
		value, readErr := readPartText(part, limit)
		part.Close()
		if readErr != nil {
			if errors.Is(readErr, errFieldTooLarge) {
				if fieldName == "text" {
					writeAPIError(w, http.StatusRequestEntityTooLarge, "text_too_large", "文本内容超过长度上限")
				} else {
					writeAPIError(w, http.StatusBadRequest, "invalid_request", "表单字段过长")
				}
				return
			}
			s.writeUploadReadError(w, readErr)
			return
		}
		if fieldName != "" {
			fields[fieldName] = value
		}
	}

	kind := strings.TrimSpace(fields["kind"])
	if kind == "" {
		if len(pending) > 0 {
			kind = model.KindFiles
		} else {
			kind = model.KindText
		}
	}
	if kind != model.KindFiles && kind != model.KindText {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "未知的分享类型")
		return
	}
	if kind == model.KindFiles {
		if len(pending) == 0 {
			writeAPIError(w, http.StatusBadRequest, "no_files", "请至少选择一个文件")
			return
		}
	}
	if kind == model.KindText {
		if len(pending) > 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_request", "文本分享不能包含文件")
			return
		}
		if strings.TrimSpace(fields["text"]) == "" {
			writeAPIError(w, http.StatusBadRequest, "empty_text", "请输入要发送的文本")
			return
		}
	}

	expiresIn := limits.DefaultExpirySeconds
	if raw, ok := fields["expires_in"]; ok {
		parsed, parseErr := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if parseErr != nil || parsed < 0 {
			writeAPIError(w, http.StatusBadRequest, "invalid_expiry", "有效期格式不正确")
			return
		}
		expiresIn = parsed
	}
	if expiresIn > limits.MaxExpirySeconds {
		writeAPIError(w, http.StatusBadRequest, "invalid_expiry", "有效期超过允许的上限")
		return
	}

	maxDownloads := 5
	if raw, ok := fields["max_downloads"]; ok {
		parsed, parseErr := strconv.Atoi(strings.TrimSpace(raw))
		if parseErr != nil || parsed < 0 || parsed > 10000 {
			writeAPIError(w, http.StatusBadRequest, "invalid_limit", "下载次数设置不正确")
			return
		}
		maxDownloads = parsed
	}

	// Identical bytes are stored once. Reuse an existing payload when the
	// digest is already registered, and remember keys chosen in this request so
	// duplicate attachments do not upload twice. An empty key marks a payload
	// that only exists in the database; CreateShare binds it from its hash.
	seenHashes := make(map[string]string)
	deduplicated := 0
	for i := range pending {
		item := &pending[i]
		item.file.SHA256 = item.digest
		if key, ok := seenHashes[item.digest]; ok {
			item.file.StorageKey = key
			deduplicated++
			continue
		}
		_, lookupErr := s.store.FindObjectKey(r.Context(), item.digest)
		switch {
		case lookupErr == nil:
			item.file.StorageKey = ""
			seenHashes[item.digest] = ""
			deduplicated++
		case errors.Is(lookupErr, store.ErrNotFound):
			key, putErr := s.putPendingUpload(r.Context(), item)
			if putErr != nil {
				if r.Context().Err() != nil {
					return
				}
				s.logger.Error("store uploaded file failed", "error", putErr, "share_id", shareID)
				writeAPIError(w, http.StatusInternalServerError, "storage_error", "文件保存失败，请稍后再试")
				return
			}
			item.file.StorageKey = key
			seenHashes[item.digest] = key
			storedKeys = append(storedKeys, key)
		default:
			s.logger.Error("look up deduplicated object failed", "error", lookupErr, "share_id", shareID)
			writeAPIError(w, http.StatusInternalServerError, "server_error", "创建分享失败，请稍后再试")
			return
		}
	}

	files := make([]model.File, len(pending))
	for i := range pending {
		files[i] = pending[i].file
	}

	createdAt := time.Now().UTC().Truncate(time.Second)
	for i := range files {
		files[i].CreatedAt = createdAt
	}
	expiresAt := time.Time{}
	if expiresIn > 0 {
		expiresAt = createdAt.Add(time.Duration(expiresIn) * time.Second)
	}

	agent := r.UserAgent()
	if runes := []rune(agent); len(runes) > 255 {
		agent = string(runes[:255])
	}

	share := &model.Share{
		ID:            shareID,
		Kind:          kind,
		Text:          fields["text"],
		MaxDownloads:  maxDownloads,
		DownloadCount: 0,
		SenderIP:      clientIP(r, s.cfg.TrustProxy),
		SenderAgent:   agent,
		CreatedAt:     createdAt,
		ExpiresAt:     expiresAt,
	}

	var createErr error
	for attempt := 0; attempt < 8; attempt++ {
		code, codeErr := randomCode(6)
		if codeErr != nil {
			createErr = codeErr
			break
		}
		share.Code = code
		var orphaned []string
		orphaned, createErr = s.store.CreateShare(r.Context(), share, files)
		if errors.Is(createErr, store.ErrStaleObject) {
			// The shared payload expired between lookup and commit; upload the
			// spooled bytes again so this share keeps its own reference.
			recovered, recoverErr := s.recoverStalePayloads(r.Context(), pending, files, seenHashes)
			storedKeys = append(storedKeys, recovered...)
			if recoverErr != nil {
				createErr = recoverErr
				break
			}
			continue
		}
		if createErr == nil {
			if len(orphaned) > 0 {
				s.deleteStoredKeys(orphaned)
			}
			break
		}
		if !errors.Is(createErr, store.ErrCodeConflict) {
			break
		}
	}
	if createErr != nil {
		if r.Context().Err() != nil {
			return
		}
		s.logger.Error("create share failed", "error", createErr, "share_id", shareID)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "创建分享失败，请稍后再试")
		return
	}
	committed = true

	response := createdShareDTO{
		Code:         share.Code,
		Link:         s.shareLink(r, share.Code),
		Kind:         kind,
		FileCount:    len(files),
		MaxDownloads: maxDownloads,
	}
	if !expiresAt.IsZero() {
		expires := expiresAt
		response.ExpiresAt = &expires
	}

	s.logger.Info("share created",
		"share_id", shareID,
		"kind", kind,
		"files", len(files),
		"bytes", totalBytes,
		"deduplicated", deduplicated,
		"sender_ip", share.SenderIP,
	)
	writeJSON(w, http.StatusCreated, response)
}

// putPendingUpload stores a spooled file under a fresh content-address style
// key so a concurrent purge of an older payload can never delete this copy.
func (s *Server) putPendingUpload(ctx context.Context, item *pendingUpload) (string, error) {
	suffix, err := randomHex(20)
	if err != nil {
		return "", err
	}
	key := "objects/" + item.digest[:2] + "/" + suffix
	source, err := os.Open(item.tempPath)
	if err != nil {
		return "", err
	}
	putErr := s.storage.Put(ctx, key, source, item.file.Size, item.file.ContentType)
	closeErr := source.Close()
	if putErr != nil {
		return "", putErr
	}
	if closeErr != nil {
		s.logger.Warn("close upload spool failed", "error", closeErr)
	}
	return key, nil
}

// recoverStalePayloads re-resolves files that hoped to reuse a payload which
// disappeared before the share transaction committed.
func (s *Server) recoverStalePayloads(ctx context.Context, pending []pendingUpload, files []model.File, seen map[string]string) ([]string, error) {
	var uploaded []string
	for i := range files {
		if files[i].StorageKey != "" {
			continue
		}
		digest := files[i].SHA256
		if key, ok := seen[digest]; ok && key != "" {
			files[i].StorageKey = key
			continue
		}
		if _, err := s.store.FindObjectKey(ctx, digest); err == nil {
			continue
		} else if !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
		key, err := s.putPendingUpload(ctx, &pending[i])
		if err != nil {
			return nil, err
		}
		files[i].StorageKey = key
		seen[digest] = key
		uploaded = append(uploaded, key)
	}
	return uploaded, nil
}

func (s *Server) deleteStoredKeys(keys []string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	for _, key := range keys {
		if err := s.storage.Delete(ctx, key); err != nil {
			s.logger.Warn("delete superseded payload failed", "key", key, "error", err)
		}
	}
}

func readPartText(part *multipart.Part, limit int64) (string, error) {
	data, err := io.ReadAll(io.LimitReader(part, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > limit {
		return "", errFieldTooLarge
	}
	return string(data), nil
}

func (s *Server) writeUploadReadError(w http.ResponseWriter, err error) {
	var maxBytesError *http.MaxBytesError
	if errors.As(err, &maxBytesError) {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "too_large", "上传内容超过大小上限")
		return
	}
	if errors.Is(err, context.Canceled) {
		return
	}
	s.logger.Warn("read upload failed", "error", err)
	writeAPIError(w, http.StatusBadRequest, "invalid_request", "上传内容读取失败")
}

func (s *Server) handleMetadata(w http.ResponseWriter, r *http.Request) {
	code := normalizeCode(r.PathValue("code"))
	if code == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "取件码不存在或已失效")
		return
	}
	share, files, err := s.store.GetByCode(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "not_found", "取件码不存在或已失效")
		return
	}
	if err != nil {
		s.logger.Error("load share metadata failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "读取分享失败，请稍后再试")
		return
	}
	deviceID := s.deviceID(w, r)
	consumed, err := s.store.ConsumedByDevice(r.Context(), share.ID, deviceID)
	if err != nil {
		// Metadata must still render; the device simply looks new.
		s.logger.Warn("check device claim failed", "share_id", share.ID, "error", err)
		consumed = false
	}
	writeJSON(w, http.StatusOK, newShareDTO(share, files, time.Now(), consumed))
}

// handleDownloadCheck lets the frontend verify that a download is still
// available without claiming one. HEAD requests never touch the counter.
func (s *Server) handleDownloadCheck(w http.ResponseWriter, r *http.Request) {
	code := normalizeCode(r.PathValue("code"))
	if code == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "取件码不存在或已失效")
		return
	}
	share, _, err := s.store.GetByCode(r.Context(), code)
	if errors.Is(err, store.ErrNotFound) {
		writeAPIError(w, http.StatusNotFound, "not_found", "取件码不存在或已失效")
		return
	}
	if err != nil {
		s.logger.Error("check download failed", "error", err)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "取件失败，请稍后再试")
		return
	}
	if share.Kind != model.KindFiles {
		writeAPIError(w, http.StatusNotFound, "not_found", "取件码不存在或已失效")
		return
	}
	deviceID := s.deviceID(w, r)
	consumed, err := s.store.ConsumedByDevice(r.Context(), share.ID, deviceID)
	if err != nil {
		s.logger.Warn("check device claim failed", "share_id", share.ID, "error", err)
	}
	switch share.Status(time.Now()) {
	case model.StatusExpired:
		writeAPIError(w, http.StatusGone, "expired", "这份分享已经过期")
		return
	case model.StatusExhausted:
		// A device that already picked this drop up may download again.
		if !consumed {
			writeAPIError(w, http.StatusGone, "exhausted", "这份分享的下载次数已用完")
			return
		}
	}
	setDownloadHeaders(w, share)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		s.handleDownloadCheck(w, r)
		return
	}
	code := normalizeCode(r.PathValue("code"))
	if code == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "取件码不存在或已失效")
		return
	}

	share, files, _, err := s.store.Consume(r.Context(), code, model.KindFiles, s.deviceID(w, r))
	if err != nil {
		s.writeConsumeError(w, err)
		return
	}
	if len(files) == 0 {
		s.logger.Error("file share has no files", "share_id", share.ID)
		writeAPIError(w, http.StatusInternalServerError, "server_error", "文件读取失败，请稍后再试")
		return
	}

	type openedFile struct {
		file    model.File
		content io.ReadCloser
	}
	opened := make([]openedFile, 0, len(files))
	closeOpened := func() {
		for _, item := range opened {
			item.content.Close()
		}
	}
	defer closeOpened()

	for _, file := range files {
		content, openErr := s.storage.Get(r.Context(), file.StorageKey)
		if openErr != nil {
			s.logger.Error("open stored file failed", "share_id", share.ID, "file_id", file.ID, "error", openErr)
			writeAPIError(w, http.StatusInternalServerError, "storage_error", "文件读取失败，请稍后再试")
			return
		}
		opened = append(opened, openedFile{file: file, content: content})
	}

	setDownloadHeaders(w, share)
	if len(opened) == 1 {
		file := opened[0].file
		w.Header().Set("Content-Type", file.ContentType)
		w.Header().Set("Content-Disposition", attachmentHeader(file.Name))
		w.Header().Set("Content-Length", strconv.FormatInt(file.Size, 10))
		w.WriteHeader(http.StatusOK)
		if _, copyErr := io.Copy(w, opened[0].content); copyErr != nil && r.Context().Err() == nil {
			s.logger.Warn("stream download interrupted", "share_id", share.ID, "error", copyErr)
		}
		return
	}

	archiveName := "send-to-me-" + share.Code + ".zip"
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", attachmentHeader(archiveName))
	w.WriteHeader(http.StatusOK)

	archive := zip.NewWriter(w)
	usedNames := make(map[string]int)
	for _, item := range opened {
		header := &zip.FileHeader{
			Name:   uniqueArchiveName(usedNames, item.file.Name),
			Method: zip.Deflate,
		}
		header.SetMode(0o644)
		header.Modified = item.file.CreatedAt
		writer, createErr := archive.CreateHeader(header)
		if createErr != nil {
			s.logger.Warn("create zip entry failed", "share_id", share.ID, "error", createErr)
			return
		}
		if _, copyErr := io.Copy(writer, item.content); copyErr != nil {
			s.logger.Warn("stream zip entry interrupted", "share_id", share.ID, "error", copyErr)
			return
		}
	}
	if closeErr := archive.Close(); closeErr != nil && r.Context().Err() == nil {
		s.logger.Warn("finish zip download failed", "share_id", share.ID, "error", closeErr)
	}
}

func setDownloadHeaders(w http.ResponseWriter, share *model.Share) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "sandbox")
	if remaining := share.Remaining(); remaining != nil {
		w.Header().Set("X-Share-Remaining", strconv.Itoa(*remaining))
	} else {
		w.Header().Set("X-Share-Remaining", "unlimited")
	}
}

func (s *Server) handleRevealText(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		w.Header().Set("Allow", "GET")
		writeAPIError(w, http.StatusMethodNotAllowed, "method_not_allowed", "请使用 GET 查看文本")
		return
	}
	code := normalizeCode(r.PathValue("code"))
	if code == "" {
		writeAPIError(w, http.StatusNotFound, "not_found", "取件码不存在或已失效")
		return
	}

	share, _, consumed, err := s.store.Consume(r.Context(), code, model.KindText, s.deviceID(w, r))
	if err != nil {
		s.writeConsumeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"text":               share.Text,
		"downloadCount":      share.DownloadCount,
		"maxDownloads":       share.MaxDownloads,
		"remainingDownloads": share.Remaining(),
		"consumed":           consumed,
	})
}

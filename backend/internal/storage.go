package internal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type StoredObject struct{ Key, Backend string }
type Storage interface {
	Put(context.Context, string, io.Reader, int64, string) (StoredObject, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Delete(context.Context, string) error
}

type LocalStorage struct{ root string }

func NewLocalStorage(root string) (*LocalStorage, error) {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	return &LocalStorage{root: root}, nil
}
func (s *LocalStorage) path(key string) (string, error) {
	if strings.TrimSpace(key) == "" || filepath.IsAbs(key) {
		return "", fmt.Errorf("invalid storage key")
	}
	for _, part := range strings.FieldsFunc(filepath.ToSlash(key), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return "", fmt.Errorf("invalid storage key")
		}
	}
	clean := filepath.Clean(filepath.FromSlash(key))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid storage key")
	}
	path := filepath.Join(s.root, clean)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid storage key")
	}
	return path, nil
}
func (s *LocalStorage) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) (StoredObject, error) {
	path, err := s.path(key)
	if err != nil {
		return StoredObject{}, err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return StoredObject{}, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return StoredObject{}, err
	}
	defer f.Close()
	if _, err = io.Copy(f, r); err != nil {
		return StoredObject{}, err
	}
	return StoredObject{Key: key, Backend: "local"}, nil
}
func (s *LocalStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	path, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}
func (s *LocalStorage) Delete(_ context.Context, key string) error {
	path, err := s.path(key)
	if err != nil {
		return err
	}
	return os.Remove(path)
}

type TelegramStorage struct {
	token, chatID string
	client        *http.Client
}

func NewTelegramStorage(token, chatID string) *TelegramStorage {
	return &TelegramStorage{token: token, chatID: chatID, client: &http.Client{Timeout: 45 * time.Second}}
}
func (s *TelegramStorage) endpoint(method string) string {
	return "https://api.telegram.org/bot" + s.token + "/" + method
}
func (s *TelegramStorage) Put(ctx context.Context, _ string, r io.Reader, size int64, contentType string) (StoredObject, error) {
	limit := int64(64 << 20)
	if size > 0 && size < limit {
		limit = size + 1
	}
	data, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return StoredObject{}, err
	}
	if int64(len(data)) > limit-1 {
		return StoredObject{}, fmt.Errorf("telegram object exceeds storage limit")
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if err = mw.WriteField("chat_id", s.chatID); err != nil {
		return StoredObject{}, err
	}
	part, err := mw.CreateFormFile("document", "upload")
	if err != nil {
		return StoredObject{}, err
	}
	if _, err = part.Write(data); err != nil {
		return StoredObject{}, err
	}
	_ = mw.WriteField("caption", "imagehub content-type="+contentType)
	if err = mw.Close(); err != nil {
		return StoredObject{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint("sendDocument"), &body)
	if err != nil {
		return StoredObject{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := s.client.Do(req)
	if err != nil {
		return StoredObject{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return StoredObject{}, fmt.Errorf("telegram upload returned %s", resp.Status)
	}
	var result struct {
		OK     bool `json:"ok"`
		Result struct {
			MessageID int `json:"message_id"`
			Document  struct {
				FileID string `json:"file_id"`
			} `json:"document"`
		} `json:"result"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return StoredObject{}, err
	}
	if !result.OK || result.Result.Document.FileID == "" {
		return StoredObject{}, fmt.Errorf("telegram upload failed")
	}
	return StoredObject{Key: strconv.Itoa(result.Result.MessageID) + ":" + result.Result.Document.FileID, Backend: "telegram"}, nil
}
func (s *TelegramStorage) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid telegram object")
	}
	var get struct {
		OK     bool `json:"ok"`
		Result struct {
			FilePath string `json:"file_path"`
		} `json:"result"`
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.endpoint("getFile")+"?file_id="+url.QueryEscape(parts[1]), nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err = json.NewDecoder(resp.Body).Decode(&get); err != nil || !get.OK {
		return nil, fmt.Errorf("telegram getFile failed")
	}
	fileURL := "https://api.telegram.org/file/bot" + s.token + "/" + get.Result.FilePath
	fileReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	fileResp, err := s.client.Do(fileReq)
	if err != nil {
		return nil, err
	}
	if fileResp.StatusCode >= 300 {
		fileResp.Body.Close()
		return nil, fmt.Errorf("telegram file returned %s", fileResp.Status)
	}
	return fileResp.Body, nil
}
func (s *TelegramStorage) Delete(ctx context.Context, key string) error {
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid telegram object")
	}
	form := url.Values{"chat_id": {s.chatID}, "message_id": {parts[0]}}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint("deleteMessage"), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("telegram delete returned %s", resp.Status)
	}
	return nil
}

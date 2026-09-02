package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"landrop/internal/storage"
)

func TestFileTransferFlow(t *testing.T) {
	t.Parallel()
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := New(Options{
		Store:    store,
		Token:    "482901",
		MaxBytes: 1024 * 1024,
		ShareURL: "http://192.168.1.20:8080/?token=482901",
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()

	unauthorized, err := http.Get(testServer.URL + "/api/files")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.StatusCode)
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	sessionBody := strings.NewReader(`{"token":"482901"}`)
	sessionResponse, err := client.Post(testServer.URL+"/api/session", "application/json", sessionBody)
	if err != nil {
		t.Fatal(err)
	}
	sessionResponse.Body.Close()
	if sessionResponse.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d", sessionResponse.StatusCode)
	}

	shareResponse, err := client.Get(testServer.URL + "/api/share")
	if err != nil {
		t.Fatal(err)
	}
	var share struct {
		URL     string `json:"url"`
		Address string `json:"address"`
	}
	if err := json.NewDecoder(shareResponse.Body).Decode(&share); err != nil {
		t.Fatal(err)
	}
	shareResponse.Body.Close()
	if shareResponse.StatusCode != http.StatusOK || share.URL != "http://192.168.1.20:8080/?token=482901" || share.Address != "192.168.1.20:8080" {
		t.Fatalf("share response = %+v, status = %d", share, shareResponse.StatusCode)
	}

	qrResponse, err := client.Get(testServer.URL + "/api/share/qr")
	if err != nil {
		t.Fatal(err)
	}
	qrPNG, err := io.ReadAll(qrResponse.Body)
	qrResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if qrResponse.StatusCode != http.StatusOK || qrResponse.Header.Get("Content-Type") != "image/png" || !bytes.HasPrefix(qrPNG, []byte("\x89PNG\r\n\x1a\n")) {
		t.Fatalf("QR response status = %d, type = %q", qrResponse.StatusCode, qrResponse.Header.Get("Content-Type"))
	}

	content := []byte("abcdef")
	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	part, err := writer.CreateFormFile("file", "demo.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	uploadRequest, err := http.NewRequest(http.MethodPost, testServer.URL+"/api/files", &uploadBody)
	if err != nil {
		t.Fatal(err)
	}
	uploadRequest.Header.Set("Content-Type", writer.FormDataContentType())
	uploadResponse, err := client.Do(uploadRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer uploadResponse.Body.Close()
	if uploadResponse.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(uploadResponse.Body)
		t.Fatalf("upload status = %d: %s", uploadResponse.StatusCode, body)
	}

	listResponse, err := client.Get(testServer.URL + "/api/files")
	if err != nil {
		t.Fatal(err)
	}
	defer listResponse.Body.Close()
	var list struct {
		Count int            `json:"count"`
		Files []storage.File `json:"files"`
	}
	if err := json.NewDecoder(listResponse.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if list.Count != 1 || len(list.Files) != 1 || list.Files[0].Name != "demo.txt" {
		t.Fatalf("list response = %+v", list)
	}

	downloadURL := testServer.URL + "/api/files/" + url.PathEscape("demo.txt")
	downloadResponse, err := client.Get(downloadURL)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := io.ReadAll(downloadResponse.Body)
	downloadResponse.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if downloadResponse.StatusCode != http.StatusOK || !bytes.Equal(downloaded, content) {
		t.Fatalf("download status = %d, body = %q", downloadResponse.StatusCode, downloaded)
	}
	if downloadResponse.Header.Get("X-Content-SHA256") == "" {
		t.Fatal("download response is missing checksum header")
	}

	rangeRequest, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	rangeRequest.Header.Set("Range", "bytes=1-3")
	rangeResponse, err := client.Do(rangeRequest)
	if err != nil {
		t.Fatal(err)
	}
	rangeBody, _ := io.ReadAll(rangeResponse.Body)
	rangeResponse.Body.Close()
	if rangeResponse.StatusCode != http.StatusPartialContent || string(rangeBody) != "bcd" {
		t.Fatalf("range status = %d, body = %q", rangeResponse.StatusCode, rangeBody)
	}

	deleteRequest, err := http.NewRequest(http.MethodDelete, downloadURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	deleteResponse, err := client.Do(deleteRequest)
	if err != nil {
		t.Fatal(err)
	}
	deleteResponse.Body.Close()
	if deleteResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", deleteResponse.StatusCode)
	}
}

func TestFrontendAndSecurityHeaders(t *testing.T) {
	t.Parallel()
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := New(Options{Store: store, Token: "test", MaxBytes: 1024})
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()

	response, err := http.Get(testServer.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("bigbang")) {
		t.Fatalf("frontend status = %d", response.StatusCode)
	}
	if response.Header.Get("Content-Security-Policy") == "" || response.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatal("security headers are missing")
	}
}

package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"landrop/internal/storage"
)

func TestEventStreamPublishesFileChanges(t *testing.T) {
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := New(Options{
		Store:    store,
		Token:    "482901",
		MaxBytes: 1024 * 1024,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()

	client := authenticatedClient(t, testServer.URL, "482901")
	ctx, cancel := context.WithCancel(context.Background())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, testServer.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("event stream status = %d, type = %q", response.StatusCode, response.Header.Get("Content-Type"))
	}

	reader := bufio.NewReader(response.Body)
	ready := readEvent(t, reader)
	if ready.Kind != "ready" {
		t.Fatalf("initial event = %+v", ready)
	}

	uploadResponse := uploadFile(t, client, testServer.URL, "live.txt", []byte("live update"))
	uploadResponse.Body.Close()
	if uploadResponse.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d", uploadResponse.StatusCode)
	}
	created := readEvent(t, reader)
	if created.Kind != "created" || created.Name != "live.txt" || created.Version == 0 {
		t.Fatalf("created event = %+v", created)
	}

	cancel()
	response.Body.Close()
	deadline := time.Now().Add(time.Second)
	for app.events.subscriberCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := app.events.subscriberCount(); got != 0 {
		t.Fatalf("event subscribers after disconnect = %d", got)
	}
}

func TestSessionRateLimitAndRecovery(t *testing.T) {
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := New(Options{
		Store:    store,
		Token:    "482901",
		MaxBytes: 1024,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	now := time.Date(2026, time.August, 10, 12, 0, 0, 0, time.UTC)
	app.limiter = newLoginLimiter(3, time.Minute, time.Minute)
	app.limiter.now = func() time.Time { return now }
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()

	for attempt := 1; attempt <= 3; attempt++ {
		response := postToken(t, http.DefaultClient, testServer.URL, "wrong")
		response.Body.Close()
		want := http.StatusUnauthorized
		if attempt == 3 {
			want = http.StatusTooManyRequests
			if response.Header.Get("Retry-After") == "" {
				t.Fatal("rate-limited response is missing Retry-After")
			}
		}
		if response.StatusCode != want {
			t.Fatalf("attempt %d status = %d, want %d", attempt, response.StatusCode, want)
		}
	}

	blocked := postToken(t, http.DefaultClient, testServer.URL, "482901")
	blocked.Body.Close()
	if blocked.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("correct token while blocked status = %d", blocked.StatusCode)
	}

	now = now.Add(2 * time.Minute)
	recovered := postToken(t, http.DefaultClient, testServer.URL, "482901")
	recovered.Body.Close()
	if recovered.StatusCode != http.StatusOK {
		t.Fatalf("correct token after block status = %d", recovered.StatusCode)
	}
}

func TestLoginLimiterBoundsTrackedClients(t *testing.T) {
	limiter := newLoginLimiter(5, time.Minute, time.Minute)
	limiter.maxEntries = 3
	for index := 0; index < 260; index++ {
		limiter.failure(fmt.Sprintf("192.0.2.%d", index))
	}
	if got := len(limiter.attempts); got != limiter.maxEntries {
		t.Fatalf("tracked clients = %d, want %d", got, limiter.maxEntries)
	}
}

func TestInvalidAPIHeaderIsRateLimited(t *testing.T) {
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := New(Options{
		Store:    store,
		Token:    "482901",
		MaxBytes: 1024,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	app.limiter = newLoginLimiter(2, time.Minute, time.Minute)
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()

	for attempt := 1; attempt <= 2; attempt++ {
		request, err := http.NewRequest(http.MethodGet, testServer.URL+"/api/files", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("X-Share-Token", "wrong")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		want := http.StatusUnauthorized
		if attempt == 2 {
			want = http.StatusTooManyRequests
		}
		if response.StatusCode != want {
			t.Fatalf("attempt %d status = %d, want %d", attempt, response.StatusCode, want)
		}
	}
}

func TestHealthAndRequestTracing(t *testing.T) {
	store, err := storage.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	app := New(Options{
		Store:    store,
		Token:    "test",
		MaxBytes: 1024,
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	testServer := httptest.NewServer(app.Handler())
	defer testServer.Close()

	response, err := http.Get(testServer.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("X-Request-ID") == "" {
		t.Fatalf("health status = %d, request id = %q", response.StatusCode, response.Header.Get("X-Request-ID"))
	}
	var health struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "ok" {
		t.Fatalf("health response = %+v", health)
	}
}

func authenticatedClient(t *testing.T, baseURL, token string) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Jar: jar}
	response := postToken(t, client, baseURL, token)
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d", response.StatusCode)
	}
	return client
}

func postToken(t *testing.T, client *http.Client, baseURL, token string) *http.Response {
	t.Helper()
	response, err := client.Post(baseURL+"/api/session", "application/json", strings.NewReader(`{"token":"`+token+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func uploadFile(t *testing.T, client *http.Client, baseURL, name string, data []byte) *http.Response {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/files", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func readEvent(t *testing.T, reader *bufio.Reader) fileEvent {
	t.Helper()
	var data string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "data: ") {
			data = strings.TrimPrefix(line, "data: ")
		}
	}
	var event fileEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		t.Fatalf("decode event %q: %v", data, err)
	}
	return event
}

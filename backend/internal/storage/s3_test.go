package storage_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/whoaskedssselery/studyvault/backend/internal/storage"
)

// Тесты идут на настоящем S3-совместимом хранилище (docker compose up) во временном бакете,
// который удаляется в конце. Без S3_ENDPOINT они пропускаются.
func s3Env(t *testing.T) storage.Config {
	t.Helper()
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_ENDPOINT is not set")
	}
	return storage.Config{
		Endpoint:  endpoint,
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		Bucket:    "test-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20],
	}
}

func newS3(t *testing.T) *storage.S3 {
	t.Helper()
	cfg := s3Env(t)
	s, err := storage.NewS3(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, err := minio.New(cfg.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, "")})
		if err != nil {
			return
		}
		ctx := context.Background()
		for o := range c.ListObjects(ctx, cfg.Bucket, minio.ListObjectsOptions{Recursive: true}) {
			_ = c.RemoveObject(ctx, cfg.Bucket, o.Key, minio.RemoveObjectOptions{})
		}
		_ = c.RemoveBucket(ctx, cfg.Bucket)
	})
	return s
}

func put(t *testing.T, target string, body []byte, contentType string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, target, bytes.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func TestS3_PresignedPut_EnforcesTheDeclaredSizeAndType(t *testing.T) {
	s := newS3(t)
	ctx := context.Background()
	key := uuid.NewString() + "/" + uuid.NewString()
	body := []byte("hello, study vault")

	target, err := s.PresignPut(ctx, key, "text/plain", int64(len(body)), time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if code := put(t, target, append(body, bytes.Repeat([]byte("x"), 1000)...), "text/plain"); code != http.StatusForbidden {
		t.Errorf("a larger body than declared: status = %d, want 403", code)
	}
	if code := put(t, target, body, "application/x-evil"); code != http.StatusForbidden {
		t.Errorf("another content type than declared: status = %d, want 403", code)
	}
	if code := put(t, target, body, ""); code != http.StatusForbidden {
		t.Errorf("no content type: status = %d, want 403", code)
	}
	if _, err := s.Stat(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("nothing may be stored after the rejected uploads: err = %v", err)
	}

	if code := put(t, target, body, "text/plain"); code != http.StatusOK {
		t.Fatalf("the exact declared upload: status = %d, want 200", code)
	}
	size, err := s.Stat(ctx, key)
	if err != nil || size != int64(len(body)) {
		t.Errorf("Stat = %d, %v; want %d", size, err, len(body))
	}
}

func TestS3_PresignedGet_ReturnsTheFileAsADownload(t *testing.T) {
	s := newS3(t)
	ctx := context.Background()
	key := uuid.NewString() + "/" + uuid.NewString()
	body := []byte("конспект")
	target, _ := s.PresignPut(ctx, key, "text/plain", int64(len(body)), time.Minute)
	if code := put(t, target, body, "text/plain"); code != http.StatusOK {
		t.Fatalf("upload: %d", code)
	}

	link, err := s.PresignGet(ctx, key, "Лекция №1.txt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK || !bytes.Equal(got, body) {
		t.Fatalf("status = %d, body = %q", resp.StatusCode, got)
	}
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "filename*=utf-8''") {
		t.Errorf("must force a download with a safely encoded name, got %q", cd)
	}
}

func TestS3_DownloadName_CannotInjectHeaders(t *testing.T) {
	s := newS3(t)
	ctx := context.Background()
	key := uuid.NewString()
	target, _ := s.PresignPut(ctx, key, "text/plain", 1, time.Minute)
	put(t, target, []byte("x"), "text/plain")

	link, err := s.PresignGet(ctx, key, "evil\r\nSet-Cookie: a=b\".txt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(link)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Set-Cookie") != "" {
		t.Error("the file name must not be able to inject response headers")
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Errorf("Content-Disposition = %q", resp.Header.Get("Content-Disposition"))
	}
}

func TestS3_StatAndRemove(t *testing.T) {
	s := newS3(t)
	ctx := context.Background()
	key := uuid.NewString()

	if _, err := s.Stat(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("a missing object: err = %v, want ErrNotFound", err)
	}

	target, _ := s.PresignPut(ctx, key, "text/plain", 3, time.Minute)
	put(t, target, []byte("abc"), "text/plain")
	if err := s.Remove(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat(ctx, key); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("after Remove: err = %v, want ErrNotFound", err)
	}
	if err := s.Remove(ctx, key); err != nil {
		t.Errorf("removing an absent object must not fail: %v", err)
	}
}

func TestS3_PublicEndpoint_IsUsedInLinks(t *testing.T) {
	cfg := s3Env(t)
	cfg.PublicEndpoint = "files.example.org"
	s, err := storage.NewS3(cfg)
	if err != nil {
		t.Fatal(err)
	}

	put, err := s.PresignPut(context.Background(), "k", "text/plain", 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	get, _ := s.PresignGet(context.Background(), "k", "a.txt", time.Minute)

	for _, link := range []string{put, get} {
		u, _ := url.Parse(link)
		if u.Host != "files.example.org" {
			t.Errorf("link host = %q, want the public endpoint", u.Host)
		}
	}
}

func TestS3_Unreachable_FailsWithoutBeingMistakenForMissing(t *testing.T) {
	s, err := storage.NewS3(storage.Config{Endpoint: "127.0.0.1:1", AccessKey: "a", SecretKey: "bbbbbbbb", Bucket: "nope"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = s.Stat(ctx, "k")

	if err == nil || errors.Is(err, storage.ErrNotFound) || !errors.Is(err, storage.ErrUnavailable) {
		t.Fatalf("err = %v: an outage must be ErrUnavailable and never look like a missing file", err)
	}
}

func TestNewS3_InvalidConfig_ReturnsError(t *testing.T) {
	if _, err := storage.NewS3(storage.Config{Endpoint: "", Bucket: "b"}); err == nil {
		t.Error("an empty endpoint must be rejected")
	}
	if _, err := storage.NewS3(storage.Config{Endpoint: "localhost:9000", Bucket: ""}); err == nil {
		t.Error("an empty bucket must be rejected")
	}
}

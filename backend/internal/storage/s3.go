package storage

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const defaultRegion = "us-east-1"

var (
	ErrNotFound = errors.New("object not found")
	// ErrUnavailable: хранилище не отвечает или отказывает. Не путать с ErrNotFound: файл мог быть на месте.
	ErrUnavailable = errors.New("storage is unavailable")
)

type Config struct {
	Endpoint  string // адрес хранилища для самого сервера, например localhost:9000
	AccessKey string
	SecretKey string
	Bucket    string
	Region    string // по умолчанию us-east-1
	UseSSL    bool
	// PublicEndpoint — адрес, который увидит клиент в presigned-ссылках, если он отличается от
	// Endpoint (например, внутри Docker-сети и снаружи). Пусто: тот же, что Endpoint.
	PublicEndpoint string
}

// S3 работает с любым S3-совместимым хранилищем. Сами файлы идут напрямую между клиентом и
// хранилищем по presigned-ссылкам, через сервер они не проходят.
type S3 struct {
	client *minio.Client // действия сервера: проверка, удаление
	signer *minio.Client // подпись ссылок для клиента (сеть не нужна)
	bucket string

	mu    sync.Mutex
	ready bool
}

func NewS3(cfg Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, errors.New("storage: endpoint and bucket are required")
	}
	if cfg.Region == "" {
		cfg.Region = defaultRegion
	}
	newClient := func(endpoint string) (*minio.Client, error) {
		// Регион задан явно, поэтому клиент не ходит в сеть за ним, а подпись ссылок остаётся локальной.
		return minio.New(endpoint, &minio.Options{
			Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
			Secure: cfg.UseSSL,
			Region: cfg.Region,
		})
	}
	client, err := newClient(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("storage client: %w", err)
	}
	signer := client
	if cfg.PublicEndpoint != "" && cfg.PublicEndpoint != cfg.Endpoint {
		if signer, err = newClient(cfg.PublicEndpoint); err != nil {
			return nil, fmt.Errorf("storage public client: %w", err)
		}
	}
	return &S3{client: client, signer: signer, bucket: cfg.Bucket}, nil
}

// ensureBucket создаёт бакет при первом обращении и повторяет попытку, пока она не удастся,
// так что сервер можно запускать раньше хранилища.
func (s *S3) ensureBucket(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ready {
		return nil
	}
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("%w: check bucket: %v", ErrUnavailable, err)
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{}); err != nil {
			if code := minio.ToErrorResponse(err).Code; code != "BucketAlreadyOwnedByYou" && code != "BucketAlreadyExists" {
				return fmt.Errorf("%w: create bucket: %v", ErrUnavailable, err)
			}
		}
	}
	s.ready = true
	return nil
}

// PresignPut выдаёт ссылку для загрузки файла напрямую в хранилище. В подпись входят
// Content-Type и Content-Length, поэтому залить другой тип или файл другого размера по ней нельзя:
// хранилище ответит 403. Клиент должен отправить ровно эти значения.
func (s *S3) PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, error) {
	if err := s.ensureBucket(ctx); err != nil {
		return "", err
	}
	h := http.Header{}
	h.Set("Content-Type", contentType)
	h.Set("Content-Length", strconv.FormatInt(size, 10))
	u, err := s.signer.PresignHeader(ctx, http.MethodPut, s.bucket, key, ttl, url.Values{}, h)
	if err != nil {
		return "", fmt.Errorf("presign put: %w", err)
	}
	return u.String(), nil
}

// PresignGet выдаёт временную ссылку на скачивание. Файл всегда отдаётся как вложение
// (Content-Disposition: attachment), а имя кодируется по RFC 6266, так что в заголовок ничего не внедрить.
func (s *S3) PresignGet(ctx context.Context, key, fileName string, ttl time.Duration) (string, error) {
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": fileName})
	if disposition == "" { // имя не получилось закодировать: отдаём без него
		disposition = "attachment"
	}
	u, err := s.signer.PresignedGetObject(ctx, s.bucket, key, ttl, url.Values{"response-content-disposition": {disposition}})
	if err != nil {
		return "", fmt.Errorf("presign get: %w", err)
	}
	return u.String(), nil
}

// Stat возвращает размер объекта или ErrNotFound. Недоступность хранилища ErrNotFound не маскируется.
func (s *S3) Stat(ctx context.Context, key string) (int64, error) {
	if err := s.ensureBucket(ctx); err != nil {
		return 0, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return 0, ErrNotFound
		}
		return 0, fmt.Errorf("%w: stat object: %v", ErrUnavailable, err)
	}
	return info.Size, nil
}

// Remove удаляет объект. Отсутствие объекта не ошибка.
func (s *S3) Remove(ctx context.Context, key string) error {
	if err := s.ensureBucket(ctx); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("%w: remove object: %v", ErrUnavailable, err)
	}
	return nil
}

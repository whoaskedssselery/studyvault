package search

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/meilisearch/meilisearch-go"
)

const (
	// IndexUID — имя индекса заметок в Meilisearch.
	IndexUID      = "notes"
	primaryKey    = "id"
	cropLength    = 25 // слов в сниппете
	taskPollEvery = 20 * time.Millisecond
	// requestTimeout короткий: индексация идёт прямо во время запроса пользователя.
	requestTimeout = 3 * time.Second
	// setupCooldown: пауза между попытками настроить индекс, пока Meilisearch не отвечает.
	setupCooldown = 5 * time.Second
	reindexBatch  = 500
)

var errCoolingDown = errors.New("meilisearch setup failed recently, not retrying yet")

// Doc — документ индекса. Индекс лишь ускоряет поиск: источник правды — Postgres.
type Doc struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Title       string    `json:"title"`
	ContentText string    `json:"content_text"`
	Status      string    `json:"status"` // draft, pending, published или rejected: по нему библиотека ищет только опубликованное
}

// Meili — адаптер к Meilisearch. Настройки индекса применяются при первом обращении
// (и повторяются, пока не получится), так что сервер можно запускать раньше Meilisearch.
type Meili struct {
	client meilisearch.ServiceManager
	uid    string

	mu      sync.Mutex
	ready   bool
	timeout time.Duration

	lastTask atomic.Int64 // самая свежая из поставленных в очередь задач, для Wait

	now     func() time.Time
	retryAt time.Time // до этого момента настройку индекса не повторяем: прошлая попытка провалилась
}

// MeiliOption настраивает адаптер; нужен в основном тестам.
type MeiliOption func(*Meili)

// WithTimeout задаёт предел ожидания ответа Meilisearch на один запрос.
func WithTimeout(d time.Duration) MeiliOption { return func(m *Meili) { m.timeout = d } }

// WithClock подменяет часы: так проверяется повтор настройки после паузы.
func WithClock(now func() time.Time) MeiliOption { return func(m *Meili) { m.now = now } }

func NewMeili(url, apiKey, indexUID string, opts ...MeiliOption) *Meili {
	m := &Meili{uid: indexUID, now: time.Now, timeout: requestTimeout}
	for _, o := range opts {
		o(m)
	}
	httpClient := &http.Client{Timeout: m.timeout}
	m.client = meilisearch.New(url, meilisearch.WithAPIKey(apiKey), meilisearch.WithCustomClient(httpClient))
	return m
}

func (m *Meili) ensure(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ready {
		return nil
	}
	// Пока Meilisearch не отвечает, не заставляем каждый запрос ждать таймаут заново: запись заметок
	// и поиск не должны зависеть от скорости чужого сервиса. Настройку повторим после паузы.
	if m.now().Before(m.retryAt) {
		return errCoolingDown
	}
	err := m.setup(ctx)
	if err != nil {
		m.retryAt = m.now().Add(setupCooldown)
		return err
	}
	m.ready = true
	return nil
}

func (m *Meili) setup(ctx context.Context) error {
	// Первичный ключ задаём явно: Meilisearch не угадает его, раз id и user_id оба похожи на ключ.
	// Если индекс уже есть, задача завершится ошибкой, и это нормально.
	if task, err := m.client.CreateIndex(&meilisearch.IndexConfig{Uid: m.uid, PrimaryKey: primaryKey}); err == nil {
		_, _ = m.client.WaitForTaskWithContext(ctx, task.TaskUID, taskPollEvery)
	}

	task, err := m.client.Index(m.uid).UpdateSettings(&meilisearch.Settings{
		SearchableAttributes: []string{"title", "content_text"},
		FilterableAttributes: []string{"user_id", "status"},
	})
	if err != nil {
		return fmt.Errorf("update index settings: %w", err)
	}
	if err := m.waitFor(ctx, task.TaskUID); err != nil {
		return fmt.Errorf("apply index settings: %w", err)
	}
	return nil
}

func (m *Meili) Upsert(ctx context.Context, d Doc) error {
	if err := m.ensure(ctx); err != nil {
		return err
	}
	return m.add(ctx, []Doc{clean(d)})
}

func (m *Meili) Remove(ctx context.Context, id uuid.UUID) error {
	if err := m.ensure(ctx); err != nil {
		return err
	}
	task, err := m.client.Index(m.uid).DeleteDocument(id.String(), nil)
	if err != nil {
		return fmt.Errorf("delete document: %w", err)
	}
	m.remember(task.TaskUID)
	return nil
}

// Search возвращает кандидатов только для одного пользователя: фильтр по user_id обязателен
// и собирается из uuid.UUID, подставить в него постороннюю строку нельзя.
func (m *Meili) Search(ctx context.Context, userID uuid.UUID, query string, limit int) ([]Hit, error) {
	return m.searchWithFilter(ctx, fmt.Sprintf("user_id = %q", userID.String()), query, limit)
}

// SearchPublished ищет по опубликованным материалам всех авторов (поиск библиотеки). Фильтр по статусу
// зашит в код, а перепроверку в Postgres делает вызывающая сторона: индекс мог отстать.
func (m *Meili) SearchPublished(ctx context.Context, query string, limit int) ([]Hit, error) {
	return m.searchWithFilter(ctx, `status = "published"`, query, limit)
}

func (m *Meili) searchWithFilter(ctx context.Context, filter, query string, limit int) ([]Hit, error) {
	if err := m.ensure(ctx); err != nil {
		return nil, err
	}
	resp, err := m.client.Index(m.uid).SearchWithContext(ctx, query, &meilisearch.SearchRequest{
		Limit:                 int64(limit),
		Filter:                filter,
		AttributesToRetrieve:  []string{primaryKey},
		AttributesToCrop:      []string{"content_text"},
		CropLength:            cropLength,
		CropMarker:            "…",
		AttributesToHighlight: []string{"content_text"},
		HighlightPreTag:       markStart,
		HighlightPostTag:      markEnd,
	})
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	hits := make([]Hit, 0, len(resp.Hits))
	for _, raw := range resp.Hits {
		var h struct {
			ID        string `json:"id"`
			Formatted struct {
				ContentText string `json:"content_text"`
			} `json:"_formatted"`
		}
		if err := raw.DecodeInto(&h); err != nil {
			return nil, fmt.Errorf("decode hit: %w", err)
		}
		id, err := uuid.Parse(h.ID)
		if err != nil {
			continue // посторонний документ в индексе: пропускаем, а не падаем
		}
		hits = append(hits, Hit{ID: id, Snippet: h.Formatted.ContentText})
	}
	return hits, nil
}

// Wait ждёт, пока движок обработает все поставленные этим адаптером задачи. Нужен тестам
// и команде пересборки: обычные запросы индексацию не ждут.
func (m *Meili) Wait(ctx context.Context) error {
	uid := m.lastTask.Load()
	if uid == 0 {
		return nil
	}
	return m.waitFor(ctx, uid)
}

// ReplaceAll полностью пересобирает индекс: стирает его и загружает пачки документов от next,
// пока она не вернёт пустой список. Возвращает число загруженных документов.
func (m *Meili) ReplaceAll(ctx context.Context, next func(context.Context) ([]Doc, error)) (int, error) {
	if err := m.ensure(ctx); err != nil {
		return 0, err
	}
	task, err := m.client.Index(m.uid).DeleteAllDocuments(nil)
	if err != nil {
		return 0, fmt.Errorf("clear index: %w", err)
	}
	if err := m.waitFor(ctx, task.TaskUID); err != nil {
		return 0, fmt.Errorf("clear index: %w", err)
	}

	total := 0
	for {
		docs, err := next(ctx)
		if err != nil {
			return total, err
		}
		if len(docs) == 0 {
			return total, nil
		}
		for i := range docs {
			docs[i] = clean(docs[i])
		}
		if err := m.add(ctx, docs); err != nil {
			return total, err
		}
		total += len(docs)
	}
}

func (m *Meili) add(_ context.Context, docs []Doc) error {
	pk := primaryKey
	task, err := m.client.Index(m.uid).AddDocuments(docs, &meilisearch.DocumentOptions{PrimaryKey: &pk})
	if err != nil {
		return fmt.Errorf("add documents: %w", err)
	}
	m.remember(task.TaskUID)
	return nil
}

func (m *Meili) remember(uid int64) {
	for {
		cur := m.lastTask.Load()
		if uid <= cur || m.lastTask.CompareAndSwap(cur, uid) {
			return
		}
	}
}

func (m *Meili) waitFor(ctx context.Context, uid int64) error {
	task, err := m.client.WaitForTaskWithContext(ctx, uid, taskPollEvery)
	if err != nil {
		return err
	}
	if task.Status != meilisearch.TaskStatusSucceeded {
		return fmt.Errorf("task %d ended as %s: %s", uid, task.Status, task.Error.Message)
	}
	return nil
}

// clean убирает из текста символы-маркеры подсветки, чтобы пользователь не мог подделать <mark>.
func clean(d Doc) Doc {
	strip := strings.NewReplacer(markStart, "", markEnd, "")
	d.Title = strip.Replace(d.Title)
	d.ContentText = strip.Replace(d.ContentText)
	return d
}

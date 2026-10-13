package attachment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/storage"
)

const (
	// MaxSize — предельный размер файла, 20 МБ.
	MaxSize = 20 << 20

	linkTTL = 15 * time.Minute
)

var (
	// ErrNotFound возвращается и для чужого файла: снаружи они неотличимы.
	ErrNotFound     = errors.New("attachment not found")
	ErrNoteNotFound = errors.New("note not found")
	// ErrNotUploaded: запись создана, но клиент ещё не загрузил сам файл.
	ErrNotUploaded = errors.New("file has not been uploaded yet")
	ErrInvalid     = errors.New("invalid attachment")
)

// Белый список типов. Нет html и svg: файл, отданный браузеру с таким типом, мог бы выполнить скрипт.
var allowedMIME = map[string]bool{
	"application/pdf": true,
	"image/png":       true,
	"image/jpeg":      true,
	"image/webp":      true,
	"image/gif":       true,
	"text/plain":      true,
	"text/markdown":   true,
}

type Store interface {
	NoteOwnedByUser(ctx context.Context, arg db.NoteOwnedByUserParams) (bool, error)
	CreateAttachment(ctx context.Context, arg db.CreateAttachmentParams) (db.Attachment, error)
	GetAttachment(ctx context.Context, arg db.GetAttachmentParams) (db.Attachment, error)
	ListAttachmentsByNote(ctx context.Context, arg db.ListAttachmentsByNoteParams) ([]db.Attachment, error)
	DeleteAttachment(ctx context.Context, arg db.DeleteAttachmentParams) (db.Attachment, error)
}

type Storage interface {
	PresignPut(ctx context.Context, key, contentType string, size int64, ttl time.Duration) (string, error)
	PresignGet(ctx context.Context, key, fileName string, ttl time.Duration) (string, error)
	Stat(ctx context.Context, key string) (int64, error)
	Remove(ctx context.Context, key string) error
}

type Input struct {
	NoteID    *uuid.UUID
	FileName  string
	MimeType  string
	SizeBytes int64
}

type Upload struct {
	Attachment db.Attachment
	URL        string // presigned PUT: клиент загружает файл сюда
}

type Download struct {
	Attachment db.Attachment
	URL        string // presigned GET
}

type Service struct {
	store   Store
	storage Storage
}

func NewService(store Store, storage Storage) *Service {
	return &Service{store: store, storage: storage}
}

// Create регистрирует файл и выдаёт ссылку для загрузки. Ссылка привязана к заявленным типу и размеру,
// так что залить что-то другое по ней нельзя.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, in Input) (Upload, error) {
	if err := validate(in); err != nil {
		return Upload{}, err
	}
	if in.NoteID != nil {
		owned, err := s.store.NoteOwnedByUser(ctx, db.NoteOwnedByUserParams{ID: *in.NoteID, UserID: userID})
		if err != nil {
			return Upload{}, fmt.Errorf("check note: %w", err)
		}
		if !owned {
			return Upload{}, ErrNoteNotFound
		}
	}

	key := userID.String() + "/" + uuid.NewString()
	url, err := s.storage.PresignPut(ctx, key, in.MimeType, in.SizeBytes, linkTTL)
	if err != nil {
		return Upload{}, fmt.Errorf("presign upload: %w", err)
	}
	row, err := s.store.CreateAttachment(ctx, db.CreateAttachmentParams{
		UserID:    userID,
		NoteID:    toNullUUID(in.NoteID),
		ObjectKey: key,
		FileName:  in.FileName,
		MimeType:  in.MimeType,
		SizeBytes: in.SizeBytes,
	})
	if err != nil {
		return Upload{}, fmt.Errorf("create attachment: %w", err)
	}
	return Upload{Attachment: row, URL: url}, nil
}

func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (Download, error) {
	row, err := s.store.GetAttachment(ctx, db.GetAttachmentParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return Download{}, ErrNotFound
	}
	if err != nil {
		return Download{}, fmt.Errorf("get attachment: %w", err)
	}

	if _, err := s.storage.Stat(ctx, row.ObjectKey); err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return Download{}, ErrNotUploaded
		}
		return Download{}, fmt.Errorf("check stored file: %w", err)
	}
	url, err := s.storage.PresignGet(ctx, row.ObjectKey, row.FileName, linkTTL)
	if err != nil {
		return Download{}, fmt.Errorf("presign download: %w", err)
	}
	return Download{Attachment: row, URL: url}, nil
}

func (s *Service) List(ctx context.Context, userID, noteID uuid.UUID) ([]db.Attachment, error) {
	owned, err := s.store.NoteOwnedByUser(ctx, db.NoteOwnedByUserParams{ID: noteID, UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("check note: %w", err)
	}
	if !owned {
		return nil, ErrNoteNotFound
	}
	rows, err := s.store.ListAttachmentsByNote(ctx, db.ListAttachmentsByNoteParams{NoteID: toNullUUID(&noteID), UserID: userID})
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	if rows == nil {
		rows = []db.Attachment{}
	}
	return rows, nil
}

// Delete сначала удаляет запись, затем объект. Если объект убрать не вышло, остаётся невидимый
// мусор в хранилище, а пользователь своё удаление уже получил, поэтому это только логируется.
func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	row, err := s.store.DeleteAttachment(ctx, db.DeleteAttachmentParams{ID: id, UserID: userID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("delete attachment: %w", err)
	}
	if err := s.storage.Remove(ctx, row.ObjectKey); err != nil {
		slog.Error("attachment: stored file was not removed", "key", row.ObjectKey, "err", err)
	}
	return nil
}

func validate(in Input) error {
	switch {
	case in.FileName == "":
		return fmt.Errorf("%w: file name is required", ErrInvalid)
	case in.SizeBytes < 1 || in.SizeBytes > MaxSize:
		return fmt.Errorf("%w: size must be between 1 byte and 20 MB", ErrInvalid)
	case !allowedMIME[in.MimeType]:
		return fmt.Errorf("%w: file type %q is not allowed", ErrInvalid, in.MimeType)
	}
	return nil
}

func toNullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}

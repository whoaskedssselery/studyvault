package library

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
)

const (
	maxReasonLen       = 1000
	maxSubjectTitleLen = 200

	DecisionApproved = "approved"
	DecisionRejected = "rejected"

	statusDraft   = "draft"
	statusPending = "pending"
)

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Submit отправляет заметку на проверку: из черновика или после отказа. Для отправки нужны название, содержимое
// (текст или хотя бы один файл) и общий предмет библиотеки. Опубликованным материал становится только после одобрения.
func (s *Service) Submit(ctx context.Context, uid, id uuid.UUID) (db.Note, error) {
	n, err := s.store.GetNote(ctx, db.GetNoteParams{ID: id, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Note{}, ErrNotFound
	}
	if err != nil {
		return db.Note{}, fmt.Errorf("get note: %w", err)
	}
	if n.Status != statusDraft && n.Status != statusRejected {
		return db.Note{}, fmt.Errorf("%w: a %s material cannot be submitted", ErrInvalidTransition, n.Status)
	}
	if strings.TrimSpace(n.Title) == "" {
		return db.Note{}, fmt.Errorf("%w: the title is empty", ErrInvalid)
	}
	if !n.LibrarySubjectID.Valid {
		return db.Note{}, fmt.Errorf("%w: choose a library subject (library_subject_id) before submitting", ErrInvalid)
	}
	if strings.TrimSpace(n.ContentText) == "" {
		files, err := s.store.ListNoteAttachmentsAny(ctx, uuid.NullUUID{UUID: id, Valid: true})
		if err != nil {
			return db.Note{}, fmt.Errorf("list attachments: %w", err)
		}
		if len(files) == 0 {
			return db.Note{}, fmt.Errorf("%w: the material has neither text nor files", ErrInvalid)
		}
	}

	submitted, err := s.store.SubmitNote(ctx, db.SubmitNoteParams{ID: id, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) { // статус изменился между проверкой и записью
		return db.Note{}, ErrInvalidTransition
	}
	if err != nil {
		return db.Note{}, fmt.Errorf("submit note: %w", err)
	}
	s.index(ctx, submitted)
	return submitted, nil
}

// Decide фиксирует решение модератора по материалу на проверке. Статус и запись решения меняются одной
// транзакцией. Свой материал модератор рассматривать не может. Если за это время материал уже рассмотрел другой
// модератор или автор его изменил, вернётся ErrInvalidTransition.
func (s *Service) Decide(ctx context.Context, moderatorID, id uuid.UUID, decision, reason string) (db.Note, error) {
	if err := s.requireModerator(ctx, moderatorID); err != nil {
		return db.Note{}, err
	}
	reason = strings.TrimSpace(reason)
	switch decision {
	case DecisionApproved:
		reason = ""
	case DecisionRejected:
		if n := utf8.RuneCountInString(reason); n < 1 || n > maxReasonLen {
			return db.Note{}, fmt.Errorf("%w: a rejection needs a reason of 1 to %d characters", ErrInvalid, maxReasonLen)
		}
	default:
		return db.Note{}, fmt.Errorf("%w: decision must be %q or %q", ErrInvalid, DecisionApproved, DecisionRejected)
	}

	var decided db.Note
	err := s.inTx(ctx, func(st Store) error {
		cur, err := st.GetNoteStatus(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get note status: %w", err)
		}
		if cur.UserID == moderatorID {
			return ErrForbidden
		}
		if cur.Status != statusPending {
			return fmt.Errorf("%w: the material is %s, not pending", ErrInvalidTransition, cur.Status)
		}

		if decision == DecisionApproved {
			decided, err = st.ApproveNote(ctx, id)
		} else {
			decided, err = st.RejectNote(ctx, id)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidTransition
		}
		if err != nil {
			return fmt.Errorf("change status: %w", err)
		}
		return st.InsertModerationDecision(ctx, db.InsertModerationDecisionParams{
			NoteID: id, ModeratorID: uuid.NullUUID{UUID: moderatorID, Valid: true}, Decision: decision, Reason: textOrNull(reason),
		})
	})
	if err != nil {
		return db.Note{}, err
	}
	s.index(ctx, decided)
	return decided, nil
}

type QueueItem struct {
	ID          uuid.UUID
	Title       string
	Summary     *string
	Kind        string
	Semester    *int16
	Subject     *SubjectRef
	Author      Author
	SubmittedAt time.Time
	CreatedAt   time.Time
}

type QueuePage struct {
	Items      []QueueItem
	NextCursor string
}

// Queue отдаёт материалы на проверке, старые сверху. Только для модератора.
func (s *Service) Queue(ctx context.Context, moderatorID uuid.UUID, limit int, cursor string) (QueuePage, error) {
	if err := s.requireModerator(ctx, moderatorID); err != nil {
		return QueuePage{}, err
	}
	limit = clampLimit(limit)
	arg := db.ListPendingMaterialsParams{PageSize: int32(limit + 1)}
	if cursor != "" {
		var c timeCursor
		if err := decodeCursor(cursor, &c); err != nil {
			return QueuePage{}, err
		}
		arg.CursorUpdatedAt = &c.At
		arg.CursorID = uuid.NullUUID{UUID: c.ID, Valid: true}
	}
	rows, err := s.store.ListPendingMaterials(ctx, arg)
	if err != nil {
		return QueuePage{}, fmt.Errorf("list moderation queue: %w", err)
	}
	page := QueuePage{}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[limit-1]
		page.NextCursor = encodeCursor(timeCursor{At: last.UpdatedAt, ID: last.ID})
	}
	page.Items = make([]QueueItem, len(rows))
	for i, r := range rows {
		qi := QueueItem{
			ID: r.ID, Title: r.Title, Summary: fromText(r.Summary), Kind: r.Kind, Semester: fromInt2(r.Semester),
			Author:      Author{ID: r.UserID, DisplayName: r.AuthorName, Faculty: fromText(r.AuthorFaculty)},
			SubmittedAt: r.UpdatedAt, CreatedAt: r.CreatedAt,
		}
		if r.LibrarySubjectID.Valid {
			qi.Subject = &SubjectRef{ID: r.LibrarySubjectID.UUID, Title: r.SubjectTitle.String, Color: fromText(r.SubjectColor)}
		}
		page.Items[i] = qi
	}
	return page, nil
}

// ---------------------------------------------------------------------------
// Общие предметы библиотеки
// ---------------------------------------------------------------------------

type Subject struct {
	ID             uuid.UUID
	Title          string
	Color          *string
	MaterialsCount int64
}

func (s *Service) ListSubjects(ctx context.Context) ([]Subject, error) {
	rows, err := s.store.ListLibrarySubjects(ctx)
	if err != nil {
		return nil, fmt.Errorf("list library subjects: %w", err)
	}
	out := make([]Subject, len(rows))
	for i, r := range rows {
		out[i] = Subject{ID: r.ID, Title: r.Title, Color: fromText(r.Color), MaterialsCount: r.MaterialsCount}
	}
	return out, nil
}

func (s *Service) CreateSubject(ctx context.Context, uid uuid.UUID, title string, color *string) (Subject, error) {
	if err := s.requireModerator(ctx, uid); err != nil {
		return Subject{}, err
	}
	title, err := validSubjectTitle(title)
	if err != nil {
		return Subject{}, err
	}
	if err := validColor(color); err != nil {
		return Subject{}, err
	}
	row, err := s.store.CreateLibrarySubject(ctx, db.CreateLibrarySubjectParams{Title: title, Color: toText(color)})
	if isPgCode(err, pgUniqueViolation) {
		return Subject{}, ErrSubjectExists
	}
	if err != nil {
		return Subject{}, fmt.Errorf("create library subject: %w", err)
	}
	return Subject{ID: row.ID, Title: row.Title, Color: fromText(row.Color)}, nil
}

// SubjectPatch: nil в Title значит «не менять»; цвет можно очистить, поэтому его «передан ли» хранится флагом.
type SubjectPatch struct {
	Title    *string
	SetColor bool
	Color    *string
}

func (s *Service) UpdateSubject(ctx context.Context, uid, id uuid.UUID, p SubjectPatch) (Subject, error) {
	if err := s.requireModerator(ctx, uid); err != nil {
		return Subject{}, err
	}
	arg := db.UpdateLibrarySubjectParams{ID: id, SetColor: p.SetColor, Color: toText(p.Color)}
	if p.Title != nil {
		title, err := validSubjectTitle(*p.Title)
		if err != nil {
			return Subject{}, err
		}
		arg.Title = toText(&title)
	}
	if err := validColor(p.Color); err != nil {
		return Subject{}, err
	}
	row, err := s.store.UpdateLibrarySubject(ctx, arg)
	if errors.Is(err, pgx.ErrNoRows) {
		return Subject{}, ErrNotFound
	}
	if isPgCode(err, pgUniqueViolation) {
		return Subject{}, ErrSubjectExists
	}
	if err != nil {
		return Subject{}, fmt.Errorf("update library subject: %w", err)
	}
	return Subject{ID: row.ID, Title: row.Title, Color: fromText(row.Color)}, nil
}

func (s *Service) DeleteSubject(ctx context.Context, uid, id uuid.UUID) error {
	if err := s.requireModerator(ctx, uid); err != nil {
		return err
	}
	n, err := s.store.DeleteLibrarySubject(ctx, id)
	if isPgCode(err, pgForeignKeyViolation) {
		return ErrSubjectInUse
	}
	if err != nil {
		return fmt.Errorf("delete library subject: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func validSubjectTitle(raw string) (string, error) {
	title := strings.TrimSpace(raw)
	if n := utf8.RuneCountInString(title); n < 1 || n > maxSubjectTitleLen {
		return "", fmt.Errorf("%w: title must be 1 to %d characters", ErrInvalid, maxSubjectTitleLen)
	}
	return title, nil
}

func validColor(c *string) error {
	if c != nil && !colorPattern.MatchString(*c) {
		return fmt.Errorf("%w: color must look like #4f46e5", ErrInvalid)
	}
	return nil
}

func isPgCode(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// Withdraw забирает материал с проверки обратно в черновики. Только автор и только пока материал на проверке:
// после решения модератора отзывать уже нечего, а опубликованный материал автор снимает правкой или удалением.
func (s *Service) Withdraw(ctx context.Context, uid, id uuid.UUID) (db.Note, error) {
	n, err := s.store.WithdrawNote(ctx, db.WithdrawNoteParams{ID: id, UserID: uid})
	if errors.Is(err, pgx.ErrNoRows) {
		cur, getErr := s.store.GetNote(ctx, db.GetNoteParams{ID: id, UserID: uid})
		if errors.Is(getErr, pgx.ErrNoRows) {
			return db.Note{}, ErrNotFound
		}
		if getErr != nil {
			return db.Note{}, fmt.Errorf("get note: %w", getErr)
		}
		return db.Note{}, fmt.Errorf("%w: only a pending material can be withdrawn, this one is %s", ErrInvalidTransition, cur.Status)
	}
	if err != nil {
		return db.Note{}, fmt.Errorf("withdraw note: %w", err)
	}
	s.index(ctx, n)
	return n, nil
}

const DecisionUnpublished = "unpublished"

// Unpublish снимает опубликованный материал с публикации. Он становится отклонённым с причиной: автор видит её,
// исправляет материал и отправляет его на проверку заново. Оценки, комментарии и избранное сохраняются и снова
// видны после повторной публикации. Статус и запись о решении меняются одной транзакцией.
func (s *Service) Unpublish(ctx context.Context, moderatorID, id uuid.UUID, reason string) (db.Note, error) {
	if err := s.requireModerator(ctx, moderatorID); err != nil {
		return db.Note{}, err
	}
	reason = strings.TrimSpace(reason)
	if n := utf8.RuneCountInString(reason); n < 1 || n > maxReasonLen {
		return db.Note{}, fmt.Errorf("%w: taking a material down needs a reason of 1 to %d characters", ErrInvalid, maxReasonLen)
	}

	var down db.Note
	err := s.inTx(ctx, func(st Store) error {
		cur, err := st.GetNoteStatus(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("get note status: %w", err)
		}
		if cur.Status != statusPublished {
			return fmt.Errorf("%w: the material is %s, not published", ErrInvalidTransition, cur.Status)
		}
		down, err = st.UnpublishNote(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) { // автор успел отредактировать материал, и он уже на проверке
			return ErrInvalidTransition
		}
		if err != nil {
			return fmt.Errorf("unpublish note: %w", err)
		}
		return st.InsertModerationDecision(ctx, db.InsertModerationDecisionParams{
			NoteID: id, ModeratorID: uuid.NullUUID{UUID: moderatorID, Valid: true}, Decision: DecisionUnpublished, Reason: textOrNull(reason),
		})
	})
	if err != nil {
		return db.Note{}, err
	}
	s.index(ctx, down)
	return down, nil
}

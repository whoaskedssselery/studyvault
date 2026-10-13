package subject_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/subject"
)

type fakeStore struct {
	err       error
	gotCreate db.CreateSubjectParams
	gotUpdate db.UpdateSubjectParams
	gotDelete db.DeleteSubjectParams
	deleteN   int64
}

func (f *fakeStore) ListSubjects(context.Context, uuid.UUID) ([]db.Subject, error) { return nil, f.err }

func (f *fakeStore) CreateSubject(_ context.Context, a db.CreateSubjectParams) (db.Subject, error) {
	f.gotCreate = a
	return db.Subject{}, f.err
}

func (f *fakeStore) UpdateSubject(_ context.Context, a db.UpdateSubjectParams) (db.Subject, error) {
	f.gotUpdate = a
	return db.Subject{}, f.err
}

func (f *fakeStore) DeleteSubject(_ context.Context, a db.DeleteSubjectParams) (int64, error) {
	f.gotDelete = a
	return f.deleteN, f.err
}

func TestCreate_MapsOptionalFields(t *testing.T) {
	f := &fakeStore{}
	uid := uuid.New()
	color, sem := "#aabbcc", int16(3)

	if _, err := subject.NewService(f).Create(context.Background(), uid, subject.Input{Title: "Math", Color: &color, Semester: &sem}); err != nil {
		t.Fatal(err)
	}
	got := f.gotCreate
	if got.UserID != uid || got.Title != "Math" {
		t.Errorf("bad params: %+v", got)
	}
	if got.Color != (pgtype.Text{String: "#aabbcc", Valid: true}) || got.Semester != (pgtype.Int2{Int16: 3, Valid: true}) {
		t.Errorf("optional fields not mapped: %+v", got)
	}

	if _, err := subject.NewService(f).Create(context.Background(), uid, subject.Input{Title: "Bare"}); err != nil {
		t.Fatal(err)
	}
	if f.gotCreate.Color.Valid || f.gotCreate.Semester.Valid {
		t.Error("absent optionals must be stored as NULL")
	}
}

func TestUpdate_MapsFlagsSoNullCanClearAField(t *testing.T) {
	f := &fakeStore{}
	uid, id := uuid.New(), uuid.New()

	// color: null -> очистить; semester не трогаем; title не трогаем.
	_, err := subject.NewService(f).Update(context.Background(), uid, id, subject.Patch{SetColor: true, Color: nil})
	if err != nil {
		t.Fatal(err)
	}
	got := f.gotUpdate
	if got.ID != id || got.UserID != uid {
		t.Errorf("update must be scoped by id and user: %+v", got)
	}
	if !got.SetColor || got.Color.Valid {
		t.Errorf("color must be cleared: %+v", got)
	}
	if got.SetSemester || got.Title.Valid {
		t.Errorf("untouched fields must not be flagged: %+v", got)
	}
}

func TestStoreNoRows_BecomesErrNotFound(t *testing.T) {
	f := &fakeStore{err: pgx.ErrNoRows}
	svc := subject.NewService(f)
	ctx, uid, id := context.Background(), uuid.New(), uuid.New()

	if _, err := svc.Update(ctx, uid, id, subject.Patch{}); !errors.Is(err, subject.ErrNotFound) {
		t.Errorf("Update: err = %v, want ErrNotFound", err)
	}
}

func TestDelete_ZeroRowsAffected_ReturnsErrNotFound(t *testing.T) {
	f := &fakeStore{deleteN: 0}
	uid, id := uuid.New(), uuid.New()

	err := subject.NewService(f).Delete(context.Background(), uid, id)
	if !errors.Is(err, subject.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if f.gotDelete.ID != id || f.gotDelete.UserID != uid {
		t.Errorf("delete must be scoped by id and user: %+v", f.gotDelete)
	}

	f.deleteN = 1
	if err := subject.NewService(f).Delete(context.Background(), uid, id); err != nil {
		t.Errorf("deleting an existing subject: %v", err)
	}
}

func TestStoreError_IsPropagated(t *testing.T) {
	boom := errors.New("db down")
	svc := subject.NewService(&fakeStore{err: boom})

	if _, err := svc.List(context.Background(), uuid.New()); !errors.Is(err, boom) {
		t.Errorf("List: err = %v, want the store error", err)
	}
	if err := svc.Delete(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, boom) || errors.Is(err, subject.ErrNotFound) {
		t.Errorf("Delete: err = %v, want the store error", err)
	}
}

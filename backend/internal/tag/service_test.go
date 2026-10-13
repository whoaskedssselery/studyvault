package tag_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/limits"
	"github.com/whoaskedssselery/studyvault/backend/internal/tag"
)

type fakeStore struct {
	err        error
	noteOwned  bool
	existing   int64 // сколько из переданных тегов «существует у пользователя»
	deleteRows int64

	replaced   *db.ReplaceNoteTagsParams
	countedFor []uuid.UUID

	userTags int64 // сколько тегов уже есть у пользователя
	countErr error
}

func (f *fakeStore) CountTagsByUser(context.Context, uuid.UUID) (int64, error) {
	return f.userTags, f.countErr
}

func (f *fakeStore) ListTags(context.Context, uuid.UUID) ([]db.Tag, error) { return nil, f.err }

func (f *fakeStore) CreateTag(_ context.Context, a db.CreateTagParams) (db.Tag, error) {
	return db.Tag{ID: uuid.New(), UserID: a.UserID, Name: a.Name}, f.err
}

func (f *fakeStore) DeleteTag(context.Context, db.DeleteTagParams) (int64, error) {
	return f.deleteRows, f.err
}

func (f *fakeStore) NoteOwnedByUser(context.Context, db.NoteOwnedByUserParams) (bool, error) {
	return f.noteOwned, f.err
}

func (f *fakeStore) CountUserTags(_ context.Context, a db.CountUserTagsParams) (int64, error) {
	f.countedFor = a.TagIds
	return f.existing, f.err
}

func (f *fakeStore) ReplaceNoteTags(_ context.Context, a db.ReplaceNoteTagsParams) error {
	f.replaced = &a
	return f.err
}

func TestCreate_DuplicateName_ReturnsErrNameTaken(t *testing.T) {
	f := &fakeStore{err: &pgconn.PgError{Code: "23505"}}

	_, err := tag.NewService(f).Create(context.Background(), uuid.New(), "exam")

	if !errors.Is(err, tag.ErrNameTaken) {
		t.Fatalf("err = %v, want ErrNameTaken", err)
	}
}

func TestDelete_ZeroRows_ReturnsErrNotFound(t *testing.T) {
	err := tag.NewService(&fakeStore{}).Delete(context.Background(), uuid.New(), uuid.New())

	if !errors.Is(err, tag.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestSetNoteTags_NoteNotOwned_WritesNothing(t *testing.T) {
	f := &fakeStore{noteOwned: false, existing: 1}

	err := tag.NewService(f).SetNoteTags(context.Background(), uuid.New(), uuid.New(), []uuid.UUID{uuid.New()})

	if !errors.Is(err, tag.ErrNoteNotFound) {
		t.Fatalf("err = %v, want ErrNoteNotFound", err)
	}
	if f.replaced != nil {
		t.Error("nothing may be written for a foreign note")
	}
}

func TestSetNoteTags_UnknownOrForeignTag_WritesNothing(t *testing.T) {
	f := &fakeStore{noteOwned: true, existing: 1} // из двух запрошенных у пользователя есть один
	ids := []uuid.UUID{uuid.New(), uuid.New()}

	err := tag.NewService(f).SetNoteTags(context.Background(), uuid.New(), uuid.New(), ids)

	if !errors.Is(err, tag.ErrInvalidTag) {
		t.Fatalf("err = %v, want ErrInvalidTag", err)
	}
	if f.replaced != nil {
		t.Error("the set must be replaced entirely or not at all")
	}
}

func TestSetNoteTags_DeduplicatesIDs(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	f := &fakeStore{noteOwned: true, existing: 2}
	uid, nid := uuid.New(), uuid.New()

	if err := tag.NewService(f).SetNoteTags(context.Background(), uid, nid, []uuid.UUID{a, b, a, b, a}); err != nil {
		t.Fatalf("duplicates must not fail the request: %v", err)
	}
	if len(f.countedFor) != 2 || len(f.replaced.TagIds) != 2 {
		t.Errorf("ids must be deduplicated: counted %d, replaced %d", len(f.countedFor), len(f.replaced.TagIds))
	}
	if f.replaced.NoteID != nid || f.replaced.UserID != uid {
		t.Errorf("replace must be scoped by note and user: %+v", f.replaced)
	}
}

func TestSetNoteTags_EmptyList_ClearsTags(t *testing.T) {
	f := &fakeStore{noteOwned: true}

	if err := tag.NewService(f).SetNoteTags(context.Background(), uuid.New(), uuid.New(), nil); err != nil {
		t.Fatal(err)
	}
	if f.replaced == nil || f.replaced.TagIds == nil || len(f.replaced.TagIds) != 0 {
		t.Errorf("an empty set must be sent as an empty (non-nil) array: %#v", f.replaced)
	}
}

func TestStoreError_IsPropagated(t *testing.T) {
	boom := errors.New("db down")
	svc := tag.NewService(&fakeStore{err: boom})

	if _, err := svc.List(context.Background(), uuid.New()); !errors.Is(err, boom) {
		t.Errorf("List: err = %v", err)
	}
	if err := svc.SetNoteTags(context.Background(), uuid.New(), uuid.New(), nil); !errors.Is(err, boom) {
		t.Errorf("SetNoteTags: err = %v", err)
	}
}

func TestCreate_AtTheQuota_IsRefused(t *testing.T) {
	_, err := tag.NewService(&fakeStore{userTags: limits.MaxTagsPerUser}).Create(context.Background(), uuid.New(), "one-too-many")
	if !errors.Is(err, limits.ErrReached) {
		t.Fatalf("err = %v, want limits.ErrReached", err)
	}
}

func TestCreate_JustBelowTheQuota_Succeeds(t *testing.T) {
	if _, err := tag.NewService(&fakeStore{userTags: limits.MaxTagsPerUser - 1}).Create(context.Background(), uuid.New(), "last"); err != nil {
		t.Fatalf("the last allowed tag must be accepted: %v", err)
	}
}

func TestCreate_CountFailure_IsReturned(t *testing.T) {
	if _, err := tag.NewService(&fakeStore{countErr: errors.New("db down")}).Create(context.Background(), uuid.New(), "x"); err == nil {
		t.Fatal("a failing count must not be treated as zero")
	}
}

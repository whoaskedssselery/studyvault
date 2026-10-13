package attachment_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/whoaskedssselery/studyvault/backend/internal/attachment"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/storage"
)

// memStorage — хранилище в памяти: «загрузить» файл значит положить ключ в objects.
type memStorage struct {
	objects   map[string]int64
	putCalls  []putCall
	removed   []string
	statErr   error
	removeErr error
}

type putCall struct {
	key, mime string
	size      int64
	ttl       time.Duration
}

func newMemStorage() *memStorage { return &memStorage{objects: map[string]int64{}} }

func (m *memStorage) PresignPut(_ context.Context, key, mime string, size int64, ttl time.Duration) (string, error) {
	m.putCalls = append(m.putCalls, putCall{key, mime, size, ttl})
	return "https://s3.test/put/" + key, nil
}

func (m *memStorage) PresignGet(_ context.Context, key, name string, _ time.Duration) (string, error) {
	return "https://s3.test/get/" + key + "?name=" + name, nil
}

func (m *memStorage) Stat(_ context.Context, key string) (int64, error) {
	if m.statErr != nil {
		return 0, m.statErr
	}
	size, ok := m.objects[key]
	if !ok {
		return 0, storage.ErrNotFound
	}
	return size, nil
}

func (m *memStorage) Remove(_ context.Context, key string) error {
	m.removed = append(m.removed, key)
	delete(m.objects, key)
	return m.removeErr
}

type fakeStore struct {
	noteOwned bool
	rows      map[uuid.UUID]db.Attachment
	created   []db.CreateAttachmentParams
	listed    []db.Attachment
	err       error
}

func newFakeStore() *fakeStore { return &fakeStore{rows: map[uuid.UUID]db.Attachment{}} }

func (f *fakeStore) NoteOwnedByUser(context.Context, db.NoteOwnedByUserParams) (bool, error) {
	return f.noteOwned, f.err
}

func (f *fakeStore) CreateAttachment(_ context.Context, a db.CreateAttachmentParams) (db.Attachment, error) {
	if f.err != nil {
		return db.Attachment{}, f.err
	}
	f.created = append(f.created, a)
	row := db.Attachment{ID: uuid.New(), UserID: a.UserID, NoteID: a.NoteID, ObjectKey: a.ObjectKey, FileName: a.FileName, MimeType: a.MimeType, SizeBytes: a.SizeBytes}
	f.rows[row.ID] = row
	return row, nil
}

func (f *fakeStore) GetAttachment(_ context.Context, a db.GetAttachmentParams) (db.Attachment, error) {
	row, ok := f.rows[a.ID]
	if !ok || row.UserID != a.UserID {
		return db.Attachment{}, pgx.ErrNoRows
	}
	return row, f.err
}

func (f *fakeStore) ListAttachmentsByNote(context.Context, db.ListAttachmentsByNoteParams) ([]db.Attachment, error) {
	return f.listed, f.err
}

func (f *fakeStore) DeleteAttachment(_ context.Context, a db.DeleteAttachmentParams) (db.Attachment, error) {
	row, ok := f.rows[a.ID]
	if !ok || row.UserID != a.UserID {
		return db.Attachment{}, pgx.ErrNoRows
	}
	delete(f.rows, a.ID)
	return row, f.err
}

func valid() attachment.Input {
	return attachment.Input{FileName: "lecture.pdf", MimeType: "application/pdf", SizeBytes: 1234}
}

func TestCreate_PresignsExactlyWhatWasDeclared(t *testing.T) {
	f, st := newFakeStore(), newMemStorage()
	uid := uuid.New()

	up, err := attachment.NewService(f, st).Create(context.Background(), uid, valid())
	if err != nil {
		t.Fatal(err)
	}

	if len(st.putCalls) != 1 {
		t.Fatalf("presign calls = %d", len(st.putCalls))
	}
	p := st.putCalls[0]
	if p.mime != "application/pdf" || p.size != 1234 || p.ttl <= 0 || p.ttl > time.Hour {
		t.Errorf("presign: %+v (the link must be bound to the declared type and size and be short-lived)", p)
	}
	user, rest, ok := strings.Cut(p.key, "/")
	if !ok || user != uid.String() {
		t.Errorf("key %q must start with the owner's id", p.key)
	}
	if _, err := uuid.Parse(rest); err != nil {
		t.Errorf("the key's second part must be a random uuid, got %q", rest)
	}
	if up.URL != "https://s3.test/put/"+p.key || up.Attachment.ObjectKey != p.key {
		t.Errorf("upload: %+v", up)
	}
	if len(f.created) != 1 || f.created[0].UserID != uid || f.created[0].SizeBytes != 1234 {
		t.Errorf("row: %+v", f.created)
	}
}

func TestCreate_EachUploadGetsItsOwnKey(t *testing.T) {
	f, st := newFakeStore(), newMemStorage()
	svc := attachment.NewService(f, st)
	a, _ := svc.Create(context.Background(), uuid.New(), valid())
	b, _ := svc.Create(context.Background(), uuid.New(), valid())

	if a.Attachment.ObjectKey == b.Attachment.ObjectKey {
		t.Error("object keys must never collide")
	}
}

func TestCreate_InvalidInput_IsRejectedBeforeAnythingIsWritten(t *testing.T) {
	cases := map[string]attachment.Input{
		"zero size":           {FileName: "a.pdf", MimeType: "application/pdf", SizeBytes: 0},
		"negative size":       {FileName: "a.pdf", MimeType: "application/pdf", SizeBytes: -1},
		"over the limit":      {FileName: "a.pdf", MimeType: "application/pdf", SizeBytes: attachment.MaxSize + 1},
		"html is not allowed": {FileName: "a.html", MimeType: "text/html", SizeBytes: 10},
		"svg is not allowed":  {FileName: "a.svg", MimeType: "image/svg+xml", SizeBytes: 10},
		"executable":          {FileName: "a.exe", MimeType: "application/x-msdownload", SizeBytes: 10},
		"empty mime":          {FileName: "a.pdf", MimeType: "", SizeBytes: 10},
		"mime with params":    {FileName: "a.txt", MimeType: "text/plain; charset=utf-8", SizeBytes: 10},
		"empty name":          {FileName: "", MimeType: "application/pdf", SizeBytes: 10},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			f, st := newFakeStore(), newMemStorage()

			_, err := attachment.NewService(f, st).Create(context.Background(), uuid.New(), in)

			if !errors.Is(err, attachment.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if len(f.created) != 0 || len(st.putCalls) != 0 {
				t.Error("nothing may be written or presigned for invalid input")
			}
		})
	}
}

func TestCreate_Limits_AreInclusive(t *testing.T) {
	for _, size := range []int64{1, attachment.MaxSize} {
		in := valid()
		in.SizeBytes = size
		if _, err := attachment.NewService(newFakeStore(), newMemStorage()).Create(context.Background(), uuid.New(), in); err != nil {
			t.Errorf("size %d must be accepted: %v", size, err)
		}
	}
}

func TestCreate_AllowedTypes(t *testing.T) {
	for _, m := range []string{"application/pdf", "image/png", "image/jpeg", "image/webp", "image/gif", "text/plain", "text/markdown"} {
		in := valid()
		in.MimeType = m
		if _, err := attachment.NewService(newFakeStore(), newMemStorage()).Create(context.Background(), uuid.New(), in); err != nil {
			t.Errorf("%s must be accepted: %v", m, err)
		}
	}
}

func TestCreate_ForNote_RequiresOwnership(t *testing.T) {
	f, st := newFakeStore(), newMemStorage()
	nid := uuid.New()
	in := valid()
	in.NoteID = &nid

	_, err := attachment.NewService(f, st).Create(context.Background(), uuid.New(), in)

	if !errors.Is(err, attachment.ErrNoteNotFound) {
		t.Fatalf("err = %v, want ErrNoteNotFound", err)
	}
	if len(f.created) != 0 || len(st.putCalls) != 0 {
		t.Error("nothing may be written for a foreign note")
	}

	f.noteOwned = true
	if _, err := attachment.NewService(f, st).Create(context.Background(), uuid.New(), in); err != nil {
		t.Errorf("an owned note must be accepted: %v", err)
	}
	if !f.created[0].NoteID.Valid || f.created[0].NoteID.UUID != nid {
		t.Errorf("the note id must be stored: %+v", f.created[0].NoteID)
	}
}

func TestGet_BeforeTheUpload_ReturnsErrNotUploaded(t *testing.T) {
	f, st := newFakeStore(), newMemStorage()
	uid := uuid.New()
	svc := attachment.NewService(f, st)
	up, _ := svc.Create(context.Background(), uid, valid())

	_, err := svc.Get(context.Background(), uid, up.Attachment.ID)
	if !errors.Is(err, attachment.ErrNotUploaded) {
		t.Fatalf("err = %v, want ErrNotUploaded", err)
	}

	st.objects[up.Attachment.ObjectKey] = 1234 // клиент загрузил файл
	dl, err := svc.Get(context.Background(), uid, up.Attachment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dl.URL, up.Attachment.ObjectKey) || !strings.Contains(dl.URL, "lecture.pdf") {
		t.Errorf("download link: %q", dl.URL)
	}
}

func TestGet_StorageOutage_IsNotReportedAsNotUploaded(t *testing.T) {
	f, st := newFakeStore(), newMemStorage()
	uid := uuid.New()
	svc := attachment.NewService(f, st)
	up, _ := svc.Create(context.Background(), uid, valid())
	st.statErr = errors.New("connection refused")

	_, err := svc.Get(context.Background(), uid, up.Attachment.ID)

	if err == nil || errors.Is(err, attachment.ErrNotUploaded) || errors.Is(err, attachment.ErrNotFound) {
		t.Fatalf("err = %v: an outage is an internal error, not 'not uploaded'", err)
	}
}

func TestGet_AnotherUsersAttachment_IsNotFound(t *testing.T) {
	f, st := newFakeStore(), newMemStorage()
	svc := attachment.NewService(f, st)
	up, _ := svc.Create(context.Background(), uuid.New(), valid())

	_, err := svc.Get(context.Background(), uuid.New(), up.Attachment.ID)

	if !errors.Is(err, attachment.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDelete_RemovesTheRowAndTheObject(t *testing.T) {
	f, st := newFakeStore(), newMemStorage()
	uid := uuid.New()
	svc := attachment.NewService(f, st)
	up, _ := svc.Create(context.Background(), uid, valid())
	st.objects[up.Attachment.ObjectKey] = 1234

	if err := svc.Delete(context.Background(), uid, up.Attachment.ID); err != nil {
		t.Fatal(err)
	}

	if len(f.rows) != 0 {
		t.Error("the row must be deleted")
	}
	if len(st.removed) != 1 || st.removed[0] != up.Attachment.ObjectKey {
		t.Errorf("the object must be removed from the storage: %v", st.removed)
	}
}

func TestDelete_StorageFailure_StillDeletesTheRecord(t *testing.T) {
	f, st := newFakeStore(), newMemStorage()
	uid := uuid.New()
	svc := attachment.NewService(f, st)
	up, _ := svc.Create(context.Background(), uid, valid())
	st.removeErr = errors.New("storage down")

	if err := svc.Delete(context.Background(), uid, up.Attachment.ID); err != nil {
		t.Fatalf("a leftover object is harmless garbage, the user's delete must succeed: %v", err)
	}
	if len(f.rows) != 0 {
		t.Error("the row must be gone")
	}
}

func TestDelete_AnotherUsersAttachment_ChangesNothing(t *testing.T) {
	f, st := newFakeStore(), newMemStorage()
	svc := attachment.NewService(f, st)
	up, _ := svc.Create(context.Background(), uuid.New(), valid())
	st.objects[up.Attachment.ObjectKey] = 1234

	err := svc.Delete(context.Background(), uuid.New(), up.Attachment.ID)

	if !errors.Is(err, attachment.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if len(f.rows) != 1 || len(st.removed) != 0 || len(st.objects) != 1 {
		t.Error("a stranger's delete must touch neither the row nor the file")
	}
}

func TestList_RequiresANoteOfTheUser(t *testing.T) {
	f := newFakeStore()
	f.listed = []db.Attachment{{ID: uuid.New()}}
	svc := attachment.NewService(f, newMemStorage())

	if _, err := svc.List(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, attachment.ErrNoteNotFound) {
		t.Fatalf("a foreign note: err = %v, want ErrNoteNotFound", err)
	}

	f.noteOwned = true
	got, err := svc.List(context.Background(), uuid.New(), uuid.New())
	if err != nil || len(got) != 1 {
		t.Errorf("got %v, err %v", got, err)
	}
}

func TestList_EmptyIsNonNil(t *testing.T) {
	f := newFakeStore()
	f.noteOwned = true

	got, _ := attachment.NewService(f, newMemStorage()).List(context.Background(), uuid.New(), uuid.New())

	if got == nil {
		t.Error("an empty list must be an empty slice, not nil")
	}
}

func TestStoreError_IsPropagated(t *testing.T) {
	boom := fmt.Errorf("db down")
	f := newFakeStore()
	f.err = boom

	if _, err := attachment.NewService(f, newMemStorage()).Create(context.Background(), uuid.New(), valid()); !errors.Is(err, boom) {
		t.Errorf("Create: err = %v", err)
	}
}

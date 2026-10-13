package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/whoaskedssselery/studyvault/backend/internal/note"
)

func (f *fakeNotes) Backlinks(_ context.Context, uid, id uuid.UUID) ([]note.Summary, error) {
	f.calls++
	f.gotUser, f.gotID = uid, id
	return f.backlinks, f.err
}

func (f *fakeNotes) Graph(_ context.Context, uid uuid.UUID) (note.Graph, error) {
	f.calls++
	f.gotUser = uid
	return f.graph, f.err
}

func TestBacklinks_Empty_ReturnsEmptyArray(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	id := uuid.New()
	rec := e.do(http.MethodGet, "/notes/"+id.String()+"/backlinks", "")

	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"items":[]}` {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if e.f.gotID != id || e.f.gotUser != e.uid {
		t.Error("service must get the path id and the token's user id")
	}
}

func TestBacklinks_ReturnsSummariesWithoutContent(t *testing.T) {
	n := sampleNote()
	e := newNotesEnv(&fakeNotes{backlinks: []note.Summary{n.Summary}})
	rec := e.do(http.MethodGet, "/notes/"+uuid.NewString()+"/backlinks", "")

	item := decode(t, rec)["items"].([]any)[0].(map[string]any)
	if item["title"] != "Sample" || len(item["tags"].([]any)) != 1 {
		t.Errorf("item: %v", item)
	}
	if _, has := item["content"]; has {
		t.Error("backlinks must not carry the document")
	}
	if v, present := item["subject_id"]; !present || v != nil {
		t.Errorf("subject_id must be present and null, got %v", v)
	}
}

func TestBacklinks_NotFound_And_BadUUID(t *testing.T) {
	e := newNotesEnv(&fakeNotes{err: note.ErrNotFound})
	if rec := e.do(http.MethodGet, "/notes/"+uuid.NewString()+"/backlinks", ""); rec.Code != http.StatusNotFound || errorCode(t, rec) != "not_found" {
		t.Errorf("missing note: status = %d, body = %s", rec.Code, rec.Body)
	}

	e = newNotesEnv(&fakeNotes{})
	if rec := e.do(http.MethodGet, "/notes/nope/backlinks", ""); rec.Code != http.StatusBadRequest || e.f.calls != 0 {
		t.Errorf("bad uuid: status = %d, calls = %d", rec.Code, e.f.calls)
	}
}

func TestGraph_Empty_ReturnsEmptyArraysNotNull(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	rec := e.do(http.MethodGet, "/graph", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := decode(t, rec)
	if nodes, ok := body["nodes"].([]any); !ok || len(nodes) != 0 {
		t.Errorf("nodes must be an empty array, got %v", body["nodes"])
	}
	if edges, ok := body["edges"].([]any); !ok || len(edges) != 0 {
		t.Errorf("edges must be an empty array, got %v", body["edges"])
	}
	if e.f.gotUser != e.uid {
		t.Error("service must receive the user id from the token")
	}
}

func TestGraph_MapsNodesAndEdges(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	e := newNotesEnv(&fakeNotes{graph: note.Graph{
		Nodes: []note.GraphNode{{ID: a, Title: "A"}, {ID: b, Title: "B"}},
		Edges: []note.GraphEdge{{Source: a, Target: b}},
	}})

	body := decode(t, e.do(http.MethodGet, "/graph", ""))

	nodes, edges := body["nodes"].([]any), body["edges"].([]any)
	if len(nodes) != 2 || nodes[1].(map[string]any)["title"] != "B" || nodes[0].(map[string]any)["id"] != a.String() {
		t.Errorf("nodes: %v", nodes)
	}
	edge := edges[0].(map[string]any)
	if len(edges) != 1 || edge["source"] != a.String() || edge["target"] != b.String() {
		t.Errorf("edges: %v", edges)
	}
}

func TestLinks_ServiceError_Returns500WithoutLeak(t *testing.T) {
	e := newNotesEnv(&fakeNotes{err: errors.New("pq: secret internal detail")})

	for _, path := range []string{"/graph", "/notes/" + uuid.NewString() + "/backlinks"} {
		rec := e.do(http.MethodGet, path, "")
		if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "secret internal detail") {
			t.Errorf("%s: status = %d, body = %s", path, rec.Code, rec.Body)
		}
	}
}

func TestLinks_RequireAuth(t *testing.T) {
	e := newNotesEnv(&fakeNotes{})
	e.auth = ""

	for _, path := range []string{"/graph", "/notes/" + uuid.NewString() + "/backlinks"} {
		if rec := e.do(http.MethodGet, path, ""); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, rec.Code)
		}
	}
	if e.f.calls != 0 {
		t.Error("service must not be called without auth")
	}
}

package httpapi

import "net/http"

// Публикация и модерация: отправка на проверку, отзыв, решение, снятие с публикации и очередь.

func (a *api) SubmitNote(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	if _, err := a.Library.Submit(r.Context(), uid, id); err != nil {
		libraryError(w, r, err, "submit note")
		return
	}
	// Ответ собирает обычный сервис заметок: так автор видит ровно то, что вернул бы GET /notes/{id}.
	found, err := a.Notes.Get(r.Context(), uid, id)
	a.respondNote(w, r, http.StatusOK, found, err, "submit note")
}

func (a *api) ListModerationQueue(w http.ResponseWriter, r *http.Request, params ListModerationQueueParams) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	limit, cursor, ok := pagingParams(w, params.Limit, params.Cursor)
	if !ok {
		return
	}
	page, err := a.Library.Queue(r.Context(), uid, limit, cursor)
	if err != nil {
		libraryError(w, r, err, "list moderation queue")
		return
	}
	items := make([]QueueItem, len(page.Items))
	for i, q := range page.Items {
		items[i] = QueueItem{
			Id: q.ID, Title: q.Title, Summary: nullableOf(q.Summary), Kind: MaterialKind(q.Kind),
			Semester: nullableInt(q.Semester), Subject: nullableSubjectRef(q.Subject), Author: toAuthor(q.Author),
			SubmittedAt: q.SubmittedAt, CreatedAt: q.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, QueuePage{Items: items, NextCursor: nextCursor(page.NextCursor)})
}

func (a *api) DecideMaterial(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req DecisionInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	reason := ""
	if req.Reason != nil {
		reason = *req.Reason
	}
	decided, err := a.Library.Decide(r.Context(), uid, id, string(req.Decision), reason)
	if err != nil {
		libraryError(w, r, err, "decide material")
		return
	}
	writeJSON(w, http.StatusOK, DecisionResult{Id: decided.ID, Status: MaterialStatus(decided.Status)})
}

func (a *api) WithdrawNote(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	if _, err := a.Library.Withdraw(r.Context(), uid, id); err != nil {
		libraryError(w, r, err, "withdraw note")
		return
	}
	found, err := a.Notes.Get(r.Context(), uid, id)
	a.respondNote(w, r, http.StatusOK, found, err, "withdraw note")
}

func (a *api) UnpublishMaterial(w http.ResponseWriter, r *http.Request, id Id) {
	uid, ok := currentUser(w, r)
	if !ok {
		return
	}
	var req UnpublishInput
	if err := decodeJSON(w, r, &req); err != nil {
		badRequest(w, "invalid request body: "+err.Error())
		return
	}
	down, err := a.Library.Unpublish(r.Context(), uid, id, req.Reason)
	if err != nil {
		libraryError(w, r, err, "unpublish material")
		return
	}
	writeJSON(w, http.StatusOK, DecisionResult{Id: down.ID, Status: MaterialStatus(down.Status)})
}

package task

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type fakeRepository struct {
	tasks   []Task
	nextID  int64
	listErr error
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1}
}

func (f *fakeRepository) seed(tasks ...Task) {
	for _, t := range tasks {
		f.tasks = append(f.tasks, t)
		if t.ID >= f.nextID {
			f.nextID = t.ID + 1
		}
	}
}

func (f *fakeRepository) List(context.Context) ([]Task, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]Task, len(f.tasks))
	copy(out, f.tasks)
	return out, nil
}

func (f *fakeRepository) Get(_ context.Context, id int64) (Task, error) {
	for _, t := range f.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return Task{}, ErrNotFound
}

func (f *fakeRepository) Create(_ context.Context, in CreateInput) (Task, error) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t := Task{
		ID:          f.nextID,
		Title:       in.Title,
		Description: in.Description,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	f.nextID++
	f.tasks = append(f.tasks, t)
	return t, nil
}

func (f *fakeRepository) Update(_ context.Context, t Task) (Task, error) {
	for i := range f.tasks {
		if f.tasks[i].ID == t.ID {
			t.CreatedAt = f.tasks[i].CreatedAt
			t.UpdatedAt = f.tasks[i].UpdatedAt.Add(time.Minute)
			f.tasks[i] = t
			return t, nil
		}
	}
	return Task{}, ErrNotFound
}

func (f *fakeRepository) Delete(_ context.Context, id int64) error {
	for i := range f.tasks {
		if f.tasks[i].ID == id {
			f.tasks = append(f.tasks[:i], f.tasks[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func newTestRouter(repo Repository) *gin.Engine {
	router := gin.New()
	handler := NewHandler(repo)
	handler.RegisterRoutes(router)
	handler.RegisterRoutes(router.Group("/api/v1"))
	return router
}

func performRequest(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response %q: %v", rec.Body.String(), err)
	}
	return body
}

type errorResponse struct {
	Error struct {
		Code    string            `json:"code"`
		Message string            `json:"message"`
		Fields  map[string]string `json:"fields"`
	} `json:"error"`
}

func TestListTasks(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(
		Task{ID: 1, Title: "first", CreatedAt: time.Now()},
		Task{ID: 2, Title: "second", CreatedAt: time.Now()},
	)
	router := newTestRouter(repo)

	rec := performRequest(router, http.MethodGet, "/tasks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got []Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(tasks) = %d, want 2", len(got))
	}
}

func TestListTasksEmptyReturnsArray(t *testing.T) {
	router := newTestRouter(newFakeRepository())

	rec := performRequest(router, http.MethodGet, "/tasks", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if body := strings.TrimSpace(rec.Body.String()); body != "[]" {
		t.Fatalf("body = %q, want []", body)
	}
}

func TestListTasksInternalError(t *testing.T) {
	repo := newFakeRepository()
	repo.listErr = fmt.Errorf("database unavailable")
	router := newTestRouter(repo)

	rec := performRequest(router, http.MethodGet, "/tasks", "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Error.Code != "internal_error" {
		t.Errorf("code = %q, want internal_error", body.Error.Code)
	}
}

func TestCreateTask(t *testing.T) {
	router := newTestRouter(newFakeRepository())

	rec := performRequest(router, http.MethodPost, "/tasks",
		`{"title":"  Buy milk  ","description":"2 liters"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/tasks/1" {
		t.Errorf("Location = %q, want /tasks/1", loc)
	}

	var got Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("ID = %d, want 1", got.ID)
	}
	if got.Title != "Buy milk" {
		t.Errorf("Title = %q, want trimmed title", got.Title)
	}
	if got.Description != "2 liters" {
		t.Errorf("Description = %q, want 2 liters", got.Description)
	}
	if got.Completed {
		t.Error("Completed = true, want false")
	}
}

func TestCreateTaskValidation(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		wantField string
	}{
		{name: "missing title", body: `{}`, wantField: "title"},
		{name: "blank title", body: `{"title":"   "}`, wantField: "title"},
		{name: "title too long", body: `{"title":"` + strings.Repeat("a", MaxTitleLength+1) + `"}`, wantField: "title"},
		{name: "description too long", body: `{"title":"ok","description":"` + strings.Repeat("a", MaxDescriptionLength+1) + `"}`, wantField: "description"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(newFakeRepository())
			rec := performRequest(router, http.MethodPost, "/tasks", tt.body)

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
			}
			body := decodeError(t, rec)
			if body.Error.Code != "validation_error" {
				t.Errorf("code = %q, want validation_error", body.Error.Code)
			}
			if _, ok := body.Error.Fields[tt.wantField]; !ok {
				t.Errorf("fields = %v, want key %q", body.Error.Fields, tt.wantField)
			}
		})
	}
}

func TestCreateTaskInvalidJSON(t *testing.T) {
	router := newTestRouter(newFakeRepository())

	rec := performRequest(router, http.MethodPost, "/tasks", `{"title":`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Error.Code != "invalid_json" {
		t.Errorf("code = %q, want invalid_json", body.Error.Code)
	}
}

func TestGetTask(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(Task{ID: 42, Title: "answer"})
	router := newTestRouter(repo)

	rec := performRequest(router, http.MethodGet, "/tasks/42", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID != 42 || got.Title != "answer" {
		t.Errorf("task = %+v, want id 42 title answer", got)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	router := newTestRouter(newFakeRepository())

	rec := performRequest(router, http.MethodGet, "/tasks/999", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	body := decodeError(t, rec)
	if body.Error.Code != "not_found" {
		t.Errorf("code = %q, want not_found", body.Error.Code)
	}
}

func TestGetTaskInvalidID(t *testing.T) {
	tests := []string{"/tasks/abc", "/tasks/0", "/tasks/-1"}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			router := newTestRouter(newFakeRepository())
			rec := performRequest(router, http.MethodGet, path, "")
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			body := decodeError(t, rec)
			if body.Error.Code != "invalid_id" {
				t.Errorf("code = %q, want invalid_id", body.Error.Code)
			}
		})
	}
}

func TestAPIAlias(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(Task{ID: 1, Title: "aliased"})
	router := newTestRouter(repo)

	rec := performRequest(router, http.MethodGet, "/api/v1/tasks/1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateTask(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(Task{ID: 1, Title: "old", Description: "keep me"})
	router := newTestRouter(repo)

	rec := performRequest(router, http.MethodPut, "/tasks/1",
		`{"title":"  new title  ","completed":true}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var got Task
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Title != "new title" {
		t.Errorf("Title = %q, want new title", got.Title)
	}
	if got.Description != "keep me" {
		t.Errorf("Description = %q, want unchanged", got.Description)
	}
	if !got.Completed {
		t.Error("Completed = false, want true")
	}
	if got.UpdatedAt.Equal(got.CreatedAt) {
		t.Error("UpdatedAt was not refreshed")
	}
}

func TestUpdateTaskValidation(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(Task{ID: 1, Title: "old"})
	router := newTestRouter(repo)

	rec := performRequest(router, http.MethodPut, "/tasks/1", `{"title":"   "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	body := decodeError(t, rec)
	if _, ok := body.Error.Fields["title"]; !ok {
		t.Errorf("fields = %v, want title", body.Error.Fields)
	}
}

func TestUpdateTaskNotFound(t *testing.T) {
	router := newTestRouter(newFakeRepository())

	rec := performRequest(router, http.MethodPut, "/tasks/99", `{"completed":true}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteTask(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(Task{ID: 7, Title: "delete me"})
	router := newTestRouter(repo)

	rec := performRequest(router, http.MethodDelete, "/tasks/7", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", rec.Body.String())
	}

	if _, err := repo.Get(context.Background(), 7); err != ErrNotFound {
		t.Errorf("Get after delete error = %v, want ErrNotFound", err)
	}
}

func TestDeleteTaskNotFound(t *testing.T) {
	router := newTestRouter(newFakeRepository())

	rec := performRequest(router, http.MethodDelete, "/tasks/404", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

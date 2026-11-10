package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/h4r5h6l/kubernetes_demo/internal/task"
)

func init() {
	gin.SetMode(gin.TestMode)
}

type fakeRepository struct {
	tasks  []task.Task
	nextID int64
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{nextID: 1}
}

func (f *fakeRepository) seed(tasks ...task.Task) {
	for _, t := range tasks {
		f.tasks = append(f.tasks, t)
		if t.ID >= f.nextID {
			f.nextID = t.ID + 1
		}
	}
}

func (f *fakeRepository) List(context.Context) ([]task.Task, error) {
	out := make([]task.Task, len(f.tasks))
	copy(out, f.tasks)
	return out, nil
}

func (f *fakeRepository) Get(_ context.Context, id int64) (task.Task, error) {
	for _, t := range f.tasks {
		if t.ID == id {
			return t, nil
		}
	}
	return task.Task{}, task.ErrNotFound
}

func (f *fakeRepository) Create(_ context.Context, in task.CreateInput) (task.Task, error) {
	now := time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC)
	t := task.Task{
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

func (f *fakeRepository) Update(_ context.Context, t task.Task) (task.Task, error) {
	for i := range f.tasks {
		if f.tasks[i].ID == t.ID {
			t.CreatedAt = f.tasks[i].CreatedAt
			t.UpdatedAt = f.tasks[i].UpdatedAt.Add(time.Minute)
			f.tasks[i] = t
			return t, nil
		}
	}
	return task.Task{}, task.ErrNotFound
}

func (f *fakeRepository) Delete(_ context.Context, id int64) error {
	for i := range f.tasks {
		if f.tasks[i].ID == id {
			f.tasks = append(f.tasks[:i], f.tasks[i+1:]...)
			return nil
		}
	}
	return task.ErrNotFound
}

func newTestRouter(t *testing.T, repo task.Repository) *gin.Engine {
	t.Helper()
	handler, err := NewHandler(repo)
	if err != nil {
		t.Fatalf("NewHandler() error = %v", err)
	}
	router := gin.New()
	handler.RegisterRoutes(router)
	return router
}

func get(router http.Handler, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postForm(router http.Handler, path string, values url.Values) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Errorf("body does not contain %q\nbody:\n%s", want, body)
	}
}

func TestListPageEmpty(t *testing.T) {
	router := newTestRouter(t, newFakeRepository())
	rec := get(router, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	body := rec.Body.String()
	assertContains(t, body, "No tasks yet")
	assertContains(t, body, `action="/ui/tasks"`)
	assertContains(t, body, "0 tasks total")
}

func TestListPageShowsTasks(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(
		task.Task{
			ID:          1,
			Title:       "Write docs",
			Description: "Explain every endpoint",
			Completed:   true,
			CreatedAt:   time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC),
		},
		task.Task{ID: 2, Title: "Open task", CreatedAt: time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)},
	)
	rec := get(newTestRouter(t, repo), "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, "Write docs")
	assertContains(t, body, "Explain every endpoint")
	assertContains(t, body, "is-complete")
	assertContains(t, body, "Done")
	assertContains(t, body, "Open task")
	assertContains(t, body, "Jan 2, 2026 15:04")
	assertContains(t, body, "2 tasks total")
	assertContains(t, body, `href="/ui/tasks/1/delete"`)
}

func TestCreateTaskForm(t *testing.T) {
	repo := newFakeRepository()
	router := newTestRouter(t, repo)

	rec := postForm(router, "/ui/tasks", url.Values{
		"title":       {"  From the form  "},
		"description": {"created without JSON"},
	})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "/" {
		t.Errorf("Location = %q, want /", loc)
	}
	if len(repo.tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(repo.tasks))
	}
	if got := repo.tasks[0].Title; got != "From the form" {
		t.Errorf("Title = %q, want trimmed title", got)
	}
}

func TestCreateTaskFormValidation(t *testing.T) {
	repo := newFakeRepository()
	router := newTestRouter(t, repo)

	rec := postForm(router, "/ui/tasks", url.Values{"title": {"   "}})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertContains(t, rec.Body.String(), "title is required")
	if len(repo.tasks) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(repo.tasks))
	}
}

func TestEditPage(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(task.Task{
		ID:          1,
		Title:       "Old title",
		Description: "Old description",
		CreatedAt:   time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC),
	})
	rec := get(newTestRouter(t, repo), "/ui/tasks/1/edit")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, "Old title")
	assertContains(t, body, "Old description")
	assertContains(t, body, `action="/ui/tasks/1"`)
	assertContains(t, body, `href="/ui/tasks/1/delete"`)
}

func TestEditPageNotFound(t *testing.T) {
	rec := get(newTestRouter(t, newFakeRepository()), "/ui/tasks/99/edit")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	assertContains(t, rec.Body.String(), "Task not found")
}

func TestEditPageInvalidID(t *testing.T) {
	rec := get(newTestRouter(t, newFakeRepository()), "/ui/tasks/abc/edit")

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertContains(t, rec.Body.String(), "Invalid task ID")
}

func TestUpdateTaskForm(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(task.Task{ID: 1, Title: "Old", Description: "keep me"})
	router := newTestRouter(t, repo)

	rec := postForm(router, "/ui/tasks/1", url.Values{
		"title":       {"  New title  "},
		"description": {"changed"},
		"completed":   {"true"},
	})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	got := repo.tasks[0]
	if got.Title != "New title" {
		t.Errorf("Title = %q, want New title", got.Title)
	}
	if got.Description != "changed" {
		t.Errorf("Description = %q, want changed", got.Description)
	}
	if !got.Completed {
		t.Error("Completed = false, want true")
	}
}

func TestUpdateTaskUnchecked(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(task.Task{ID: 1, Title: "Done task", Completed: true})
	router := newTestRouter(t, repo)

	rec := postForm(router, "/ui/tasks/1", url.Values{
		"title":       {"Done task"},
		"description": {""},
	})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if repo.tasks[0].Completed {
		t.Error("Completed = true, want false when checkbox is omitted")
	}
}

func TestUpdateTaskFormValidation(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(task.Task{ID: 1, Title: "Keep me"})
	router := newTestRouter(t, repo)

	rec := postForm(router, "/ui/tasks/1", url.Values{"title": {"  "}})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	assertContains(t, rec.Body.String(), "title is required")
	if repo.tasks[0].Title != "Keep me" {
		t.Errorf("Title = %q, want unchanged", repo.tasks[0].Title)
	}
}

func TestUpdateTaskNotFound(t *testing.T) {
	rec := postForm(newTestRouter(t, newFakeRepository()), "/ui/tasks/99", url.Values{"title": {"x"}})

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestToggleTask(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(task.Task{ID: 1, Title: "Toggle me"})
	router := newTestRouter(t, repo)

	rec := postForm(router, "/ui/tasks/1/toggle", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if !repo.tasks[0].Completed {
		t.Error("Completed = false, want true after first toggle")
	}

	rec = postForm(router, "/ui/tasks/1/toggle", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if repo.tasks[0].Completed {
		t.Error("Completed = true, want false after second toggle")
	}
}

func TestDeleteTaskForm(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(task.Task{ID: 1, Title: "Delete me"})
	router := newTestRouter(t, repo)

	rec := postForm(router, "/ui/tasks/1/delete", nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	if len(repo.tasks) != 0 {
		t.Errorf("len(tasks) = %d, want 0", len(repo.tasks))
	}
}

func TestDeleteTaskNotFound(t *testing.T) {
	rec := postForm(newTestRouter(t, newFakeRepository()), "/ui/tasks/99/delete", nil)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteConfirmPage(t *testing.T) {
	repo := newFakeRepository()
	repo.seed(task.Task{
		ID:        1,
		Title:     "Delete me",
		CreatedAt: time.Date(2026, 1, 2, 15, 4, 0, 0, time.UTC),
	})
	rec := get(newTestRouter(t, repo), "/ui/tasks/1/delete")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	assertContains(t, body, "Delete me")
	assertContains(t, body, "Delete task")
	assertContains(t, body, `action="/ui/tasks/1/delete"`)
	if len(repo.tasks) != 1 {
		t.Errorf("len(tasks) = %d after GET, want 1 (confirmation must not delete)", len(repo.tasks))
	}
}

func TestDeleteConfirmPageNotFound(t *testing.T) {
	rec := get(newTestRouter(t, newFakeRepository()), "/ui/tasks/99/delete")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestStaticCSS(t *testing.T) {
	rec := get(newTestRouter(t, newFakeRepository()), "/static/style.css")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("Content-Type = %q, want text/css", ct)
	}
	assertContains(t, rec.Body.String(), "--accent")
}

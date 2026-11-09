package web

import (
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/h4r5h6l/kubernetes_demo/internal/task"
)

// Handler serves the server-rendered HTML interface.
type Handler struct {
	repo   task.Repository
	pages  map[string]*template.Template
	static fs.FS
}

// NewHandler parses the embedded templates and prepares the static file system.
func NewHandler(repo task.Repository) (*Handler, error) {
	funcs := template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.Format("Jan 2, 2006 15:04")
		},
	}

	pages := make(map[string]*template.Template, 3)
	for _, page := range []string{"list.html", "edit.html", "error.html"} {
		tmpl, err := template.New("root").Funcs(funcs).
			ParseFS(assets, "templates/layout.html", "templates/"+page)
		if err != nil {
			return nil, fmt.Errorf("parse templates for %s: %w", page, err)
		}
		pages[page] = tmpl
	}

	staticFS, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, fmt.Errorf("prepare static assets: %w", err)
	}

	return &Handler{repo: repo, pages: pages, static: staticFS}, nil
}

// RegisterRoutes mounts the HTML pages and form actions.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/", h.List)
	r.POST("/ui/tasks", h.Create)
	r.GET("/ui/tasks/:id/edit", h.Edit)
	r.POST("/ui/tasks/:id", h.Update)
	r.POST("/ui/tasks/:id/toggle", h.Toggle)
	r.POST("/ui/tasks/:id/delete", h.Delete)
	r.StaticFS("/static", http.FS(h.static))
}

type listPageData struct {
	Title           string
	Tasks           []task.Task
	Errors          map[string]string
	FormTitle       string
	FormDescription string
}

type editPageData struct {
	Title  string
	Task   task.Task
	Errors map[string]string
}

type errorPageData struct {
	Title   string
	Status  int
	Heading string
	Message string
}

type taskForm struct {
	Title       string `form:"title"`
	Description string `form:"description"`
	Completed   string `form:"completed"`
}

// List handles GET /.
func (h *Handler) List(c *gin.Context) {
	tasks, err := h.repo.List(c.Request.Context())
	if err != nil {
		h.internalError(c, err, "Could not load tasks.")
		return
	}

	h.render(c, http.StatusOK, "list.html", listPageData{
		Title: "Tasks",
		Tasks: tasks,
	})
}

// Create handles POST /ui/tasks.
func (h *Handler) Create(c *gin.Context) {
	var form taskForm
	if err := c.ShouldBind(&form); err != nil {
		h.renderError(c, http.StatusBadRequest, "Invalid form", "Please submit the form again.")
		return
	}

	in := task.CreateInput{
		Title:       form.Title,
		Description: form.Description,
	}.Normalize()

	if fields := in.Validate(); len(fields) > 0 {
		tasks, err := h.repo.List(c.Request.Context())
		if err != nil {
			h.internalError(c, err, "Could not load tasks.")
			return
		}
		h.render(c, http.StatusBadRequest, "list.html", listPageData{
			Title:           "Tasks",
			Tasks:           tasks,
			Errors:          fields,
			FormTitle:       form.Title,
			FormDescription: form.Description,
		})
		return
	}

	if _, err := h.repo.Create(c.Request.Context(), in); err != nil {
		h.internalError(c, err, "Could not create the task.")
		return
	}

	c.Redirect(http.StatusSeeOther, "/")
}

// Edit handles GET /ui/tasks/:id/edit.
func (h *Handler) Edit(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}

	t, err := h.repo.Get(c.Request.Context(), id)
	if errors.Is(err, task.ErrNotFound) {
		h.renderError(c, http.StatusNotFound, "Task not found", "This task may have already been deleted.")
		return
	}
	if err != nil {
		h.internalError(c, err, "Could not load the task.")
		return
	}

	h.render(c, http.StatusOK, "edit.html", editPageData{
		Title: "Edit task",
		Task:  t,
	})
}

// Update handles POST /ui/tasks/:id.
func (h *Handler) Update(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}

	t, err := h.repo.Get(c.Request.Context(), id)
	if errors.Is(err, task.ErrNotFound) {
		h.renderError(c, http.StatusNotFound, "Task not found", "This task may have already been deleted.")
		return
	}
	if err != nil {
		h.internalError(c, err, "Could not load the task.")
		return
	}

	var form taskForm
	if err := c.ShouldBind(&form); err != nil {
		h.renderError(c, http.StatusBadRequest, "Invalid form", "Please submit the form again.")
		return
	}

	t.Title = task.NormalizeTitle(form.Title)
	t.Description = form.Description
	t.Completed = formCompleted(form.Completed)

	if fields := task.ValidateFields(t.Title, t.Description); len(fields) > 0 {
		h.render(c, http.StatusBadRequest, "edit.html", editPageData{
			Title:  "Edit task",
			Task:   t,
			Errors: fields,
		})
		return
	}

	if _, err := h.repo.Update(c.Request.Context(), t); err != nil {
		if errors.Is(err, task.ErrNotFound) {
			h.renderError(c, http.StatusNotFound, "Task not found", "This task may have already been deleted.")
			return
		}
		h.internalError(c, err, "Could not save the task.")
		return
	}

	c.Redirect(http.StatusSeeOther, "/")
}

// Toggle handles POST /ui/tasks/:id/toggle.
func (h *Handler) Toggle(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}

	t, err := h.repo.Get(c.Request.Context(), id)
	if errors.Is(err, task.ErrNotFound) {
		h.renderError(c, http.StatusNotFound, "Task not found", "This task may have already been deleted.")
		return
	}
	if err != nil {
		h.internalError(c, err, "Could not load the task.")
		return
	}

	t.Completed = !t.Completed
	if _, err := h.repo.Update(c.Request.Context(), t); err != nil {
		if errors.Is(err, task.ErrNotFound) {
			h.renderError(c, http.StatusNotFound, "Task not found", "This task may have already been deleted.")
			return
		}
		h.internalError(c, err, "Could not update the task.")
		return
	}

	c.Redirect(http.StatusSeeOther, "/")
}

// Delete handles POST /ui/tasks/:id/delete.
func (h *Handler) Delete(c *gin.Context) {
	id, ok := h.parseID(c)
	if !ok {
		return
	}

	if err := h.repo.Delete(c.Request.Context(), id); err != nil {
		if errors.Is(err, task.ErrNotFound) {
			h.renderError(c, http.StatusNotFound, "Task not found", "This task may have already been deleted.")
			return
		}
		h.internalError(c, err, "Could not delete the task.")
		return
	}

	c.Redirect(http.StatusSeeOther, "/")
}

func (h *Handler) parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		h.renderError(c, http.StatusBadRequest, "Invalid task ID", "Task IDs are positive numbers.")
		return 0, false
	}
	return id, true
}

func (h *Handler) render(c *gin.Context, status int, page string, data any) {
	tmpl, ok := h.pages[page]
	if !ok {
		log.Printf("web: unknown page %q", page)
		c.String(http.StatusInternalServerError, "internal server error")
		return
	}

	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Status(status)
	if err := tmpl.ExecuteTemplate(c.Writer, "layout", data); err != nil {
		log.Printf("web: render %s: %v", page, err)
	}
}

func (h *Handler) renderError(c *gin.Context, status int, heading, message string) {
	h.render(c, status, "error.html", errorPageData{
		Title:   http.StatusText(status),
		Status:  status,
		Heading: heading,
		Message: message,
	})
}

func (h *Handler) internalError(c *gin.Context, err error, message string) {
	log.Printf("web: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	h.renderError(c, http.StatusInternalServerError, "Something went wrong", message)
}

func formCompleted(value string) bool {
	return value == "true" || value == "on" || value == "1"
}

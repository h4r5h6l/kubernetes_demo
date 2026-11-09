package task

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/h4r5h6l/kubernetes_demo/internal/httpx"
)

// Handler serves the task JSON API.
type Handler struct {
	repo Repository
}

// NewHandler creates an API handler backed by repo.
func NewHandler(repo Repository) *Handler {
	return &Handler{repo: repo}
}

// RegisterRoutes mounts the CRUD routes on the given router.
func (h *Handler) RegisterRoutes(r gin.IRouter) {
	r.GET("/tasks", h.List)
	r.POST("/tasks", h.Create)
	r.GET("/tasks/:id", h.Get)
	r.PUT("/tasks/:id", h.Update)
	r.DELETE("/tasks/:id", h.Delete)
}

// List handles GET /tasks.
func (h *Handler) List(c *gin.Context) {
	tasks, err := h.repo.List(c.Request.Context())
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, tasks)
}

// Get handles GET /tasks/:id.
func (h *Handler) Get(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	t, err := h.repo.Get(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.NotFound(c, "task not found")
		return
	}
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, t)
}

// Create handles POST /tasks.
func (h *Handler) Create(c *gin.Context) {
	var in CreateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, "invalid_json", "request body must be valid JSON")
		return
	}

	in = in.Normalize()
	if fields := in.Validate(); len(fields) > 0 {
		httpx.ValidationError(c, fields)
		return
	}

	created, err := h.repo.Create(c.Request.Context(), in)
	if err != nil {
		httpx.Internal(c, err)
		return
	}

	c.Header("Location", fmt.Sprintf("/tasks/%d", created.ID))
	c.JSON(http.StatusCreated, created)
}

// Update handles PUT /tasks/:id. Omitted fields keep their current value.
func (h *Handler) Update(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	var in UpdateInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.BadRequest(c, "invalid_json", "request body must be valid JSON")
		return
	}

	t, err := h.repo.Get(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.NotFound(c, "task not found")
		return
	}
	if err != nil {
		httpx.Internal(c, err)
		return
	}

	if in.Title != nil {
		t.Title = *in.Title
	}
	if in.Description != nil {
		t.Description = *in.Description
	}
	if in.Completed != nil {
		t.Completed = *in.Completed
	}

	// Normalize after applying the patch so whitespace-only titles are caught.
	t.Title = NormalizeTitle(t.Title)
	if fields := ValidateFields(t.Title, t.Description); len(fields) > 0 {
		httpx.ValidationError(c, fields)
		return
	}

	updated, err := h.repo.Update(c.Request.Context(), t)
	if errors.Is(err, ErrNotFound) {
		httpx.NotFound(c, "task not found")
		return
	}
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.JSON(http.StatusOK, updated)
}

// Delete handles DELETE /tasks/:id.
func (h *Handler) Delete(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}

	err := h.repo.Delete(c.Request.Context(), id)
	if errors.Is(err, ErrNotFound) {
		httpx.NotFound(c, "task not found")
		return
	}
	if err != nil {
		httpx.Internal(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func parseID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id < 1 {
		httpx.BadRequest(c, "invalid_id", "task id must be a positive integer")
		return 0, false
	}
	return id, true
}

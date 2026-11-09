package task

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxTitleLength matches the tasks_title_check constraint.
	MaxTitleLength = 200
	// MaxDescriptionLength is validated in the application layer.
	MaxDescriptionLength = 2000
)

// Task is a single task item.
type Task struct {
	ID          int64     `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Completed   bool      `json:"completed"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// CreateInput is the request body for POST /tasks.
type CreateInput struct {
	Title       string `json:"title"`
	Description string `json:"description"`
}

// UpdateInput is the request body for PUT /tasks/:id. Every field is optional;
// omitted fields keep their current value.
type UpdateInput struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Completed   *bool   `json:"completed"`
}

// Normalize trims surrounding whitespace from the title.
func (in CreateInput) Normalize() CreateInput {
	in.Title = NormalizeTitle(in.Title)
	return in
}

// NormalizeTitle trims surrounding whitespace from a title.
func NormalizeTitle(title string) string {
	return strings.TrimSpace(title)
}

// Validate returns field-level error messages keyed by JSON field name.
func (in CreateInput) Validate() map[string]string {
	return ValidateFields(in.Title, in.Description)
}

// ValidateFields validates a title/description pair.
func ValidateFields(title, description string) map[string]string {
	fields := make(map[string]string)
	if title == "" {
		fields["title"] = "title is required"
	} else if utf8.RuneCountInString(title) > MaxTitleLength {
		fields["title"] = fmt.Sprintf("title must be at most %d characters", MaxTitleLength)
	}
	if utf8.RuneCountInString(description) > MaxDescriptionLength {
		fields["description"] = fmt.Sprintf("description must be at most %d characters", MaxDescriptionLength)
	}
	return fields
}

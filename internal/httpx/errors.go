// Package httpx contains shared HTTP helpers and handlers.
package httpx

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type errorDetail struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

// Error writes a consistent JSON error response.
func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, errorEnvelope{
		Error: errorDetail{Code: code, Message: message},
	})
}

// ValidationError writes a 400 response with per-field messages.
func ValidationError(c *gin.Context, fields map[string]string) {
	c.AbortWithStatusJSON(http.StatusBadRequest, errorEnvelope{
		Error: errorDetail{
			Code:    "validation_error",
			Message: "request validation failed",
			Fields:  fields,
		},
	})
}

// NotFound writes a 404 response.
func NotFound(c *gin.Context, message string) {
	Error(c, http.StatusNotFound, "not_found", message)
}

// BadRequest writes a 400 response.
func BadRequest(c *gin.Context, code, message string) {
	Error(c, http.StatusBadRequest, code, message)
}

// Internal logs the underlying error and writes a generic 500 response.
func Internal(c *gin.Context, err error) {
	log.Printf("internal error: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	Error(c, http.StatusInternalServerError, "internal_error", "internal server error")
}

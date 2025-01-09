// handlers/generic.go
package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
)

// Request represents the base request structure
type Request struct {
	Method      string
	PathParams  map[string]string
	QueryParams map[string]string
	Headers     http.Header
	Body        []byte
}

// Response represents the base response structure
type Response struct {
	StatusCode  int
	Headers     http.Header
	Body        interface{}
	ContentType string
	IsStreaming bool
}

// HandlerFunc is the generic handler function type
type HandlerFunc[T any, R any] func(context.Context, *Request, T) (R, error)

// ErrorResponse represents a standard error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// HandlerOptions contains configuration for the handler
type HandlerOptions struct {
	Logger        *slog.Logger
	DecodeBody    bool
	ValidateInput func(interface{}) error
}

// Handle creates a generic HTTP handler that processes requests of type T and returns responses of type R
func Handle[T any, R any](handlerFunc HandlerFunc[T, R], opts HandlerOptions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger := opts.Logger
		if logger == nil {
			logger = slog.Default()
		}

		req, err := parseRequest(r)
		if err != nil {
			sendError(w, logger, http.StatusBadRequest, "failed to parse request", err)
			return
		}

		var input T
		if opts.DecodeBody && len(req.Body) > 0 && r.Header.Get("Content-Type") == "application/json" {
			if err := json.Unmarshal(req.Body, &input); err != nil {
				sendError(w, logger, http.StatusBadRequest, "failed to decode request body", err)
				return
			}
		}

		if opts.ValidateInput != nil {
			if err := opts.ValidateInput(input); err != nil {
				sendError(w, logger, http.StatusBadRequest, "input validation failed", err)
				return
			}
		}

		result, err := handlerFunc(r.Context(), req, input)
		if err != nil {
			handleError(w, logger, err)
			return
		}

		sendResponse(w, logger, result)
	}
}

func parseRequest(r *http.Request) (*Request, error) {
	req := &Request{
		Method:      r.Method,
		PathParams:  make(map[string]string),
		QueryParams: make(map[string]string),
		Headers:     r.Header,
	}

	// Extract path parameters if they exist
	for _, param := range []string{"bucket", "key"} {
		if val := r.PathValue(param); val != "" {
			req.PathParams[param] = val
		}
	}

	// Parse query parameters
	for key := range r.URL.Query() {
		req.QueryParams[key] = r.URL.Query().Get(key)
	}

	// Read and store body if present
	if r.Body != nil {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		defer r.Body.Close()
		req.Body = body
	}

	return req, nil
}

func sendError(w http.ResponseWriter, logger *slog.Logger, code int, message string, err error) {
	logger.Error(message,
		"error", err,
		"code", code,
	)

	errResp := ErrorResponse{
		Error:   err.Error(),
		Code:    code,
		Message: message,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(errResp)
}

func handleError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var code int
	var message string

	switch err.(type) {
	case *NotFoundError:
		code = http.StatusNotFound
		message = "resource not found"
	case *ValidationError:
		code = http.StatusBadRequest
		message = "validation error"
	default:
		code = http.StatusInternalServerError
		message = "internal server error"
	}

	sendError(w, logger, code, message, err)
}

func sendResponse(w http.ResponseWriter, logger *slog.Logger, response interface{}) {
	var statusCode int
	var bodySize int
	var contentType string

	switch resp := response.(type) {
	case *Response:
		// Set custom headers
		for k, v := range resp.Headers {
			w.Header()[k] = v
		}

		// Set content type
		if resp.ContentType != "" {
			w.Header().Set("Content-Type", resp.ContentType)
			contentType = resp.ContentType
		} else {
			w.Header().Set("Content-Type", "application/json")
			contentType = "application/json"
		}

		// Set status code
		if resp.StatusCode != 0 {
			w.WriteHeader(resp.StatusCode)
			statusCode = resp.StatusCode
		} else {
			w.WriteHeader(http.StatusOK)
			statusCode = http.StatusOK
		}

		// Write body based on type
		switch body := resp.Body.(type) {
		case []byte:
			w.Write(body)
			bodySize = len(body)
		case string:
			bodyBytes := []byte(body)
			w.Write(bodyBytes)
			bodySize = len(bodyBytes)
		default:
			if body != nil {
				buf := &bytes.Buffer{}
				encoder := json.NewEncoder(buf)
				if err := encoder.Encode(body); err != nil {
					logger.Error("failed to encode response", "error", err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
					return
				}
				w.Write(buf.Bytes())
				bodySize = buf.Len()
			}
		}
	default:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		statusCode = http.StatusOK

		buf := &bytes.Buffer{}
		encoder := json.NewEncoder(buf)
		if err := encoder.Encode(response); err != nil {
			logger.Error("failed to encode response", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
		w.Write(buf.Bytes())
		bodySize = buf.Len()
		contentType = "application/json"
	}

	// Log the response details
	logger.Info("response sent",
		"status_code", statusCode,
		"content_type", contentType,
		"body_size", bodySize,
	)
}

// Custom error types
type NotFoundError struct {
	Resource string
	ID       string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("%s with ID %s not found", e.Resource, e.ID)
}

type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s - %s", e.Field, e.Message)
}

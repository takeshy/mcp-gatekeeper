package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Writer writes Server-Sent Events to an HTTP response
type Writer struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

// NewWriter creates a new SSE writer from an http.ResponseWriter
// Returns an error if the ResponseWriter does not support flushing
func NewWriter(w http.ResponseWriter) (*Writer, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported")
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	return &Writer{
		w:       w,
		flusher: flusher,
	}, nil
}

// WriteEvent writes an SSE event with the given event type, ID, and data
// The data is JSON-encoded before being written
func (s *Writer) WriteEvent(eventType string, id string, data interface{}) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	return s.WriteRawEvent(eventType, id, string(jsonData))
}

// WriteRawEvent writes an SSE event with raw string data
func (s *Writer) WriteRawEvent(eventType string, id string, data string) error {
	// Write event type if provided
	if eventType != "" {
		if _, err := fmt.Fprintf(s.w, "event: %s\n", eventType); err != nil {
			return err
		}
	}

	// Write event ID if provided
	if id != "" {
		if _, err := fmt.Fprintf(s.w, "id: %s\n", id); err != nil {
			return err
		}
	}

	// Write data
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return err
	}

	s.flusher.Flush()
	return nil
}

// WriteComment writes an SSE comment (used for keep-alive)
func (s *Writer) WriteComment(comment string) error {
	if _, err := fmt.Fprintf(s.w, ": %s\n\n", comment); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// Flush flushes the response writer
func (s *Writer) Flush() {
	s.flusher.Flush()
}

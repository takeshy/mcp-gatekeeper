package sse

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// flushRecorder wraps httptest.ResponseRecorder to support http.Flusher
type flushRecorder struct {
	*httptest.ResponseRecorder
	flushed bool
}

func (r *flushRecorder) Flush() {
	r.flushed = true
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{
		ResponseRecorder: httptest.NewRecorder(),
	}
}

// nonFlushRecorder is an http.ResponseWriter that doesn't support Flusher
type nonFlushRecorder struct {
	http.ResponseWriter
}

func TestNewWriter(t *testing.T) {
	// Test with Flusher support
	w := newFlushRecorder()
	sseWriter, err := NewWriter(w)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if sseWriter == nil {
		t.Fatal("expected writer, got nil")
	}

	// Check headers
	if got := w.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("expected Content-Type text/event-stream, got %s", got)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("expected Cache-Control no-cache, got %s", got)
	}
	if got := w.Header().Get("Connection"); got != "keep-alive" {
		t.Errorf("expected Connection keep-alive, got %s", got)
	}

	// Test without Flusher support
	nonFlush := &nonFlushRecorder{}
	_, err = NewWriter(nonFlush)
	if err == nil {
		t.Error("expected error for non-flusher, got nil")
	}
}

func TestWriter_WriteEvent(t *testing.T) {
	w := newFlushRecorder()
	sseWriter, _ := NewWriter(w)

	data := map[string]string{"key": "value"}
	err := sseWriter.WriteEvent("message", "123", data)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	body := w.Body.String()

	// Check event type
	if !strings.Contains(body, "event: message\n") {
		t.Error("expected event line in output")
	}

	// Check ID
	if !strings.Contains(body, "id: 123\n") {
		t.Error("expected id line in output")
	}

	// Check data
	if !strings.Contains(body, `data: {"key":"value"}`) {
		t.Errorf("expected data line in output, got: %s", body)
	}

	// Check flushed
	if !w.flushed {
		t.Error("expected Flush to be called")
	}
}

func TestWriter_WriteEvent_EmptyEventType(t *testing.T) {
	w := newFlushRecorder()
	sseWriter, _ := NewWriter(w)

	err := sseWriter.WriteEvent("", "123", "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	body := w.Body.String()

	// Should not have event line
	if strings.Contains(body, "event:") {
		t.Error("expected no event line for empty event type")
	}

	// Should have id line
	if !strings.Contains(body, "id: 123\n") {
		t.Error("expected id line in output")
	}
}

func TestWriter_WriteEvent_EmptyID(t *testing.T) {
	w := newFlushRecorder()
	sseWriter, _ := NewWriter(w)

	err := sseWriter.WriteEvent("message", "", "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	body := w.Body.String()

	// Should have event line
	if !strings.Contains(body, "event: message\n") {
		t.Error("expected event line in output")
	}

	// Should not have id line
	if strings.Contains(body, "id:") {
		t.Error("expected no id line for empty ID")
	}
}

func TestWriter_WriteRawEvent(t *testing.T) {
	w := newFlushRecorder()
	sseWriter, _ := NewWriter(w)

	err := sseWriter.WriteRawEvent("message", "456", "raw data here")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	body := w.Body.String()

	expected := "event: message\nid: 456\ndata: raw data here\n\n"
	if body != expected {
		t.Errorf("expected:\n%q\ngot:\n%q", expected, body)
	}
}

func TestWriter_WriteComment(t *testing.T) {
	w := newFlushRecorder()
	sseWriter, _ := NewWriter(w)

	err := sseWriter.WriteComment("keep-alive")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	body := w.Body.String()

	expected := ": keep-alive\n\n"
	if body != expected {
		t.Errorf("expected %q, got %q", expected, body)
	}

	if !w.flushed {
		t.Error("expected Flush to be called")
	}
}

func TestWriter_Flush(t *testing.T) {
	w := newFlushRecorder()
	sseWriter, _ := NewWriter(w)

	sseWriter.Flush()

	if !w.flushed {
		t.Error("expected Flush to be called")
	}
}

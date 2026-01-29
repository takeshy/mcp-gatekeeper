package session

import (
	"context"
	"testing"
	"time"
)

func TestManager_Create(t *testing.T) {
	m := NewManager(time.Hour)

	session := m.Create()
	if session == nil {
		t.Fatal("expected session, got nil")
	}
	if session.ID == "" {
		t.Error("expected non-empty session ID")
	}
	if session.CreatedAt.IsZero() {
		t.Error("expected non-zero created time")
	}
	if session.LastActivity.IsZero() {
		t.Error("expected non-zero last activity time")
	}
	if m.Count() != 1 {
		t.Errorf("expected 1 session, got %d", m.Count())
	}
}

func TestManager_Get(t *testing.T) {
	m := NewManager(time.Hour)

	session := m.Create()

	// Get existing session
	got, ok := m.Get(session.ID)
	if !ok {
		t.Error("expected to find session")
	}
	if got.ID != session.ID {
		t.Errorf("expected ID %s, got %s", session.ID, got.ID)
	}

	// Get non-existent session
	_, ok = m.Get("non-existent")
	if ok {
		t.Error("expected not to find non-existent session")
	}
}

func TestManager_Delete(t *testing.T) {
	m := NewManager(time.Hour)

	session := m.Create()
	id := session.ID

	// Delete existing session
	ok := m.Delete(id)
	if !ok {
		t.Error("expected Delete to return true for existing session")
	}
	if m.Count() != 0 {
		t.Errorf("expected 0 sessions, got %d", m.Count())
	}

	// Verify session is gone
	_, ok = m.Get(id)
	if ok {
		t.Error("expected session to be deleted")
	}

	// Delete non-existent session
	ok = m.Delete("non-existent")
	if ok {
		t.Error("expected Delete to return false for non-existent session")
	}
}

func TestManager_Touch(t *testing.T) {
	m := NewManager(time.Hour)

	session := m.Create()
	originalActivity := session.LastActivity

	// Wait a bit and touch
	time.Sleep(10 * time.Millisecond)
	m.Touch(session.ID)

	got, _ := m.Get(session.ID)
	if !got.LastActivity.After(originalActivity) {
		t.Error("expected last activity to be updated")
	}

	// Touch non-existent session (should not panic)
	m.Touch("non-existent")
}

func TestManager_Cleanup(t *testing.T) {
	// Use MinTTL to ensure the TTL is valid
	ttl := MinTTL
	m := NewManager(ttl)

	session := m.Create()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.StartCleanup(ctx)

	// Session should exist initially
	_, ok := m.Get(session.ID)
	if !ok {
		t.Error("expected session to exist initially")
	}

	// Wait for TTL to expire and cleanup to run (cleanup runs every ttl/2)
	time.Sleep(ttl * 3)

	// Session should be cleaned up
	_, ok = m.Get(session.ID)
	if ok {
		t.Error("expected session to be cleaned up after TTL")
	}

	m.Stop()
}

func TestSession_SSEChannels(t *testing.T) {
	m := NewManager(time.Hour)
	session := m.Create()

	ch1 := make(chan *SSEEvent, 10)
	ch2 := make(chan *SSEEvent, 10)

	session.AddSSEChannel(ch1)
	session.AddSSEChannel(ch2)

	// Broadcast an event
	event := &SSEEvent{ID: "1", Data: "test"}
	session.Broadcast(event)

	// Both channels should receive the event
	select {
	case got := <-ch1:
		if got.ID != "1" {
			t.Errorf("expected event ID 1, got %s", got.ID)
		}
	case <-time.After(time.Second):
		t.Error("expected event on ch1")
	}

	select {
	case got := <-ch2:
		if got.ID != "1" {
			t.Errorf("expected event ID 1, got %s", got.ID)
		}
	case <-time.After(time.Second):
		t.Error("expected event on ch2")
	}

	// Remove one channel
	session.RemoveSSEChannel(ch1)

	// Broadcast another event
	event2 := &SSEEvent{ID: "2", Data: "test2"}
	session.Broadcast(event2)

	// Only ch2 should receive it
	select {
	case <-ch1:
		t.Error("ch1 should not receive event after removal")
	default:
	}

	select {
	case got := <-ch2:
		if got.ID != "2" {
			t.Errorf("expected event ID 2, got %s", got.ID)
		}
	case <-time.After(time.Second):
		t.Error("expected event on ch2")
	}
}

func TestSession_Close(t *testing.T) {
	m := NewManager(time.Hour)
	session := m.Create()

	ch := make(chan *SSEEvent, 10)
	session.AddSSEChannel(ch)

	session.Close()

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("expected channel to be closed")
	}

	// Adding channel after close should not add it
	ch2 := make(chan *SSEEvent, 10)
	session.AddSSEChannel(ch2)

	// Verify ch2 was not added by broadcasting
	session.Broadcast(&SSEEvent{ID: "1", Data: "test"})

	select {
	case <-ch2:
		t.Error("ch2 should not receive event after session close")
	default:
	}
}

func TestManager_Stop(t *testing.T) {
	m := NewManager(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.StartCleanup(ctx)

	// Stop should not panic
	m.Stop()
}

func TestManager_Stop_DoubleCall(t *testing.T) {
	m := NewManager(time.Hour)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.StartCleanup(ctx)

	// Double Stop should not panic
	m.Stop()
	m.Stop() // This should not panic
}

func TestNewManager_NegativeTTL(t *testing.T) {
	// Negative TTL should be corrected to MinTTL
	m := NewManager(-time.Hour)
	if m.ttl < MinTTL {
		t.Errorf("expected TTL to be at least %v, got %v", MinTTL, m.ttl)
	}
}

func TestNewManager_ZeroTTL(t *testing.T) {
	// Zero TTL should be corrected to MinTTL
	m := NewManager(0)
	if m.ttl < MinTTL {
		t.Errorf("expected TTL to be at least %v, got %v", MinTTL, m.ttl)
	}
}

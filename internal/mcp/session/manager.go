package session

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SSEEvent represents an SSE event to be sent to clients
type SSEEvent struct {
	ID   string
	Data interface{}
}

// Session represents an MCP session
type Session struct {
	ID           string
	CreatedAt    time.Time
	LastActivity time.Time
	mu           sync.Mutex
	sseChans     []chan *SSEEvent
	closed       bool
}

// AddSSEChannel adds an SSE channel for server-to-client notifications
func (s *Session) AddSSEChannel(ch chan *SSEEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.sseChans = append(s.sseChans, ch)
	}
}

// RemoveSSEChannel removes an SSE channel
func (s *Session) RemoveSSEChannel(ch chan *SSEEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, c := range s.sseChans {
		if c == ch {
			s.sseChans = append(s.sseChans[:i], s.sseChans[i+1:]...)
			break
		}
	}
}

// Broadcast sends an event to all SSE channels
func (s *Session) Broadcast(event *SSEEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.sseChans {
		select {
		case ch <- event:
		default:
			// Channel is full or closed, skip
		}
	}
}

// Close marks the session as closed and closes all SSE channels
func (s *Session) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, ch := range s.sseChans {
		close(ch)
	}
	s.sseChans = nil
}

// Manager manages MCP sessions with TTL expiration
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
	stopCh   chan struct{}
	stopped  bool
}

// MinTTL is the minimum allowed session TTL
const MinTTL = time.Second

// NewManager creates a new session manager
// If ttl is less than MinTTL, it defaults to MinTTL to prevent ticker panic
func NewManager(ttl time.Duration) *Manager {
	if ttl < MinTTL {
		ttl = MinTTL
	}
	return &Manager{
		sessions: make(map[string]*Session),
		ttl:      ttl,
		stopCh:   make(chan struct{}),
	}
}

// Create creates a new session and returns it
func (m *Manager) Create() *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	session := &Session{
		ID:           uuid.New().String(),
		CreatedAt:    now,
		LastActivity: now,
		sseChans:     make([]chan *SSEEvent, 0),
	}

	m.sessions[session.ID] = session
	return session
}

// Get retrieves a session by ID
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[id]
	return session, ok
}

// Delete removes a session by ID and returns whether it existed
func (m *Manager) Delete(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[id]
	if ok {
		session.Close()
		delete(m.sessions, id)
	}
	return ok
}

// Touch updates the last activity time for a session
func (m *Manager) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[id]; ok {
		session.LastActivity = time.Now()
	}
}

// StartCleanup starts a background goroutine that cleans up expired sessions
func (m *Manager) StartCleanup(ctx context.Context) {
	go m.cleanupLoop(ctx)
}

// Stop stops the cleanup goroutine (safe to call multiple times)
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.stopped {
		m.stopped = true
		close(m.stopCh)
	}
}

func (m *Manager) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(m.ttl / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.cleanup()
		}
	}
}

func (m *Manager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, session := range m.sessions {
		if now.Sub(session.LastActivity) > m.ttl {
			session.Close()
			delete(m.sessions, id)
		}
	}
}

// Count returns the number of active sessions
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

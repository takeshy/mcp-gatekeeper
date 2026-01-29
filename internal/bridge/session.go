package bridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// SSEEvent represents an SSE event to be sent to clients
type SSEEvent struct {
	ID   string
	Data interface{}
}

// BridgeSession represents a session with its own upstream connection
type BridgeSession struct {
	ID           string
	CreatedAt    time.Time
	LastActivity time.Time
	Client       *Client // Each session has its own upstream client
	mu           sync.Mutex
	sseChans     []chan *SSEEvent
	closed       bool
	clientConfig *ClientConfig
	cancel       context.CancelFunc
}

// AddSSEChannel adds an SSE channel for server-to-client notifications
func (s *BridgeSession) AddSSEChannel(ch chan *SSEEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.sseChans = append(s.sseChans, ch)
	}
}

// RemoveSSEChannel removes an SSE channel
func (s *BridgeSession) RemoveSSEChannel(ch chan *SSEEvent) {
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
func (s *BridgeSession) Broadcast(event *SSEEvent) {
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

// Close closes the session and its upstream client
func (s *BridgeSession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	// Close all SSE channels
	for _, ch := range s.sseChans {
		close(ch)
	}
	s.sseChans = nil

	// Cancel upstream process context
	if s.cancel != nil {
		s.cancel()
	}

	// Close the upstream client
	if s.Client != nil {
		return s.Client.Close()
	}
	return nil
}

// SessionManager manages bridge sessions with TTL expiration
type SessionManager struct {
	mu           sync.RWMutex
	sessions     map[string]*BridgeSession
	ttl          time.Duration
	stopCh       chan struct{}
	stopped      bool
	clientConfig *ClientConfig
	baseCtx      context.Context
	cancelAll    context.CancelFunc
}

// MinSessionTTL is the minimum allowed session TTL
const MinSessionTTL = time.Second

// NewSessionManager creates a new session manager
func NewSessionManager(ttl time.Duration, clientConfig *ClientConfig) *SessionManager {
	if ttl < MinSessionTTL {
		ttl = MinSessionTTL
	}
	baseCtx, cancel := context.WithCancel(context.Background())
	return &SessionManager{
		sessions:     make(map[string]*BridgeSession),
		ttl:          ttl,
		stopCh:       make(chan struct{}),
		clientConfig: clientConfig,
		baseCtx:      baseCtx,
		cancelAll:    cancel,
	}
}

// Create creates a new session with its own upstream client
func (m *SessionManager) Create(ctx context.Context) (*BridgeSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	sessionCtx, cancel := context.WithCancel(m.baseCtx)

	// Create a new client for this session
	client := NewClient(m.clientConfig)
	if err := client.Start(sessionCtx); err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start upstream client: %w", err)
	}

	// Initialize the upstream
	if _, err := client.Initialize(sessionCtx); err != nil {
		client.Close()
		cancel()
		return nil, fmt.Errorf("failed to initialize upstream: %w", err)
	}

	session := &BridgeSession{
		ID:           uuid.New().String(),
		CreatedAt:    now,
		LastActivity: now,
		Client:       client,
		sseChans:     make([]chan *SSEEvent, 0),
		clientConfig: m.clientConfig,
		cancel:       cancel,
	}

	m.sessions[session.ID] = session
	return session, nil
}

// Get retrieves a session by ID
func (m *SessionManager) Get(id string) (*BridgeSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[id]
	return session, ok
}

// Delete removes a session by ID and closes its upstream client
func (m *SessionManager) Delete(id string) bool {
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
func (m *SessionManager) Touch(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if session, ok := m.sessions[id]; ok {
		session.LastActivity = time.Now()
	}
}

// StartCleanup starts a background goroutine that cleans up expired sessions
func (m *SessionManager) StartCleanup(ctx context.Context) {
	go m.cleanupLoop(ctx)
}

// Stop stops the cleanup goroutine (safe to call multiple times)
func (m *SessionManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.stopped {
		m.stopped = true
		close(m.stopCh)
	}
}

func (m *SessionManager) cleanupLoop(ctx context.Context) {
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

func (m *SessionManager) cleanup() {
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
func (m *SessionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.sessions)
}

// CloseAll closes all sessions
func (m *SessionManager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancelAll != nil {
		m.cancelAll()
	}

	for id, session := range m.sessions {
		session.Close()
		delete(m.sessions, id)
	}
}

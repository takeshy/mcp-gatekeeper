package bridge

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStore manages temporary files with unique keys for HTTP retrieval
type FileStore struct {
	mu    sync.RWMutex
	files map[string]*StoredFile
	dir   string
}

// StoredFile contains metadata about a stored file
type StoredFile struct {
	Path     string
	MimeType string
	Size     int
}

// NewFileStore creates a new file store
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create file store directory: %w", err)
	}
	return &FileStore{
		files: make(map[string]*StoredFile),
		dir:   dir,
	}, nil
}

// generateKey generates a cryptographically secure random key
func generateKey() (string, error) {
	bytes := make([]byte, 32) // 256 bits
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// Store saves data and returns a unique key
func (fs *FileStore) Store(data []byte, mimeType string) (string, error) {
	key, err := generateKey()
	if err != nil {
		return "", fmt.Errorf("failed to generate key: %w", err)
	}

	// Determine file extension from mime type
	ext := extFromMimeType(mimeType)

	filename := key + ext
	filePath := filepath.Join(fs.dir, filename)

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	fs.mu.Lock()
	fs.files[key] = &StoredFile{
		Path:     filePath,
		MimeType: mimeType,
		Size:     len(data),
	}
	fs.mu.Unlock()

	return key, nil
}

// Get retrieves and deletes a file by key (one-time retrieval)
func (fs *FileStore) Get(key string) (*StoredFile, []byte, error) {
	fs.mu.Lock()
	file, exists := fs.files[key]
	if exists {
		delete(fs.files, key)
	}
	fs.mu.Unlock()

	if !exists {
		return nil, nil, fmt.Errorf("file not found")
	}

	data, err := os.ReadFile(file.Path)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read file: %w", err)
	}

	// Delete the file after reading
	os.Remove(file.Path)

	return file, data, nil
}

// StoreBase64 decodes base64 data and stores it
func (fs *FileStore) StoreBase64(base64Data string) (string, string, error) {
	// Decode base64
	decoded, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return "", "", fmt.Errorf("failed to decode base64: %w", err)
	}

	// Detect mime type from magic bytes
	mimeType := detectMimeType(decoded)

	key, err := fs.Store(decoded, mimeType)
	if err != nil {
		return "", "", err
	}

	return key, mimeType, nil
}

// extFromMimeType returns file extension for a mime type
func extFromMimeType(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "application/pdf":
		return ".pdf"
	case "text/plain":
		return ".txt"
	case "text/html":
		return ".html"
	case "text/css":
		return ".css"
	case "text/javascript", "application/javascript":
		return ".js"
	case "application/json":
		return ".json"
	case "application/xml", "text/xml":
		return ".xml"
	case "video/mp4":
		return ".mp4"
	case "video/webm":
		return ".webm"
	case "audio/mpeg":
		return ".mp3"
	case "audio/wav":
		return ".wav"
	default:
		return ".bin"
	}
}

// ExtractBase64Image extracts base64 encoded image data from text if present
// Returns the base64 data and true if found, empty string and false otherwise
func ExtractBase64Image(text string) (string, bool) {
	// Common base64 image prefixes
	prefixes := []string{
		"iVBORw0KGgo", // PNG
		"/9j/",        // JPEG
		"R0lGOD",      // GIF
		"UklGR",       // WEBP
		"JVBERi0",     // PDF
	}

	for _, prefix := range prefixes {
		if idx := findBase64Start(text, prefix); idx >= 0 {
			base64Data := extractBase64Sequence(text, idx)
			if len(base64Data) > 100 { // Minimum reasonable size
				return base64Data, true
			}
		}
	}

	// Check for data URI format: data:image/...;base64,
	if idx := findIndex(text, "data:"); idx >= 0 {
		// Find base64 marker
		base64Idx := findIndex(text[idx:], ";base64,")
		if base64Idx >= 0 {
			start := idx + base64Idx + 8 // len(";base64,")
			base64Data := extractBase64Sequence(text, start)
			if len(base64Data) > 100 {
				return base64Data, true
			}
		}
	}

	return "", false
}

// findIndex finds substring in text, returns -1 if not found
func findIndex(text, substr string) int {
	for i := 0; i <= len(text)-len(substr); i++ {
		if text[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

// findBase64Start finds the start of base64 data with given prefix
func findBase64Start(text, prefix string) int {
	return findIndex(text, prefix)
}

// extractBase64Sequence extracts continuous base64 characters starting at idx
func extractBase64Sequence(text string, idx int) string {
	end := idx
	for end < len(text) {
		c := text[end]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			end++
		} else {
			break
		}
	}
	if end > idx {
		return text[idx:end]
	}
	return ""
}

// ExtractMarkdownImagePaths extracts image file paths from Markdown links
// Returns a list of absolute file paths found in the text
func ExtractMarkdownImagePaths(text string) []string {
	var paths []string

	// Pattern: [text](path) where path ends with image extension
	imageExts := []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".PNG", ".JPG", ".JPEG", ".GIF", ".WEBP"}

	i := 0
	for i < len(text) {
		// Find "](""
		idx := findIndex(text[i:], "](")
		if idx < 0 {
			break
		}
		start := i + idx + 2 // after "]("

		// Find closing ")"
		end := findIndex(text[start:], ")")
		if end < 0 {
			break
		}

		path := text[start : start+end]

		// Check if it's an image file
		for _, ext := range imageExts {
			if len(path) > len(ext) && path[len(path)-len(ext):] == ext {
				// Convert to absolute path
				absPath := resolveImagePath(path)
				if absPath != "" {
					paths = append(paths, absPath)
				}
				break
			}
		}

		i = start + end + 1
	}

	return paths
}

// resolveImagePath converts a potentially relative path to absolute path
func resolveImagePath(path string) string {
	// Handle paths starting with ../.. or similar
	// These are typically relative to some working directory

	// If path contains /tmp/, extract from there
	if idx := findIndex(path, "/tmp/"); idx >= 0 {
		return path[idx:]
	}

	// If it's already absolute
	if len(path) > 0 && path[0] == '/' {
		return path
	}

	// Try to resolve relative paths that go up directories
	// e.g., "../../../../tmp/foo" -> "/tmp/foo"
	cleaned := path
	for len(cleaned) > 3 && cleaned[:3] == "../" {
		cleaned = cleaned[3:]
	}
	if len(cleaned) > 0 && cleaned[0] != '/' {
		// Check if it starts with tmp/
		if len(cleaned) > 4 && cleaned[:4] == "tmp/" {
			return "/" + cleaned
		}
	}

	return ""
}

// StoreFile reads a file from disk and stores it
func (fs *FileStore) StoreFile(filePath string) (string, string, int, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", 0, fmt.Errorf("failed to read file: %w", err)
	}

	mimeType := detectMimeType(data)

	key, err := fs.Store(data, mimeType)
	if err != nil {
		return "", "", 0, err
	}

	return key, mimeType, len(data), nil
}

// detectMimeType detects mime type from file magic bytes
func detectMimeType(data []byte) string {
	if len(data) < 4 {
		return "application/octet-stream"
	}

	// PNG
	if data[0] == 0x89 && data[1] == 'P' && data[2] == 'N' && data[3] == 'G' {
		return "image/png"
	}
	// JPEG
	if data[0] == 0xFF && data[1] == 0xD8 {
		return "image/jpeg"
	}
	// GIF
	if data[0] == 'G' && data[1] == 'I' && data[2] == 'F' {
		return "image/gif"
	}
	// WEBP
	if len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P' {
		return "image/webp"
	}
	// PDF
	if data[0] == '%' && data[1] == 'P' && data[2] == 'D' && data[3] == 'F' {
		return "application/pdf"
	}
	// MP4 (ftyp box)
	if len(data) >= 8 && data[4] == 'f' && data[5] == 't' && data[6] == 'y' && data[7] == 'p' {
		return "video/mp4"
	}
	// WebM
	if len(data) >= 4 && data[0] == 0x1A && data[1] == 0x45 && data[2] == 0xDF && data[3] == 0xA3 {
		return "video/webm"
	}
	// MP3
	if (data[0] == 0xFF && (data[1]&0xE0) == 0xE0) || (data[0] == 'I' && data[1] == 'D' && data[2] == '3') {
		return "audio/mpeg"
	}
	// WAV
	if len(data) >= 12 && data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'A' && data[10] == 'V' && data[11] == 'E' {
		return "audio/wav"
	}

	return "application/octet-stream"
}

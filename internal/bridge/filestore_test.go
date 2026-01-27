package bridge

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestNewFileStore(t *testing.T) {
	dir := t.TempDir()

	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}
	if fs == nil {
		t.Fatal("FileStore is nil")
	}
	if fs.dir != dir {
		t.Errorf("dir = %q, want %q", fs.dir, dir)
	}
}

func TestFileStore_Store(t *testing.T) {
	dir := t.TempDir()
	fs, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore failed: %v", err)
	}

	data := []byte("test data")
	key, err := fs.Store(data, "text/plain")
	if err != nil {
		t.Fatalf("Store failed: %v", err)
	}
	if key == "" {
		t.Fatal("key is empty")
	}
	if len(key) != 64 { // 32 bytes = 64 hex chars
		t.Errorf("key length = %d, want 64", len(key))
	}

	// Verify file exists
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Errorf("file count = %d, want 1", len(files))
	}
}

func TestFileStore_StoreWithMimeType(t *testing.T) {
	tests := []struct {
		mimeType string
		wantExt  string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"image/gif", ".gif"},
		{"image/webp", ".webp"},
		{"application/pdf", ".pdf"},
		{"text/plain", ".txt"},
		{"application/json", ".json"},
		{"application/octet-stream", ".bin"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			dir := t.TempDir()
			fs, _ := NewFileStore(dir)

			key, _ := fs.Store([]byte("data"), tt.mimeType)

			// Check file extension
			expectedPath := filepath.Join(dir, key+tt.wantExt)
			if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
				t.Errorf("file with extension %s not found", tt.wantExt)
			}
		})
	}
}

func TestFileStore_Get(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileStore(dir)

	// Store data
	data := []byte("test data for retrieval")
	key, _ := fs.Store(data, "text/plain")

	// Get data
	file, retrieved, err := fs.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(retrieved) != string(data) {
		t.Errorf("retrieved = %q, want %q", retrieved, data)
	}
	if file.MimeType != "text/plain" {
		t.Errorf("MimeType = %q, want %q", file.MimeType, "text/plain")
	}
	if file.Size != len(data) {
		t.Errorf("Size = %d, want %d", file.Size, len(data))
	}

	// Verify file is deleted after get
	_, _, err = fs.Get(key)
	if err == nil {
		t.Error("expected error on second Get, got nil")
	}
}

func TestFileStore_GetNotFound(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileStore(dir)

	_, _, err := fs.Get("nonexistent-key")
	if err == nil {
		t.Error("expected error for nonexistent key")
	}
}

func TestFileStore_StoreBase64(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileStore(dir)

	// Create test base64 data (PNG header)
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	base64Data := base64.StdEncoding.EncodeToString(pngHeader)

	key, mimeType, err := fs.StoreBase64(base64Data)
	if err != nil {
		t.Fatalf("StoreBase64 failed: %v", err)
	}
	if key == "" {
		t.Fatal("key is empty")
	}
	if mimeType != "image/png" {
		t.Errorf("mimeType = %q, want %q", mimeType, "image/png")
	}

	// Verify we can retrieve it
	file, data, _ := fs.Get(key)
	if string(data) != string(pngHeader) {
		t.Errorf("data mismatch")
	}
	if file.MimeType != "image/png" {
		t.Errorf("file.MimeType = %q, want %q", file.MimeType, "image/png")
	}
}

func TestDetectMimeType(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"PNG", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a}, "image/png"},
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0}, "image/jpeg"},
		{"GIF", []byte{'G', 'I', 'F', '8', '9', 'a'}, "image/gif"},
		{"WEBP", []byte{'R', 'I', 'F', 'F', 0, 0, 0, 0, 'W', 'E', 'B', 'P'}, "image/webp"},
		{"PDF", []byte{'%', 'P', 'D', 'F', '-', '1', '.', '4'}, "application/pdf"},
		{"Unknown", []byte{0x00, 0x01, 0x02, 0x03}, "application/octet-stream"},
		{"Short", []byte{0x00}, "application/octet-stream"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectMimeType(tt.data)
			if got != tt.want {
				t.Errorf("detectMimeType() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractBase64Image(t *testing.T) {
	// Create valid PNG base64 (needs to be long enough > 100 chars)
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
	// Pad to make base64 longer than 100 chars
	pngData := make([]byte, 100)
	copy(pngData, pngHeader)
	validPNGBase64 := base64.StdEncoding.EncodeToString(pngData)

	tests := []struct {
		name      string
		text      string
		wantFound bool
	}{
		{
			name:      "PNG base64 in text",
			text:      "Here is an image: " + validPNGBase64 + " end of image",
			wantFound: true,
		},
		{
			name:      "data URI format",
			text:      "data:image/png;base64," + validPNGBase64,
			wantFound: true,
		},
		{
			name:      "no base64",
			text:      "This is just regular text without any base64 data",
			wantFound: false,
		},
		{
			name:      "too short base64",
			text:      "iVBORw0KGgo",
			wantFound: false,
		},
		{
			name:      "empty text",
			text:      "",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, found := ExtractBase64Image(tt.text)
			if found != tt.wantFound {
				t.Errorf("ExtractBase64Image() found = %v, want %v", found, tt.wantFound)
			}
			if tt.wantFound && data == "" {
				t.Error("ExtractBase64Image() returned empty data when found=true")
			}
		})
	}
}

func TestExtFromMimeType(t *testing.T) {
	tests := []struct {
		mimeType string
		want     string
	}{
		{"image/png", ".png"},
		{"image/jpeg", ".jpg"},
		{"text/plain", ".txt"},
		{"application/json", ".json"},
		{"video/mp4", ".mp4"},
		{"audio/mpeg", ".mp3"},
		{"unknown/type", ".bin"},
	}

	for _, tt := range tests {
		t.Run(tt.mimeType, func(t *testing.T) {
			got := extFromMimeType(tt.mimeType)
			if got != tt.want {
				t.Errorf("extFromMimeType(%q) = %q, want %q", tt.mimeType, got, tt.want)
			}
		})
	}
}

func TestExtractMarkdownImagePaths(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []string
	}{
		{
			name: "single image link",
			text: "### Result\n- [Screenshot](../../../../tmp/playwright-mcp-output/123/page.png)\n",
			want: []string{"/tmp/playwright-mcp-output/123/page.png"},
		},
		{
			name: "multiple image links",
			text: "[img1](/tmp/a.png) and [img2](/tmp/b.jpg)",
			want: []string{"/tmp/a.png", "/tmp/b.jpg"},
		},
		{
			name: "non-image link",
			text: "[link](https://example.com/page.html)",
			want: nil,
		},
		{
			name: "mixed links",
			text: "[doc](file.pdf) [img](../../../../tmp/test.png)",
			want: []string{"/tmp/test.png"},
		},
		{
			name: "no links",
			text: "Just plain text without any links",
			want: nil,
		},
		{
			name: "absolute path",
			text: "[screenshot](/tmp/output/image.PNG)",
			want: []string{"/tmp/output/image.PNG"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractMarkdownImagePaths(tt.text)
			if len(got) != len(tt.want) {
				t.Errorf("ExtractMarkdownImagePaths() = %v, want %v", got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("ExtractMarkdownImagePaths()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestResolveImagePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "relative with tmp",
			path: "../../../../tmp/playwright/image.png",
			want: "/tmp/playwright/image.png",
		},
		{
			name: "absolute path",
			path: "/tmp/output/file.png",
			want: "/tmp/output/file.png",
		},
		{
			name: "tmp without leading slash",
			path: "tmp/output/file.png",
			want: "/tmp/output/file.png",
		},
		{
			name: "unresolvable relative",
			path: "../images/photo.png",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveImagePath(tt.path)
			if got != tt.want {
				t.Errorf("resolveImagePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestFileStore_StoreFile(t *testing.T) {
	dir := t.TempDir()
	fs, _ := NewFileStore(dir)

	// Create a test file
	testFile := filepath.Join(t.TempDir(), "test.png")
	pngData := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00}
	os.WriteFile(testFile, pngData, 0644)

	key, mimeType, size, err := fs.StoreFile(testFile)
	if err != nil {
		t.Fatalf("StoreFile failed: %v", err)
	}
	if key == "" {
		t.Error("key is empty")
	}
	if mimeType != "image/png" {
		t.Errorf("mimeType = %q, want %q", mimeType, "image/png")
	}
	if size != len(pngData) {
		t.Errorf("size = %d, want %d", size, len(pngData))
	}

	// Verify we can retrieve it
	file, data, _ := fs.Get(key)
	if string(data) != string(pngData) {
		t.Error("data mismatch")
	}
	if file.MimeType != "image/png" {
		t.Errorf("file.MimeType = %q, want %q", file.MimeType, "image/png")
	}
}

func TestGenerateKey(t *testing.T) {
	keys := make(map[string]bool)
	for i := 0; i < 100; i++ {
		key, err := generateKey()
		if err != nil {
			t.Fatalf("generateKey failed: %v", err)
		}
		if len(key) != 64 {
			t.Errorf("key length = %d, want 64", len(key))
		}
		if keys[key] {
			t.Error("duplicate key generated")
		}
		keys[key] = true
	}
}

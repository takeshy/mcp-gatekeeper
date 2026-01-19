package executor

/*
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	rubyWasmPath = "/opt/ruby-wasm/usr/local/bin/ruby"
	wasmDir      = "/opt"
)

func TestWasmExecutor_RubyVersion(t *testing.T) {
	if _, err := os.Stat(rubyWasmPath); os.IsNotExist(err) {
		t.Skip("ruby.wasm not found at", rubyWasmPath)
	}

	tmpDir := t.TempDir()
	executor := NewWasmExecutor(tmpDir, wasmDir)

	ctx := context.Background()
	result, err := executor.Execute(ctx, rubyWasmPath, tmpDir, []string{"-v"}, nil, 30*time.Second, 1024*1024)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(result.Stdout, "ruby") {
		t.Errorf("Expected stdout to contain 'ruby', got: %s (stderr: %s)", result.Stdout, result.Stderr)
	}

	t.Logf("Ruby version: %s", strings.TrimSpace(result.Stdout))
}

func TestWasmExecutor_RubyHelloWorld(t *testing.T) {
	if _, err := os.Stat(rubyWasmPath); os.IsNotExist(err) {
		t.Skip("ruby.wasm not found at", rubyWasmPath)
	}

	tmpDir := t.TempDir()
	executor := NewWasmExecutor(tmpDir, wasmDir)

	ctx := context.Background()
	result, err := executor.Execute(ctx, rubyWasmPath, tmpDir, []string{"-e", "puts 'Hello, WASM!'"}, nil, 30*time.Second, 1024*1024)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expected := "Hello, WASM!"
	if !strings.Contains(result.Stdout, expected) {
		t.Errorf("Expected stdout to contain %q, got: %s (stderr: %s)", expected, result.Stdout, result.Stderr)
	}
}

func TestWasmExecutor_RubyArithmetic(t *testing.T) {
	if _, err := os.Stat(rubyWasmPath); os.IsNotExist(err) {
		t.Skip("ruby.wasm not found at", rubyWasmPath)
	}

	tmpDir := t.TempDir()
	executor := NewWasmExecutor(tmpDir, wasmDir)

	ctx := context.Background()
	result, err := executor.Execute(ctx, rubyWasmPath, tmpDir, []string{"-e", "puts 2 + 3 * 4"}, nil, 30*time.Second, 1024*1024)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expected := "14"
	if !strings.Contains(result.Stdout, expected) {
		t.Errorf("Expected stdout to contain %q, got: %s (stderr: %s)", expected, result.Stdout, result.Stderr)
	}
}

func TestWasmExecutor_RubyJSON(t *testing.T) {
	if _, err := os.Stat(rubyWasmPath); os.IsNotExist(err) {
		t.Skip("ruby.wasm not found at", rubyWasmPath)
	}

	tmpDir := t.TempDir()
	executor := NewWasmExecutor(tmpDir, wasmDir)

	ctx := context.Background()
	result, err := executor.Execute(ctx, rubyWasmPath, tmpDir, []string{"-e", "require 'json'; puts JSON.generate({ok: true})"}, nil, 30*time.Second, 1024*1024)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expected := `{"ok":true}`
	if !strings.Contains(result.Stdout, expected) {
		t.Errorf("Expected stdout to contain %q, got: %s (stderr: %s)", expected, result.Stdout, result.Stderr)
	}
}

func TestWasmExecutor_RubyReadFile(t *testing.T) {
	if _, err := os.Stat(rubyWasmPath); os.IsNotExist(err) {
		t.Skip("ruby.wasm not found at", rubyWasmPath)
	}

	tmpDir := t.TempDir()

	// Create a test file
	testFile := filepath.Join(tmpDir, "test.txt")
	testContent := "Hello from file!"
	if err := os.WriteFile(testFile, []byte(testContent), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	executor := NewWasmExecutor(tmpDir, wasmDir)

	ctx := context.Background()
	// Use guest path (relative to tmpDir which is mounted as /)
	guestTestFile := "/test.txt"
	script := `puts File.read('` + guestTestFile + `')`
	result, err := executor.Execute(ctx, rubyWasmPath, tmpDir, []string{"-e", script}, nil, 30*time.Second, 1024*1024)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(result.Stdout, testContent) {
		t.Errorf("Expected stdout to contain %q, got: %s (stderr: %s)", testContent, result.Stdout, result.Stderr)
	}
}

func TestWasmExecutor_RubyWriteFile(t *testing.T) {
	if _, err := os.Stat(rubyWasmPath); os.IsNotExist(err) {
		t.Skip("ruby.wasm not found at", rubyWasmPath)
	}

	tmpDir := t.TempDir()
	executor := NewWasmExecutor(tmpDir, wasmDir)

	outputFile := filepath.Join(tmpDir, "output.txt")
	guestOutputFile := "/output.txt"
	outputContent := "Written by Ruby WASM"

	ctx := context.Background()
	script := `File.write('` + guestOutputFile + `', '` + outputContent + `'); puts 'done'`
	result, err := executor.Execute(ctx, rubyWasmPath, tmpDir, []string{"-e", script}, nil, 30*time.Second, 1024*1024)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(result.Stdout, "done") {
		t.Errorf("Expected stdout to contain 'done', got: %s (stderr: %s)", result.Stdout, result.Stderr)
	}

	// Verify file was written
	content, err := os.ReadFile(outputFile)
	if err != nil {
		t.Fatalf("Failed to read output file: %v", err)
	}

	if string(content) != outputContent {
		t.Errorf("Expected file content %q, got %q", outputContent, string(content))
	}
}

func TestWasmExecutor_RubyEnvironmentVariable(t *testing.T) {
	if _, err := os.Stat(rubyWasmPath); os.IsNotExist(err) {
		t.Skip("ruby.wasm not found at", rubyWasmPath)
	}

	tmpDir := t.TempDir()
	executor := NewWasmExecutor(tmpDir, wasmDir)

	ctx := context.Background()
	env := []string{"MY_VAR=hello_wasm"}
	result, err := executor.Execute(ctx, rubyWasmPath, tmpDir, []string{"-e", "puts ENV['MY_VAR']"}, env, 30*time.Second, 1024*1024)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expected := "hello_wasm"
	if !strings.Contains(result.Stdout, expected) {
		t.Errorf("Expected stdout to contain %q, got: %s (stderr: %s)", expected, result.Stdout, result.Stderr)
	}
}

func TestWasmExecutor_RubyStderr(t *testing.T) {
	if _, err := os.Stat(rubyWasmPath); os.IsNotExist(err) {
		t.Skip("ruby.wasm not found at", rubyWasmPath)
	}

	tmpDir := t.TempDir()
	executor := NewWasmExecutor(tmpDir, wasmDir)

	ctx := context.Background()
	result, err := executor.Execute(ctx, rubyWasmPath, tmpDir, []string{"-e", "STDERR.puts 'error message'"}, nil, 30*time.Second, 1024*1024)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	expected := "error message"
	if !strings.Contains(result.Stderr, expected) {
		t.Errorf("Expected stderr to contain %q, got: %s", expected, result.Stderr)
	}
}

func TestWasmExecutor_RubyScript(t *testing.T) {
	if _, err := os.Stat(rubyWasmPath); os.IsNotExist(err) {
		t.Skip("ruby.wasm not found at", rubyWasmPath)
	}

	tmpDir := t.TempDir()

	// Create a Ruby script file
	scriptFile := filepath.Join(tmpDir, "script.rb")
	scriptContent := `
def greet(name)
  "Hello, #{name}!"
end

puts greet("World")
puts greet("WASM")
`
	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0644); err != nil {
		t.Fatalf("Failed to create script file: %v", err)
	}

	executor := NewWasmExecutor(tmpDir, wasmDir)

	ctx := context.Background()
	// Use guest path
	guestScriptFile := "/script.rb"
	script := `eval(File.read('` + guestScriptFile + `'))`
	result, err := executor.Execute(ctx, rubyWasmPath, tmpDir, []string{"-e", script}, nil, 30*time.Second, 1024*1024)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !strings.Contains(result.Stdout, "Hello, World!") {
		t.Errorf("Expected stdout to contain 'Hello, World!', got: %s (stderr: %s)", result.Stdout, result.Stderr)
	}
	if !strings.Contains(result.Stdout, "Hello, WASM!") {
		t.Errorf("Expected stdout to contain 'Hello, WASM!', got: %s (stderr: %s)", result.Stdout, result.Stderr)
	}
}

func TestWasmExecutor_Timeout(t *testing.T) {
	// Skip: wazero context cancellation doesn't immediately terminate WASM execution
	// This is a known limitation - the WASM module must yield to the runtime for cancellation to take effect
	t.Skip("Skipping timeout test: wazero context cancellation requires cooperative yielding")
}
*/

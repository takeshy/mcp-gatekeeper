package plugin

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SandboxType represents the sandbox mode for a tool
type SandboxType string

const (
	SandboxTypeNone       SandboxType = "none"
	SandboxTypeBubblewrap SandboxType = "bubblewrap"
	SandboxTypeWasm       SandboxType = "wasm"
)

// UIType represents the UI display type for a tool
type UIType string

const (
	UITypeNone  UIType = ""
	UITypeTable UIType = "table"
	UITypeJSON  UIType = "json"
	UITypeLog   UIType = "log"
)

// OutputFormat represents the expected output format of a tool
type OutputFormat string

const (
	OutputFormatNone  OutputFormat = ""
	OutputFormatJSON  OutputFormat = "json"
	OutputFormatCSV   OutputFormat = "csv"
	OutputFormatLines OutputFormat = "lines"
)

// Tool represents a tool definition from a plugin file
type Tool struct {
	Name            string      `json:"name"`
	Description     string      `json:"description"`
	Command         string      `json:"command"`
	ArgsPrefix      []string    `json:"args_prefix,omitempty"` // Fixed arguments prepended to user args
	AllowedArgGlobs []string    `json:"allowed_arg_globs"`
	Sandbox         SandboxType `json:"sandbox"`
	WasmBinary      string      `json:"wasm_binary"`
	// UI settings (optional, for MCP Apps support)
	UIType       UIType       `json:"ui_type,omitempty"`
	OutputFormat OutputFormat `json:"output_format,omitempty"`
	UITemplate   string       `json:"ui_template,omitempty"` // Path to custom HTML template
}

// PluginFile represents the structure of a plugin JSON file
type PluginFile struct {
	Tools          []Tool   `json:"tools"`
	AllowedEnvKeys []string `json:"allowed_env_keys"`
}

// Config holds the loaded plugin configuration
type Config struct {
	Tools          map[string]*Tool // tool name -> tool
	AllowedEnvKeys []string
}

// LoadFromDir loads all plugin files from a directory
// Supports both flat .json files and subdirectories containing plugin.json
func LoadFromDir(dir string) (*Config, error) {
	config := &Config{
		Tools:          make(map[string]*Tool),
		AllowedEnvKeys: []string{},
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read plugins directory: %w", err)
	}

	for _, entry := range entries {
		var filePath string
		var sourceName string

		if entry.IsDir() {
			// Check for plugin.json inside the directory
			pluginPath := filepath.Join(dir, entry.Name(), "plugin.json")
			if _, err := os.Stat(pluginPath); err != nil {
				continue // No plugin.json in this directory
			}
			filePath = pluginPath
			sourceName = entry.Name() + "/plugin.json"
		} else {
			name := entry.Name()
			if !strings.HasSuffix(name, ".json") {
				continue
			}
			filePath = filepath.Join(dir, name)
			sourceName = name
		}

		pluginFile, err := loadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load plugin file %s: %w", sourceName, err)
		}

		// Merge tools
		for i := range pluginFile.Tools {
			tool := &pluginFile.Tools[i]
			if _, exists := config.Tools[tool.Name]; exists {
				return nil, fmt.Errorf("duplicate tool name %q in %s", tool.Name, sourceName)
			}
			// Set default sandbox type
			if tool.Sandbox == "" {
				tool.Sandbox = SandboxTypeNone
			}
			config.Tools[tool.Name] = tool
		}

		// Merge allowed env keys
		config.AllowedEnvKeys = append(config.AllowedEnvKeys, pluginFile.AllowedEnvKeys...)
	}

	return config, nil
}

// LoadFromFile loads a single plugin file
func LoadFromFile(filePath string) (*Config, error) {
	pluginFile, err := loadFile(filePath)
	if err != nil {
		return nil, err
	}

	config := &Config{
		Tools:          make(map[string]*Tool),
		AllowedEnvKeys: pluginFile.AllowedEnvKeys,
	}

	for i := range pluginFile.Tools {
		tool := &pluginFile.Tools[i]
		if _, exists := config.Tools[tool.Name]; exists {
			return nil, fmt.Errorf("duplicate tool name %q", tool.Name)
		}
		// Set default sandbox type
		if tool.Sandbox == "" {
			tool.Sandbox = SandboxTypeNone
		}
		config.Tools[tool.Name] = tool
	}

	return config, nil
}

func loadFile(filePath string) (*PluginFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var pluginFile PluginFile
	if err := json.Unmarshal(data, &pluginFile); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Get the directory of the plugin file for resolving relative paths
	pluginDir := filepath.Dir(filePath)

	// Validate and process tools
	for i := range pluginFile.Tools {
		tool := &pluginFile.Tools[i]
		if tool.Name == "" {
			return nil, fmt.Errorf("tool at index %d has no name", i)
		}
		if tool.Command == "" && tool.Sandbox != SandboxTypeWasm {
			return nil, fmt.Errorf("tool %q has no command", tool.Name)
		}
		if tool.Sandbox == SandboxTypeWasm && tool.WasmBinary == "" {
			return nil, fmt.Errorf("tool %q uses wasm sandbox but has no wasm_binary", tool.Name)
		}

		// Resolve and validate relative template paths
		if tool.UITemplate != "" && !filepath.IsAbs(tool.UITemplate) {
			// Check for parent directory traversal
			if strings.Contains(tool.UITemplate, "..") {
				return nil, fmt.Errorf("tool %q: template path cannot contain '..'", tool.Name)
			}
			resolved := filepath.Join(pluginDir, tool.UITemplate)
			// Verify resolved path is still under plugin directory
			if !strings.HasPrefix(filepath.Clean(resolved), filepath.Clean(pluginDir)) {
				return nil, fmt.Errorf("tool %q: template path escapes plugin directory", tool.Name)
			}
			tool.UITemplate = resolved
		}

		// Resolve and validate relative WASM binary paths
		if tool.WasmBinary != "" && !filepath.IsAbs(tool.WasmBinary) {
			// Check for parent directory traversal
			if strings.Contains(tool.WasmBinary, "..") {
				return nil, fmt.Errorf("tool %q: wasm_binary path cannot contain '..'", tool.Name)
			}
			resolved := filepath.Join(pluginDir, tool.WasmBinary)
			// Verify resolved path is still under plugin directory
			if !strings.HasPrefix(filepath.Clean(resolved), filepath.Clean(pluginDir)) {
				return nil, fmt.Errorf("tool %q: wasm_binary path escapes plugin directory", tool.Name)
			}
			tool.WasmBinary = resolved
		}
	}

	return &pluginFile, nil
}

// GetTool returns a tool by name
func (c *Config) GetTool(name string) *Tool {
	return c.Tools[name]
}

// ListTools returns all tools sorted by name
func (c *Config) ListTools() []*Tool {
	tools := make([]*Tool, 0, len(c.Tools))
	for _, tool := range c.Tools {
		tools = append(tools, tool)
	}
	return tools
}

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

// Tool represents a tool definition from a plugin file
type Tool struct {
	Name            string      `json:"name"`
	Description     string      `json:"description"`
	Command         string      `json:"command"`
	AllowedArgGlobs []string    `json:"allowed_arg_globs"`
	Sandbox         SandboxType `json:"sandbox"`
	WasmBinary      string      `json:"wasm_binary"`
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
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}

		filePath := filepath.Join(dir, name)
		pluginFile, err := loadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to load plugin file %s: %w", name, err)
		}

		// Merge tools
		for i := range pluginFile.Tools {
			tool := &pluginFile.Tools[i]
			if _, exists := config.Tools[tool.Name]; exists {
				return nil, fmt.Errorf("duplicate tool name %q in %s", tool.Name, name)
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

	// Validate tools
	for i, tool := range pluginFile.Tools {
		if tool.Name == "" {
			return nil, fmt.Errorf("tool at index %d has no name", i)
		}
		if tool.Command == "" && tool.Sandbox != SandboxTypeWasm {
			return nil, fmt.Errorf("tool %q has no command", tool.Name)
		}
		if tool.Sandbox == SandboxTypeWasm && tool.WasmBinary == "" {
			return nil, fmt.Errorf("tool %q uses wasm sandbox but has no wasm_binary", tool.Name)
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

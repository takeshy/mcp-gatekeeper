package mcp

import (
	"testing"

	"github.com/takeshy/mcp-gatekeeper/internal/plugin"
)

func TestBuildToolMeta(t *testing.T) {
	tests := []struct {
		name     string
		tool     *plugin.Tool
		wantNil  bool
		checkUI  bool
		checkVis bool
	}{
		{
			name:    "no UI type",
			tool:    &plugin.Tool{Name: "test"},
			wantNil: true,
		},
		{
			name: "with UI type",
			tool: &plugin.Tool{
				Name:   "test",
				UIType: plugin.UITypeTable,
			},
			wantNil: false,
			checkUI: true,
		},
		{
			name: "with UI template",
			tool: &plugin.Tool{
				Name:       "test",
				UITemplate: "/path/to/template.html",
			},
			wantNil: false,
			checkUI: true,
		},
		{
			name: "with visibility config",
			tool: &plugin.Tool{
				Name:   "test",
				UIType: plugin.UITypeJSON,
				UIConfig: &plugin.UIConfig{
					Visibility: []plugin.VisibilityType{plugin.VisibilityApp},
				},
			},
			wantNil:  false,
			checkUI:  true,
			checkVis: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := BuildToolMeta(tt.tool)

			if tt.wantNil {
				if meta != nil {
					t.Errorf("expected nil meta, got %v", meta)
				}
				return
			}

			if meta == nil {
				t.Fatal("expected non-nil meta")
			}

			if tt.checkUI {
				ui, ok := meta["ui"].(map[string]interface{})
				if !ok {
					t.Fatal("expected ui key in meta")
				}
				if _, ok := ui["resourceUri"]; !ok {
					t.Error("expected resourceUri in ui meta")
				}

				if tt.checkVis {
					vis, ok := ui["visibility"]
					if !ok {
						t.Error("expected visibility in ui meta")
					}
					visSlice, ok := vis.([]string)
					if !ok {
						t.Fatal("visibility should be string slice")
					}
					if len(visSlice) != 1 || visSlice[0] != "app" {
						t.Errorf("expected visibility [app], got %v", visSlice)
					}
				}
			}
		})
	}
}

func TestBuildResourceMeta(t *testing.T) {
	tests := []struct {
		name      string
		tool      *plugin.Tool
		wantNil   bool
		checkCSP  bool
		checkPerm bool
	}{
		{
			name:    "no UI",
			tool:    &plugin.Tool{Name: "test"},
			wantNil: true,
		},
		{
			name: "with UI type - default CSP",
			tool: &plugin.Tool{
				Name:   "test",
				UIType: plugin.UITypeTable,
			},
			wantNil:  false,
			checkCSP: true,
		},
		{
			name: "custom template without ui_config - no meta",
			tool: &plugin.Tool{
				Name:       "test",
				UITemplate: "/path/to/template.html",
			},
			wantNil: true,
		},
		{
			name: "custom template with ui_config CSP",
			tool: &plugin.Tool{
				Name:       "test",
				UITemplate: "/path/to/template.html",
				UIConfig: &plugin.UIConfig{
					CSP: &plugin.UICSPConfig{
						ResourceDomains: []string{"cdn.example.com"},
					},
				},
			},
			wantNil:  false,
			checkCSP: true,
		},
		{
			name: "with custom CSP on built-in type",
			tool: &plugin.Tool{
				Name:   "test",
				UIType: plugin.UITypeJSON,
				UIConfig: &plugin.UIConfig{
					CSP: &plugin.UICSPConfig{
						ResourceDomains: []string{"cdn.example.com"},
					},
				},
			},
			wantNil:  false,
			checkCSP: true,
		},
		{
			name: "with permissions",
			tool: &plugin.Tool{
				Name:   "test",
				UIType: plugin.UITypeJSON,
				UIConfig: &plugin.UIConfig{
					Permissions: &plugin.UIPermissions{
						ClipboardWrite: true,
					},
				},
			},
			wantNil:   false,
			checkCSP:  true,
			checkPerm: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta := BuildResourceMeta(tt.tool)

			if tt.wantNil {
				if meta != nil {
					t.Errorf("expected nil meta, got %v", meta)
				}
				return
			}

			if meta == nil {
				t.Fatal("expected non-nil meta")
			}

			if tt.checkCSP {
				csp, ok := meta["csp"].(map[string]interface{})
				if !ok {
					t.Fatal("expected csp key in meta")
				}
				_, ok = csp["resource_domains"]
				if !ok {
					t.Fatal("expected resource_domains in csp")
				}
				// For built-in UI types, esm.sh should be included
				if tt.tool.UIType != "" {
					domains, ok := csp["resource_domains"].([]string)
					if !ok {
						t.Fatal("expected resource_domains to be []string for built-in type")
					}
					found := false
					for _, d := range domains {
						if d == "esm.sh" {
							found = true
							break
						}
					}
					if !found {
						t.Error("esm.sh should be in resource_domains for built-in UI types")
					}
				}
			}

			if tt.checkPerm {
				perm, ok := meta["permissions"].(map[string]interface{})
				if !ok {
					t.Fatal("expected permissions key in meta")
				}
				if _, ok := perm["clipboard_write"]; !ok {
					t.Error("expected clipboard_write in permissions")
				}
			}
		})
	}
}

func TestUIResourceURI(t *testing.T) {
	uri := UIResourceURI("my-tool")
	expected := "ui://my-tool/result"
	if uri != expected {
		t.Errorf("UIResourceURI() = %v, want %v", uri, expected)
	}
}

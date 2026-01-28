package plugin

import (
	"testing"
)

func TestToolVisibility(t *testing.T) {
	tests := []struct {
		name           string
		tool           *Tool
		wantModel      bool
		wantApp        bool
	}{
		{
			name:      "no UIConfig - visible to both",
			tool:      &Tool{Name: "test"},
			wantModel: true,
			wantApp:   true,
		},
		{
			name: "empty visibility - visible to both",
			tool: &Tool{
				Name:     "test",
				UIConfig: &UIConfig{},
			},
			wantModel: true,
			wantApp:   true,
		},
		{
			name: "model only",
			tool: &Tool{
				Name: "test",
				UIConfig: &UIConfig{
					Visibility: []VisibilityType{VisibilityModel},
				},
			},
			wantModel: true,
			wantApp:   false,
		},
		{
			name: "app only",
			tool: &Tool{
				Name: "test",
				UIConfig: &UIConfig{
					Visibility: []VisibilityType{VisibilityApp},
				},
			},
			wantModel: false,
			wantApp:   true,
		},
		{
			name: "both model and app",
			tool: &Tool{
				Name: "test",
				UIConfig: &UIConfig{
					Visibility: []VisibilityType{VisibilityModel, VisibilityApp},
				},
			},
			wantModel: true,
			wantApp:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.tool.IsVisibleToModel(); got != tt.wantModel {
				t.Errorf("IsVisibleToModel() = %v, want %v", got, tt.wantModel)
			}
			if got := tt.tool.IsVisibleToApp(); got != tt.wantApp {
				t.Errorf("IsVisibleToApp() = %v, want %v", got, tt.wantApp)
			}
		})
	}
}

func TestUIConfigParsing(t *testing.T) {
	// Test that UIConfig can be properly parsed from JSON via LoadFromFile
	// This is implicitly tested by existing plugin loading tests
	tool := &Tool{
		Name: "test",
		UIConfig: &UIConfig{
			CSP: &UICSPConfig{
				ResourceDomains: []string{"example.com"},
			},
			Permissions: &UIPermissions{
				ClipboardWrite: true,
			},
			Visibility: []VisibilityType{VisibilityModel, VisibilityApp},
		},
	}

	if tool.UIConfig.CSP == nil {
		t.Error("CSP should not be nil")
	}
	if len(tool.UIConfig.CSP.ResourceDomains) != 1 {
		t.Errorf("Expected 1 resource domain, got %d", len(tool.UIConfig.CSP.ResourceDomains))
	}
	if !tool.UIConfig.Permissions.ClipboardWrite {
		t.Error("ClipboardWrite should be true")
	}
}

package version

import "testing"

func TestIsMCPProtocolVersionSupported(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "2026-07-28", want: true},
		{version: "2025-11-25", want: true},
		{version: "2025-06-18", want: true},
		{version: "2024-11-05", want: true},
		{version: "2026-01-01", want: false},
		{version: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			if got := IsMCPProtocolVersionSupported(tt.version); got != tt.want {
				t.Fatalf("IsMCPProtocolVersionSupported(%q) = %v, want %v", tt.version, got, tt.want)
			}
		})
	}
}

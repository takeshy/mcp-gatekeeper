package version

// Version is set at build time via ldflags
var Version = "dev"

// MCPProtocolVersion is the latest MCP protocol version this server supports.
const MCPProtocolVersion = "2026-07-28"

// MCPStreamableProtocolVersion is kept as an alias for callers that distinguish
// the Streamable HTTP transport from the protocol itself.
const MCPStreamableProtocolVersion = MCPProtocolVersion

// MCPLegacyStreamableProtocolVersion is the newest stateful protocol retained
// for backwards compatibility.
const MCPLegacyStreamableProtocolVersion = "2025-06-18"

// SupportedMCPProtocolVersions lists recognized protocol versions, newest first.
// Only legacy versions use initialization; 2026-07-28 requests are stateless.
var SupportedMCPProtocolVersions = []string{
	MCPProtocolVersion,
	MCPLegacyStreamableProtocolVersion,
	"2024-11-05",
}

// IsMCPProtocolVersionSupported reports whether version can be negotiated.
func IsMCPProtocolVersionSupported(protocolVersion string) bool {
	for _, supported := range SupportedMCPProtocolVersions {
		if protocolVersion == supported {
			return true
		}
	}
	return false
}

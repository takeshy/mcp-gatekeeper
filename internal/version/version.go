package version

// Version is set at build time via ldflags
var Version = "dev"

// MCPProtocolVersion is the MCP protocol version this server supports
const MCPProtocolVersion = "2024-11-05"

// MCPStreamableProtocolVersion is the MCP Streamable HTTP protocol version
const MCPStreamableProtocolVersion = "2025-06-18"

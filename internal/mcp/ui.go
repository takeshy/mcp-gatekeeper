package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"html/template"
	"os"
	"strings"

	"github.com/takeshy/mcp-gatekeeper/internal/plugin"
)

// UIResourceURI generates a UI resource URI for a tool
func UIResourceURI(toolName string) string {
	return fmt.Sprintf("ui://%s/result", toolName)
}

// BuildToolMeta creates _meta object for a tool with UI support
func BuildToolMeta(tool *plugin.Tool) map[string]interface{} {
	if tool.UIType == "" && tool.UITemplate == "" {
		return nil
	}

	uiMeta := map[string]interface{}{
		"resourceUri": UIResourceURI(tool.Name),
	}

	// Add visibility if configured
	if tool.UIConfig != nil && len(tool.UIConfig.Visibility) > 0 {
		visibility := make([]string, len(tool.UIConfig.Visibility))
		for i, v := range tool.UIConfig.Visibility {
			visibility[i] = string(v)
		}
		uiMeta["visibility"] = visibility
	}

	return map[string]interface{}{
		"ui": uiMeta,
	}
}

// BuildResourceMeta creates _meta object for a UI resource with CSP and permissions
func BuildResourceMeta(tool *plugin.Tool) map[string]interface{} {
	if tool.UIType == "" && tool.UITemplate == "" {
		return nil
	}

	// For custom templates without ui_config, return nil (no changes to existing behavior)
	if tool.UITemplate != "" && tool.UIConfig == nil {
		return nil
	}

	meta := make(map[string]interface{})

	// For built-in UI types, add default CSP allowing esm.sh for App SDK
	if tool.UIType != "" {
		csp := map[string]interface{}{
			"resource_domains": []string{"esm.sh"},
		}

		// Merge with custom CSP if provided
		if tool.UIConfig != nil && tool.UIConfig.CSP != nil {
			if len(tool.UIConfig.CSP.ResourceDomains) > 0 {
				// Combine default and custom domains, ensuring esm.sh is included
				domains := []string{"esm.sh"}
				for _, d := range tool.UIConfig.CSP.ResourceDomains {
					if d != "esm.sh" {
						domains = append(domains, d)
					}
				}
				csp["resource_domains"] = domains
			}
		}
		meta["csp"] = csp
	} else if tool.UIConfig != nil && tool.UIConfig.CSP != nil {
		// For custom templates with explicit CSP config
		csp := map[string]interface{}{
			"resource_domains": tool.UIConfig.CSP.ResourceDomains,
		}
		meta["csp"] = csp
	}

	// Add permissions if configured
	if tool.UIConfig != nil && tool.UIConfig.Permissions != nil {
		permissions := make(map[string]interface{})
		if tool.UIConfig.Permissions.ClipboardWrite {
			permissions["clipboard_write"] = true
		}
		if len(permissions) > 0 {
			meta["permissions"] = permissions
		}
	}

	// Return nil if meta is empty
	if len(meta) == 0 {
		return nil
	}

	return meta
}

// BuildResultMeta creates _meta object for a tool call result with output data
func BuildResultMeta(tool *plugin.Tool, output string) map[string]interface{} {
	if tool.UIType == "" && tool.UITemplate == "" {
		return nil
	}

	// Encode output in base64 for URL safety
	encodedOutput := base64.URLEncoding.EncodeToString([]byte(output))

	return map[string]interface{}{
		"ui": map[string]interface{}{
			"resourceUri": fmt.Sprintf("%s?data=%s", UIResourceURI(tool.Name), encodedOutput),
		},
	}
}

// GenerateUIHTML generates HTML for a tool's UI based on its type and output
// sessionID is the MCP Streamable HTTP session ID (empty if not using streamable)
func GenerateUIHTML(tool *plugin.Tool, encodedData string, sessionID string) (string, error) {
	// Decode the data
	data, err := base64.URLEncoding.DecodeString(encodedData)
	if err != nil {
		return "", fmt.Errorf("failed to decode data: %w", err)
	}
	output := string(data)

	// If custom template is specified, use it
	if tool.UITemplate != "" {
		return generateCustomUI(tool.UITemplate, output, sessionID)
	}

	switch tool.UIType {
	case plugin.UITypeTable:
		return generateTableUI(tool, output, sessionID)
	case plugin.UITypeJSON:
		return generateJSONUI(output, sessionID)
	case plugin.UITypeLog:
		return generateLogUI(output, sessionID)
	default:
		return generateDefaultUI(output)
	}
}

// TemplateData is passed to custom templates
type TemplateData struct {
	Output     string      // Raw output string
	Lines      []string    // Output split by lines
	JSON       interface{} // Parsed JSON (if valid)
	JSONPretty string      // Pretty-printed JSON (if valid)
	IsJSON     bool        // Whether output is valid JSON
	SessionID  string      // MCP Streamable HTTP session ID (empty if not using streamable)
}

func generateCustomUI(templatePath string, output string, sessionID string) (string, error) {
	// Read template file
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template file: %w", err)
	}

	// Parse template
	tmpl, err := template.New("ui").Funcs(template.FuncMap{
		"escape": html.EscapeString,
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"jsonPretty": func(v interface{}) template.JS {
			b, _ := json.MarshalIndent(v, "", "  ")
			return template.JS(b)
		},
		"split": strings.Split,
		"join":  strings.Join,
		"slice": func(s []string, start int) []string {
			if start >= len(s) {
				return []string{}
			}
			return s[start:]
		},
		"first": func(s []string) string {
			if len(s) == 0 {
				return ""
			}
			return s[0]
		},
		"contains":  strings.Contains,
		"hasPrefix": strings.HasPrefix,
		"trimSpace": strings.TrimSpace,
	}).Parse(string(tmplContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Prepare template data
	data := TemplateData{
		Output:    output,
		Lines:     strings.Split(output, "\n"),
		SessionID: sessionID,
	}

	// Try to parse as JSON
	var jsonData interface{}
	if err := json.Unmarshal([]byte(output), &jsonData); err == nil {
		data.JSON = jsonData
		data.IsJSON = true
		if prettyJSON, err := json.MarshalIndent(jsonData, "", "  "); err == nil {
			data.JSONPretty = string(prettyJSON)
		}
	}

	// Execute template
	var sb strings.Builder
	if err := tmpl.Execute(&sb, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return sb.String(), nil
}

func generateTableUI(tool *plugin.Tool, output string, sessionID string) (string, error) {
	var rows [][]string
	var headers []string

	switch tool.OutputFormat {
	case plugin.OutputFormatJSON:
		// Parse JSON array
		var jsonData []map[string]interface{}
		if err := json.Unmarshal([]byte(output), &jsonData); err != nil {
			// Try single object
			var singleObj map[string]interface{}
			if err := json.Unmarshal([]byte(output), &singleObj); err != nil {
				return generateDefaultUI(output)
			}
			jsonData = []map[string]interface{}{singleObj}
		}

		if len(jsonData) > 0 {
			// Extract headers from first object
			for key := range jsonData[0] {
				headers = append(headers, key)
			}
			// Extract rows
			for _, obj := range jsonData {
				row := make([]string, len(headers))
				for i, h := range headers {
					if v, ok := obj[h]; ok {
						row[i] = fmt.Sprintf("%v", v)
					}
				}
				rows = append(rows, row)
			}
		}

	case plugin.OutputFormatCSV:
		lines := strings.Split(strings.TrimSpace(output), "\n")
		if len(lines) > 0 {
			headers = strings.Split(lines[0], ",")
			for _, line := range lines[1:] {
				rows = append(rows, strings.Split(line, ","))
			}
		}

	case plugin.OutputFormatLines:
		// Parse space-separated lines (like ls -la output)
		lines := strings.Split(strings.TrimSpace(output), "\n")
		for _, line := range lines {
			fields := strings.Fields(line)
			if len(fields) > 0 {
				rows = append(rows, fields)
			}
		}
		// Auto-detect headers based on content or use generic
		if len(rows) > 0 {
			headers = make([]string, len(rows[0]))
			for i := range headers {
				headers[i] = fmt.Sprintf("Col %d", i+1)
			}
		}

	default:
		return generateDefaultUI(output)
	}

	return buildTableHTML(headers, rows, string(tool.OutputFormat), sessionID), nil
}

func buildTableHTML(headers []string, rows [][]string, outputFormat string, sessionID string) string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 16px; background: #f5f5f5; }
.toolbar { padding: 12px 16px; background: #fff; border-radius: 8px 8px 0 0; box-shadow: 0 2px 4px rgba(0,0,0,0.1); display: flex; gap: 8px; align-items: center; }
.toolbar button { padding: 8px 16px; border: none; border-radius: 6px; cursor: pointer; font-size: 14px; display: flex; align-items: center; gap: 6px; }
.toolbar button.primary { background: #4CAF50; color: white; }
.toolbar button.primary:hover { background: #45a049; }
.toolbar button:disabled { opacity: 0.6; cursor: not-allowed; }
.toolbar .status { margin-left: auto; font-size: 12px; color: #666; }
.container { background: white; border-radius: 0 0 8px 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); overflow: hidden; }
table { border-collapse: collapse; width: 100%; }
th, td { border-bottom: 1px solid #e0e0e0; padding: 12px 16px; text-align: left; }
th { background: #f8f9fa; font-weight: 600; cursor: pointer; user-select: none; }
th:hover { background: #e9ecef; }
tr:hover td { background: #f8f9fa; }
.sort-icon { margin-left: 4px; opacity: 0.5; }
.spinner { width: 14px; height: 14px; border: 2px solid #fff; border-top-color: transparent; border-radius: 50%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
</head>
<body>
<div class="toolbar">
  <button id="refresh-btn" class="primary" onclick="refresh()">
    <span id="refresh-icon">&#x21bb;</span> Refresh
  </button>
  <span id="status" class="status"></span>
</div>
<div class="container">
<table id="data-table">
<thead><tr>`)

	for i, h := range headers {
		sb.WriteString(fmt.Sprintf(`<th onclick="sortTable(%d)">%s<span class="sort-icon">↕</span></th>`, i, html.EscapeString(h)))
	}

	sb.WriteString(`</tr></thead><tbody>`)

	for _, row := range rows {
		sb.WriteString("<tr>")
		for _, cell := range row {
			sb.WriteString(fmt.Sprintf("<td>%s</td>", html.EscapeString(cell)))
		}
		sb.WriteString("</tr>")
	}

	sb.WriteString(fmt.Sprintf(`</tbody></table></div>
<script type="module">
const sessionId = %q;
const outputFormat = %q;

// MCP Apps compatibility layer
let mcpClient = null;

async function initMcpClient() {
  // Check for injected bridge first (obsidian-gemini-helper)
  if (window.mcpApps && typeof window.mcpApps.callTool === 'function') {
    const opts = sessionId ? { sessionId } : undefined;
    return {
      callServerTool: (name, args) => window.mcpApps.callTool(name, args, opts),
      type: 'bridge'
    };
  }

  // Fall back to MCP App SDK
  try {
    const { App } = await import('https://esm.sh/@anthropic-ai/mcp-app-sdk@0.1');
    const app = new App({ name: 'Table UI', version: '1.0.0' });
    await app.connect(sessionId ? { sessionId } : undefined);
    return {
      callServerTool: (name, args) => app.callServerTool(name, args),
      context: app.context,
      type: 'sdk'
    };
  } catch (e) {
    console.log('MCP App SDK not available:', e.message);
    return null;
  }
}

let currentToolName = null;
let currentArgs = {};

async function initApp() {
  try {
    mcpClient = await initMcpClient();
    if (!mcpClient) {
      setStatus('Standalone mode');
      return;
    }

    if (mcpClient.context?.toolName) {
      currentToolName = mcpClient.context.toolName;
      currentArgs = mcpClient.context.arguments || {};
    }

    setStatus('Connected (' + mcpClient.type + ')');
  } catch (e) {
    console.log('Init error:', e.message);
    setStatus('Standalone mode');
  }
}

function setStatus(msg) {
  document.getElementById('status').textContent = msg;
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function parseTable(text) {
  const trimmed = text.trim();
  if (!trimmed) {
    return { headers: [], rows: [] };
  }

  if (outputFormat === 'json') {
    try {
      const data = JSON.parse(text);
      let headers = [];
      let rows = [];

      if (Array.isArray(data)) {
        if (data.length === 0) {
          return { headers: [], rows: [] };
        }
        if (Array.isArray(data[0])) {
          headers = data[0].map((_, i) => 'Col ' + (i + 1));
          rows = data.map(row => row.map(cell => cell == null ? '' : String(cell)));
        } else if (data[0] && typeof data[0] === 'object') {
          headers = Object.keys(data[0]);
          rows = data.map(obj => headers.map(h => obj && obj[h] != null ? String(obj[h]) : ''));
        } else {
          headers = ['Value'];
          rows = data.map(v => [v == null ? '' : String(v)]);
        }
      } else if (data && typeof data === 'object') {
        headers = Object.keys(data);
        rows = [headers.map(h => data[h] != null ? String(data[h]) : '')];
      } else {
        headers = ['Value'];
        rows = [[data == null ? '' : String(data)]];
      }

      return { headers, rows };
    } catch (e) {
      // Fall through to lines parsing
    }
  }

  if (outputFormat === 'csv') {
    const lines = trimmed.split(/\r?\n/);
    if (lines.length === 0) {
      return { headers: [], rows: [] };
    }
    const headers = lines[0].split(',');
    const rows = lines.slice(1).map(line => line.split(','));
    return { headers, rows };
  }

  const lines = trimmed.split(/\r?\n/).filter(line => line.trim());
  const rows = lines.map(line => line.trim().split(/\s+/));
  let headers = [];
  if (rows.length > 0) {
    headers = Array.from({ length: rows[0].length }, (_, i) => 'Col ' + (i + 1));
  }
  return { headers, rows };
}

async function refresh() {
  if (!mcpClient || !currentToolName) {
    setStatus('Refresh not available');
    return;
  }

  const btn = document.getElementById('refresh-btn');
  const icon = document.getElementById('refresh-icon');
  btn.disabled = true;
  icon.innerHTML = '<div class="spinner"></div>';
  setStatus('Refreshing...');

  try {
    const result = await mcpClient.callServerTool(currentToolName, currentArgs);
    if (result?.content?.[0]?.text) {
      updateTableFromText(result.content[0].text);
      setStatus('Updated at ' + new Date().toLocaleTimeString());
    } else if (result?.isError) {
      setStatus('Error: ' + (result.content?.[0]?.text || 'Unknown error'));
    }
  } catch (e) {
    setStatus('Error: ' + e.message);
  } finally {
    btn.disabled = false;
    icon.innerHTML = '&#x21bb;';
  }
}

function updateTableFromText(text) {
  const parsed = parseTable(text);
  const tbody = document.querySelector('#data-table tbody');
  const theadRow = document.querySelector('#data-table thead tr');

  sortDir = {};
  theadRow.innerHTML = parsed.headers.map((h, i) =>
    '<th onclick="sortTable(' + i + ')">' + escapeHtml(h) + '<span class="sort-icon">↕</span></th>'
  ).join('');

  if (parsed.rows.length === 0) {
    tbody.innerHTML = '';
    return;
  }

  tbody.innerHTML = parsed.rows.map(row =>
    '<tr>' + row.map(c => '<td>' + escapeHtml(c) + '</td>').join('') + '</tr>'
  ).join('');
}

window.refresh = refresh;

let sortDir = {};
window.sortTable = function(col) {
  const table = document.getElementById('data-table');
  const tbody = table.querySelector('tbody');
  const rows = Array.from(tbody.querySelectorAll('tr'));
  sortDir[col] = !sortDir[col];
  rows.sort((a, b) => {
    const aVal = a.cells[col]?.textContent || '';
    const bVal = b.cells[col]?.textContent || '';
    const aNum = parseFloat(aVal), bNum = parseFloat(bVal);
    if (!isNaN(aNum) && !isNaN(bNum)) {
      return sortDir[col] ? aNum - bNum : bNum - aNum;
    }
    return sortDir[col] ? aVal.localeCompare(bVal) : bVal.localeCompare(aVal);
  });
  rows.forEach(row => tbody.appendChild(row));
};

initApp();
</script>
</body></html>`, sessionID, outputFormat))

	return sb.String()
}

func generateJSONUI(output string, sessionID string) (string, error) {
	// Pretty print JSON
	var parsed interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return generateDefaultUI(output)
	}

	prettyJSON, _ := json.MarshalIndent(parsed, "", "  ")

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: 'Monaco', 'Menlo', monospace; padding: 16px; background: #1e1e1e; color: #d4d4d4; }
.toolbar { padding: 12px 16px; background: #333; border-radius: 8px 8px 0 0; display: flex; gap: 8px; align-items: center; margin-bottom: 0; }
.toolbar button { padding: 8px 16px; border: none; border-radius: 6px; cursor: pointer; font-size: 14px; display: flex; align-items: center; gap: 6px; background: #4a4a4a; color: #fff; }
.toolbar button:hover { background: #5a5a5a; }
.toolbar button.primary { background: #4CAF50; }
.toolbar button.primary:hover { background: #45a049; }
.toolbar button:disabled { opacity: 0.6; cursor: not-allowed; }
.toolbar .status { margin-left: auto; font-size: 12px; color: #888; }
.json-container { background: #1e1e1e; border-radius: 0 0 8px 8px; padding: 16px; }
pre { white-space: pre-wrap; word-wrap: break-word; line-height: 1.5; margin: 0; }
.string { color: #ce9178; }
.number { color: #b5cea8; }
.boolean { color: #569cd6; }
.null { color: #569cd6; }
.key { color: #9cdcfe; }
.spinner { width: 14px; height: 14px; border: 2px solid #fff; border-top-color: transparent; border-radius: 50%%; animation: spin 0.8s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
.copied { background: #45a049 !important; }
</style>
</head>
<body>
<div class="toolbar">
  <button id="refresh-btn" class="primary" onclick="refresh()">
    <span id="refresh-icon">&#x21bb;</span> Refresh
  </button>
  <button id="copy-btn" onclick="copyToClipboard()">
    <span id="copy-icon">&#x2398;</span> Copy
  </button>
  <span id="status" class="status"></span>
</div>
<div class="json-container">
<pre id="json">%s</pre>
</div>
<script type="module">
const sessionId = %q;

// MCP Apps compatibility layer
let mcpClient = null;

async function initMcpClient() {
  if (window.mcpApps && typeof window.mcpApps.callTool === 'function') {
    const opts = sessionId ? { sessionId } : undefined;
    return {
      callServerTool: (name, args) => window.mcpApps.callTool(name, args, opts),
      type: 'bridge'
    };
  }
  try {
    const { App } = await import('https://esm.sh/@anthropic-ai/mcp-app-sdk@0.1');
    const app = new App({ name: 'JSON UI', version: '1.0.0' });
    await app.connect(sessionId ? { sessionId } : undefined);
    return {
      callServerTool: (name, args) => app.callServerTool(name, args),
      context: app.context,
      type: 'sdk'
    };
  } catch (e) {
    console.log('MCP App SDK not available:', e.message);
    return null;
  }
}

let currentToolName = null;
let currentArgs = {};
let currentJSON = document.getElementById('json').textContent;

async function initApp() {
  try {
    mcpClient = await initMcpClient();
    if (!mcpClient) {
      setStatus('Standalone mode');
      return;
    }

    if (mcpClient.context?.toolName) {
      currentToolName = mcpClient.context.toolName;
      currentArgs = mcpClient.context.arguments || {};
    }

    setStatus('Connected (' + mcpClient.type + ')');
  } catch (e) {
    console.log('Init error:', e.message);
    setStatus('Standalone mode');
  }
}

function setStatus(msg) {
  document.getElementById('status').textContent = msg;
}

function escapeHtml(str) {
  return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}

async function refresh() {
  if (!mcpClient || !currentToolName) {
    setStatus('Refresh not available');
    return;
  }

  const btn = document.getElementById('refresh-btn');
  const icon = document.getElementById('refresh-icon');
  btn.disabled = true;
  icon.innerHTML = '<div class="spinner"></div>';
  setStatus('Refreshing...');

  try {
    const result = await mcpClient.callServerTool(currentToolName, currentArgs);
    if (result?.content?.[0]?.text) {
      updateJSON(result.content[0].text);
      setStatus('Updated at ' + new Date().toLocaleTimeString());
    } else if (result?.isError) {
      setStatus('Error: ' + (result.content?.[0]?.text || 'Unknown error'));
    }
  } catch (e) {
    setStatus('Error: ' + e.message);
  } finally {
    btn.disabled = false;
    icon.innerHTML = '&#x21bb;';
  }
}

function updateJSON(text) {
  try {
    const parsed = JSON.parse(text);
    currentJSON = JSON.stringify(parsed, null, 2);
    document.getElementById('json').innerHTML = syntaxHighlight(currentJSON);
  } catch (e) {
    currentJSON = text;
    document.getElementById('json').textContent = text;
  }
}

async function copyToClipboard() {
  const btn = document.getElementById('copy-btn');
  try {
    await navigator.clipboard.writeText(currentJSON);
    btn.classList.add('copied');
    btn.innerHTML = '<span>&#x2713;</span> Copied!';
    setTimeout(() => {
      btn.classList.remove('copied');
      btn.innerHTML = '<span id="copy-icon">&#x2398;</span> Copy';
    }, 2000);
  } catch (e) {
    setStatus('Copy failed: ' + e.message);
  }
}

window.refresh = refresh;
window.copyToClipboard = copyToClipboard;

function syntaxHighlight(json) {
  const escaped = escapeHtml(json);
  return escaped.replace(/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?)/g, function (match) {
    let cls = 'number';
    if (/^"/.test(match)) {
      if (/:$/.test(match)) { cls = 'key'; }
      else { cls = 'string'; }
    } else if (/true|false/.test(match)) { cls = 'boolean'; }
    else if (/null/.test(match)) { cls = 'null'; }
    return '<span class="' + cls + '">' + match + '</span>';
  });
}

document.getElementById('json').innerHTML = syntaxHighlight(document.getElementById('json').textContent);
initApp();
</script>
</body></html>`, html.EscapeString(string(prettyJSON)), sessionID), nil
}

func generateLogUI(output string, sessionID string) (string, error) {
	lines := strings.Split(output, "\n")

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: 'Monaco', 'Menlo', monospace; font-size: 12px; background: #1e1e1e; color: #d4d4d4; }
.toolbar { padding: 12px 16px; background: #333; border-bottom: 1px solid #444; position: sticky; top: 0; display: flex; gap: 12px; align-items: center; flex-wrap: wrap; }
.toolbar input[type="text"] { padding: 6px 10px; border: 1px solid #555; background: #2d2d2d; color: #fff; border-radius: 4px; width: 250px; }
.toolbar button { padding: 6px 14px; border: none; border-radius: 4px; cursor: pointer; font-size: 12px; display: flex; align-items: center; gap: 6px; background: #4a4a4a; color: #fff; }
.toolbar button:hover { background: #5a5a5a; }
.toolbar button.primary { background: #4CAF50; }
.toolbar button.primary:hover { background: #45a049; }
.toolbar button:disabled { opacity: 0.6; cursor: not-allowed; }
.toolbar .auto-refresh { display: flex; align-items: center; gap: 6px; color: #888; font-size: 12px; }
.toolbar .auto-refresh input[type="checkbox"] { cursor: pointer; }
.toolbar .status { margin-left: auto; font-size: 11px; color: #888; }
.log-container { padding: 16px; max-height: calc(100vh - 60px); overflow-y: auto; }
.line { padding: 2px 0; white-space: pre-wrap; word-wrap: break-word; }
.line:hover { background: #2d2d2d; }
.line-num { color: #858585; margin-right: 16px; user-select: none; display: inline-block; min-width: 40px; text-align: right; }
.hidden { display: none; }
.error { color: #f48771; }
.warn { color: #cca700; }
.info { color: #3794ff; }
.spinner { width: 12px; height: 12px; border: 2px solid #fff; border-top-color: transparent; border-radius: 50%; animation: spin 0.8s linear infinite; display: inline-block; }
@keyframes spin { to { transform: rotate(360deg); } }
</style>
</head>
<body>
<div class="toolbar">
  <button id="refresh-btn" class="primary" onclick="refresh()">
    <span id="refresh-icon">&#x21bb;</span> Refresh
  </button>
  <input type="text" id="filter" placeholder="Filter logs..." oninput="filterLogs()">
  <label class="auto-refresh">
    <input type="checkbox" id="auto-refresh" onchange="toggleAutoRefresh()">
    Auto-refresh (5s)
  </label>
  <span id="status" class="status"></span>
</div>
<div class="log-container" id="logs">`)

	for i, line := range lines {
		cls := "line"
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") {
			cls += " error"
		} else if strings.Contains(lower, "warn") {
			cls += " warn"
		} else if strings.Contains(lower, "info") {
			cls += " info"
		}
		sb.WriteString(fmt.Sprintf(`<div class="%s"><span class="line-num">%d</span>%s</div>`, cls, i+1, html.EscapeString(line)))
	}

	sb.WriteString(fmt.Sprintf(`</div>
<script type="module">
const sessionId = %q;

// MCP Apps compatibility layer
let mcpClient = null;

async function initMcpClient() {
  if (window.mcpApps && typeof window.mcpApps.callTool === 'function') {
    const opts = sessionId ? { sessionId } : undefined;
    return {
      callServerTool: (name, args) => window.mcpApps.callTool(name, args, opts),
      type: 'bridge'
    };
  }
  try {
    const { App } = await import('https://esm.sh/@anthropic-ai/mcp-app-sdk@0.1');
    const app = new App({ name: 'Log UI', version: '1.0.0' });
    await app.connect(sessionId ? { sessionId } : undefined);
    return {
      callServerTool: (name, args) => app.callServerTool(name, args),
      context: app.context,
      type: 'sdk'
    };
  } catch (e) {
    console.log('MCP App SDK not available:', e.message);
    return null;
  }
}

let currentToolName = null;
let currentArgs = {};
let autoRefreshInterval = null;

async function initApp() {
  try {
    mcpClient = await initMcpClient();
    if (!mcpClient) {
      setStatus('Standalone mode');
      return;
    }

    if (mcpClient.context?.toolName) {
      currentToolName = mcpClient.context.toolName;
      currentArgs = mcpClient.context.arguments || {};
    }

    setStatus('Connected (' + mcpClient.type + ')');
  } catch (e) {
    console.log('Init error:', e.message);
    setStatus('Standalone mode');
  }
}

function setStatus(msg) {
  document.getElementById('status').textContent = msg;
}

function escapeHtml(str) {
  const div = document.createElement('div');
  div.textContent = str;
  return div.innerHTML;
}

function getLineClass(line) {
  const lower = line.toLowerCase();
  if (lower.includes('error')) return 'line error';
  if (lower.includes('warn')) return 'line warn';
  if (lower.includes('info')) return 'line info';
  return 'line';
}

async function refresh() {
  if (!mcpClient || !currentToolName) {
    setStatus('Refresh not available');
    return;
  }

  const btn = document.getElementById('refresh-btn');
  const icon = document.getElementById('refresh-icon');
  btn.disabled = true;
  icon.innerHTML = '<div class="spinner"></div>';
  setStatus('Refreshing...');

  try {
    const result = await mcpClient.callServerTool(currentToolName, currentArgs);
    if (result?.content?.[0]?.text) {
      updateLogs(result.content[0].text);
      setStatus('Updated at ' + new Date().toLocaleTimeString());
    } else if (result?.isError) {
      setStatus('Error: ' + (result.content?.[0]?.text || 'Unknown error'));
    }
  } catch (e) {
    setStatus('Error: ' + e.message);
  } finally {
    btn.disabled = false;
    icon.innerHTML = '&#x21bb;';
  }
}

function updateLogs(text) {
  const lines = text.split('\n');
  const container = document.getElementById('logs');
  container.innerHTML = lines.map((line, i) =>
    '<div class="' + getLineClass(line) + '"><span class="line-num">' + (i + 1) + '</span>' + escapeHtml(line) + '</div>'
  ).join('');
  filterLogs(); // Re-apply filter
}

function toggleAutoRefresh() {
  const checkbox = document.getElementById('auto-refresh');
  if (checkbox.checked) {
    autoRefreshInterval = setInterval(refresh, 5000);
    setStatus('Auto-refresh enabled');
  } else {
    if (autoRefreshInterval) {
      clearInterval(autoRefreshInterval);
      autoRefreshInterval = null;
    }
    setStatus('Auto-refresh disabled');
  }
}

window.refresh = refresh;
window.toggleAutoRefresh = toggleAutoRefresh;

window.filterLogs = function() {
  const filter = document.getElementById('filter').value.toLowerCase();
  document.querySelectorAll('.line').forEach(line => {
    const text = line.textContent.toLowerCase();
    line.classList.toggle('hidden', filter && !text.includes(filter));
  });
};

// Alias for internal use
const filterLogs = window.filterLogs;

initApp();
</script>
</body></html>`, sessionID))

	return sb.String(), nil
}

func generateDefaultUI(output string) (string, error) {
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: 'Monaco', 'Menlo', monospace; font-size: 13px; padding: 16px; background: #f5f5f5; }
pre { background: white; padding: 16px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); white-space: pre-wrap; word-wrap: break-word; line-height: 1.5; }
</style>
</head>
<body>
<pre>%s</pre>
</body></html>`, html.EscapeString(output)), nil
}

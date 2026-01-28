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

	return map[string]interface{}{
		"ui": map[string]interface{}{
			"resourceUri": UIResourceURI(tool.Name),
		},
	}
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
func GenerateUIHTML(tool *plugin.Tool, encodedData string) (string, error) {
	// Decode the data
	data, err := base64.URLEncoding.DecodeString(encodedData)
	if err != nil {
		return "", fmt.Errorf("failed to decode data: %w", err)
	}
	output := string(data)

	// If custom template is specified, use it
	if tool.UITemplate != "" {
		return generateCustomUI(tool.UITemplate, output)
	}

	switch tool.UIType {
	case plugin.UITypeTable:
		return generateTableUI(tool, output)
	case plugin.UITypeJSON:
		return generateJSONUI(output)
	case plugin.UITypeLog:
		return generateLogUI(output)
	default:
		return generateDefaultUI(output)
	}
}

// TemplateData is passed to custom templates
type TemplateData struct {
	Output     string                 // Raw output string
	Lines      []string               // Output split by lines
	JSON       interface{}            // Parsed JSON (if valid)
	JSONPretty string                 // Pretty-printed JSON (if valid)
	IsJSON     bool                   // Whether output is valid JSON
}

func generateCustomUI(templatePath string, output string) (string, error) {
	// Read template file
	tmplContent, err := os.ReadFile(templatePath)
	if err != nil {
		return "", fmt.Errorf("failed to read template file: %w", err)
	}

	// Parse template
	tmpl, err := template.New("ui").Funcs(template.FuncMap{
		"escape": html.EscapeString,
		"json": func(v interface{}) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
		"jsonPretty": func(v interface{}) string {
			b, _ := json.MarshalIndent(v, "", "  ")
			return string(b)
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
		"contains": strings.Contains,
		"hasPrefix": strings.HasPrefix,
		"trimSpace": strings.TrimSpace,
	}).Parse(string(tmplContent))
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	// Prepare template data
	data := TemplateData{
		Output: output,
		Lines:  strings.Split(output, "\n"),
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

func generateTableUI(tool *plugin.Tool, output string) (string, error) {
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

	return buildTableHTML(headers, rows), nil
}

func buildTableHTML(headers []string, rows [][]string) string {
	var sb strings.Builder

	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; padding: 16px; background: #f5f5f5; }
.container { background: white; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); overflow: hidden; }
table { border-collapse: collapse; width: 100%; }
th, td { border-bottom: 1px solid #e0e0e0; padding: 12px 16px; text-align: left; }
th { background: #f8f9fa; font-weight: 600; cursor: pointer; user-select: none; }
th:hover { background: #e9ecef; }
tr:hover td { background: #f8f9fa; }
.sort-icon { margin-left: 4px; opacity: 0.5; }
</style>
</head>
<body>
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

	sb.WriteString(`</tbody></table></div>
<script>
let sortDir = {};
function sortTable(col) {
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
}
</script>
</body></html>`)

	return sb.String()
}

func generateJSONUI(output string) (string, error) {
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
pre { white-space: pre-wrap; word-wrap: break-word; line-height: 1.5; }
.string { color: #ce9178; }
.number { color: #b5cea8; }
.boolean { color: #569cd6; }
.null { color: #569cd6; }
.key { color: #9cdcfe; }
</style>
</head>
<body>
<pre id="json">%s</pre>
<script>
function syntaxHighlight(json) {
  return json.replace(/("(\\u[a-zA-Z0-9]{4}|\\[^u]|[^\\"])*"(\s*:)?|\b(true|false|null)\b|-?\d+(?:\.\d*)?(?:[eE][+\-]?\d+)?)/g, function (match) {
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
</script>
</body></html>`, html.EscapeString(string(prettyJSON))), nil
}

func generateLogUI(output string) (string, error) {
	lines := strings.Split(output, "\n")

	var sb strings.Builder
	sb.WriteString(`<!DOCTYPE html>
<html>
<head>
<meta charset="UTF-8">
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: 'Monaco', 'Menlo', monospace; font-size: 12px; background: #1e1e1e; color: #d4d4d4; }
.toolbar { padding: 8px 16px; background: #333; border-bottom: 1px solid #444; position: sticky; top: 0; }
.toolbar input { padding: 4px 8px; border: 1px solid #555; background: #2d2d2d; color: #fff; border-radius: 4px; width: 300px; }
.log-container { padding: 16px; max-height: calc(100vh - 50px); overflow-y: auto; }
.line { padding: 2px 0; white-space: pre-wrap; word-wrap: break-word; }
.line:hover { background: #2d2d2d; }
.line-num { color: #858585; margin-right: 16px; user-select: none; display: inline-block; min-width: 40px; text-align: right; }
.hidden { display: none; }
.error { color: #f48771; }
.warn { color: #cca700; }
.info { color: #3794ff; }
</style>
</head>
<body>
<div class="toolbar">
<input type="text" id="filter" placeholder="Filter logs..." oninput="filterLogs()">
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

	sb.WriteString(`</div>
<script>
function filterLogs() {
  const filter = document.getElementById('filter').value.toLowerCase();
  document.querySelectorAll('.line').forEach(line => {
    const text = line.textContent.toLowerCase();
    line.classList.toggle('hidden', filter && !text.includes(filter));
  });
}
</script>
</body></html>`)

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

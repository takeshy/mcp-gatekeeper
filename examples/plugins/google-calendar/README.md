# Google Calendar Plugin for MCP Gatekeeper

A Go CLI binary (`gcal`) that wraps Google Calendar API v3, providing calendar management through MCP Gatekeeper. Includes an interactive `show-calendar` MCP App for viewing, adding, and deleting events.

## Setup

### 1. Create Google Cloud Credentials

1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Create a new project (or use an existing one)
3. Enable the **Google Calendar API**
4. Go to **Credentials** → **Create Credentials** → **OAuth 2.0 Client IDs**
5. Choose **Desktop app** as the application type
6. Download the credentials JSON file

### 2. Install Credentials

Place the downloaded file at `~/.config/mcp-gcal/credentials.json`:

```bash
mkdir -p ~/.config/mcp-gcal
cp ~/Downloads/client_secret_*.json ~/.config/mcp-gcal/credentials.json
```

Or set `XDG_CONFIG_HOME` to use a custom config directory.

### 3. Build

```bash
make build
```

### 4. Authenticate

```bash
./gcal auth
```

This opens your browser for Google OAuth consent. After authorization, a token is saved to `~/.config/mcp-gcal/token.json`.

## Usage

### CLI

```bash
# List calendars
./gcal list-calendars

# List upcoming events (next 7 days)
./gcal list-events

# List events for a specific date range
./gcal list-events --time-min=2026-02-01T00:00:00Z --time-max=2026-03-01T00:00:00Z

# Get event details
./gcal get-event --event-id=EVENT_ID

# Search events
./gcal search-events --query="meeting"

# Create event
./gcal create-event --summary="Team Meeting" --start=2026-02-15T10:00:00-05:00 --end=2026-02-15T11:00:00-05:00

# Create all-day event
./gcal create-event --summary="Vacation" --start=2026-02-20 --end=2026-02-21

# Update event
./gcal update-event --event-id=EVENT_ID --summary="Updated Title"

# Delete event
./gcal delete-event --event-id=EVENT_ID

# Respond to event invitation
./gcal respond-to-event --event-id=EVENT_ID --response=accepted
```

All output is JSON to stdout. Errors are `{"error": "message"}` with non-zero exit.

### MCP Gatekeeper Integration

```bash
mcp-gatekeeper \
  --root-dir=/path/to/google-calendar \
  --plugin-file=/path/to/google-calendar/plugin.json \
  --mode=stdio
```

The `gcal` binary must be in the `--root-dir` path or in `$PATH`.

### MCP App

The `show-calendar` tool provides an interactive calendar UI with:

- Month and week views
- Event detail panel
- Add new events
- Delete events
- Navigate between months/weeks

## Tools

| Tool | Description | Visibility |
|---|---|---|
| `list-calendars` | List all calendars | Model + App |
| `list-events` | List events with date range | Model + App |
| `get-event` | Get event details | Model + App |
| `search-events` | Search events by query | Model + App |
| `create-event` | Create a new event | Model + App |
| `update-event` | Update an event | Model + App |
| `delete-event` | Delete an event | Model + App |
| `respond-to-event` | Respond to invitation | Model + App |
| `show-calendar` | Interactive calendar UI | Model + App |
| `gcal-list-events-app` | List events (UI helper) | App only |
| `gcal-create-event-app` | Create event (UI helper) | App only |
| `gcal-delete-event-app` | Delete event (UI helper) | App only |
| `gcal-get-event-app` | Get event (UI helper) | App only |

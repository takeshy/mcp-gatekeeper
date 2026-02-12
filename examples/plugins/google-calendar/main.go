package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
)

// outputWriter is where JSON output is written (stdout). Declared as a var for testability.
var outputWriter io.Writer = os.Stdout

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: gcal <subcommand> [flags]\nsubcommands: auth, list-calendars, list-events, get-event, search-events, create-event, update-event, delete-event, respond-to-event")
	}

	subcmd := os.Args[1]
	args := os.Args[2:]

	if subcmd == "auth" {
		fs := flag.NewFlagSet("auth", flag.ExitOnError)
		credFile := fs.String("credentials-file", "", "Path to OAuth2 credentials JSON file")
		fs.Parse(args)
		if err := runAuthFlow(*credFile); err != nil {
			fatalf("%v", err)
		}
		return
	}

	// All other subcommands require a calendar service
	config, err := loadOAuthConfig("")
	if err != nil {
		fatalf("%v", err)
	}
	ts, err := getTokenSource(config)
	if err != nil {
		fatalf("%v", err)
	}
	ctx := context.Background()
	svc, err := newCalendarService(ctx, ts)
	if err != nil {
		fatalf("failed to create calendar service: %v", err)
	}

	switch subcmd {
	case "list-calendars":
		if err := listCalendars(svc); err != nil {
			fatalf("%v", err)
		}

	case "list-events":
		fs := flag.NewFlagSet("list-events", flag.ExitOnError)
		calID := fs.String("calendar-id", "primary", "Calendar ID")
		timeMin := fs.String("time-min", "", "Start time (RFC3339)")
		timeMax := fs.String("time-max", "", "End time (RFC3339)")
		maxResults := fs.Int64("max-results", 50, "Maximum number of events")
		singleEvents := fs.Bool("single-events", true, "Expand recurring events")
		orderBy := fs.String("order-by", "startTime", "Order by (startTime or updated)")
		fs.Parse(args)
		if err := listEvents(svc, *calID, *timeMin, *timeMax, *maxResults, *singleEvents, *orderBy); err != nil {
			fatalf("%v", err)
		}

	case "get-event":
		fs := flag.NewFlagSet("get-event", flag.ExitOnError)
		calID := fs.String("calendar-id", "primary", "Calendar ID")
		eventID := fs.String("event-id", "", "Event ID (required)")
		fs.Parse(args)
		if *eventID == "" {
			fatalf("--event-id is required")
		}
		if err := getEvent(svc, *calID, *eventID); err != nil {
			fatalf("%v", err)
		}

	case "search-events":
		fs := flag.NewFlagSet("search-events", flag.ExitOnError)
		calID := fs.String("calendar-id", "primary", "Calendar ID")
		query := fs.String("query", "", "Search query (required)")
		timeMin := fs.String("time-min", "", "Start time (RFC3339)")
		timeMax := fs.String("time-max", "", "End time (RFC3339)")
		maxResults := fs.Int64("max-results", 50, "Maximum number of events")
		fs.Parse(args)
		if *query == "" {
			fatalf("--query is required")
		}
		if err := searchEvents(svc, *calID, *query, *timeMin, *timeMax, *maxResults); err != nil {
			fatalf("%v", err)
		}

	case "create-event":
		fs := flag.NewFlagSet("create-event", flag.ExitOnError)
		calID := fs.String("calendar-id", "primary", "Calendar ID")
		summary := fs.String("summary", "", "Event summary (required)")
		description := fs.String("description", "", "Event description")
		location := fs.String("location", "", "Event location")
		start := fs.String("start", "", "Start time - RFC3339 or YYYY-MM-DD (required)")
		end := fs.String("end", "", "End time - RFC3339 or YYYY-MM-DD (required)")
		timezone := fs.String("timezone", "", "Timezone (e.g., America/New_York)")
		attendees := fs.String("attendees", "", "Comma-separated attendee emails")
		fs.Parse(args)
		if *summary == "" || *start == "" || *end == "" {
			fatalf("--summary, --start, and --end are required")
		}
		if err := createEvent(svc, *calID, *summary, *description, *location, *start, *end, *timezone, *attendees); err != nil {
			fatalf("%v", err)
		}

	case "update-event":
		fs := flag.NewFlagSet("update-event", flag.ExitOnError)
		calID := fs.String("calendar-id", "primary", "Calendar ID")
		eventID := fs.String("event-id", "", "Event ID (required)")
		fs.String("summary", "", "New summary")
		fs.String("description", "", "New description")
		fs.String("location", "", "New location")
		fs.String("start", "", "New start time")
		fs.String("end", "", "New end time")
		fs.String("attendees", "", "New attendee list (comma-separated)")
		fs.Parse(args)
		if *eventID == "" {
			fatalf("--event-id is required")
		}
		updates := make(map[string]string)
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "summary", "description", "location", "start", "end", "attendees":
				updates[f.Name] = f.Value.String()
			}
		})
		if err := updateEvent(svc, *calID, *eventID, updates); err != nil {
			fatalf("%v", err)
		}

	case "delete-event":
		fs := flag.NewFlagSet("delete-event", flag.ExitOnError)
		calID := fs.String("calendar-id", "primary", "Calendar ID")
		eventID := fs.String("event-id", "", "Event ID (required)")
		fs.Parse(args)
		if *eventID == "" {
			fatalf("--event-id is required")
		}
		if err := deleteEvent(svc, *calID, *eventID); err != nil {
			fatalf("%v", err)
		}

	case "respond-to-event":
		fs := flag.NewFlagSet("respond-to-event", flag.ExitOnError)
		calID := fs.String("calendar-id", "primary", "Calendar ID")
		eventID := fs.String("event-id", "", "Event ID (required)")
		response := fs.String("response", "", "Response: accepted, declined, or tentative (required)")
		fs.Parse(args)
		if *eventID == "" || *response == "" {
			fatalf("--event-id and --response are required")
		}
		if err := respondToEvent(svc, *calID, *eventID, *response); err != nil {
			fatalf("%v", err)
		}

	default:
		fatalf("unknown subcommand: %s", subcmd)
	}
}

func fatalf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	json.NewEncoder(os.Stdout).Encode(map[string]string{"error": msg})
	os.Exit(1)
}

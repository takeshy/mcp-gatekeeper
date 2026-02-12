package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

// newCalendarService creates a Google Calendar API service using the given token source.
func newCalendarService(ctx context.Context, ts oauth2.TokenSource) (*calendar.Service, error) {
	return calendar.NewService(ctx, option.WithTokenSource(ts))
}

// eventJSON is the simplified event structure for JSON output.
type eventJSON struct {
	ID          string          `json:"id"`
	Summary     string          `json:"summary"`
	Description string          `json:"description,omitempty"`
	Location    string          `json:"location,omitempty"`
	Start       *dateTimeJSON   `json:"start,omitempty"`
	End         *dateTimeJSON   `json:"end,omitempty"`
	Status      string          `json:"status,omitempty"`
	HTMLLink    string          `json:"htmlLink,omitempty"`
	Attendees   []attendeeJSON  `json:"attendees,omitempty"`
	Organizer   *organizerJSON  `json:"organizer,omitempty"`
	Created     string          `json:"created,omitempty"`
	Updated     string          `json:"updated,omitempty"`
}

type dateTimeJSON struct {
	DateTime string `json:"dateTime,omitempty"`
	Date     string `json:"date,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type attendeeJSON struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Self           bool   `json:"self,omitempty"`
}

type organizerJSON struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName,omitempty"`
	Self        bool   `json:"self,omitempty"`
}

type calendarJSON struct {
	ID          string `json:"id"`
	Summary     string `json:"summary"`
	Description string `json:"description,omitempty"`
	Primary     bool   `json:"primary,omitempty"`
	TimeZone    string `json:"timeZone,omitempty"`
}

func convertEvent(e *calendar.Event) eventJSON {
	ev := eventJSON{
		ID:          e.Id,
		Summary:     e.Summary,
		Description: e.Description,
		Location:    e.Location,
		Status:      e.Status,
		HTMLLink:    e.HtmlLink,
		Created:     e.Created,
		Updated:     e.Updated,
	}
	if e.Start != nil {
		ev.Start = &dateTimeJSON{
			DateTime: e.Start.DateTime,
			Date:     e.Start.Date,
			TimeZone: e.Start.TimeZone,
		}
	}
	if e.End != nil {
		ev.End = &dateTimeJSON{
			DateTime: e.End.DateTime,
			Date:     e.End.Date,
			TimeZone: e.End.TimeZone,
		}
	}
	for _, a := range e.Attendees {
		ev.Attendees = append(ev.Attendees, attendeeJSON{
			Email:          a.Email,
			DisplayName:    a.DisplayName,
			ResponseStatus: a.ResponseStatus,
			Self:           a.Self,
		})
	}
	if e.Organizer != nil {
		ev.Organizer = &organizerJSON{
			Email:       e.Organizer.Email,
			DisplayName: e.Organizer.DisplayName,
			Self:        e.Organizer.Self,
		}
	}
	return ev
}

func listCalendars(svc *calendar.Service) error {
	list, err := svc.CalendarList.List().Do()
	if err != nil {
		return fmt.Errorf("failed to list calendars: %w", err)
	}
	var result []calendarJSON
	for _, c := range list.Items {
		result = append(result, calendarJSON{
			ID:          c.Id,
			Summary:     c.Summary,
			Description: c.Description,
			Primary:     c.Primary,
			TimeZone:    c.TimeZone,
		})
	}
	return json.NewEncoder(outputWriter).Encode(result)
}

func listEvents(svc *calendar.Service, calendarID, timeMin, timeMax string, maxResults int64, singleEvents bool, orderBy string) error {
	if calendarID == "" {
		calendarID = "primary"
	}
	now := time.Now()
	if timeMin == "" {
		timeMin = now.Format(time.RFC3339)
	}
	if timeMax == "" {
		timeMax = now.AddDate(0, 0, 7).Format(time.RFC3339)
	}
	if maxResults <= 0 {
		maxResults = 50
	}

	call := svc.Events.List(calendarID).
		TimeMin(timeMin).
		TimeMax(timeMax).
		MaxResults(maxResults).
		SingleEvents(singleEvents)

	if orderBy != "" {
		call = call.OrderBy(orderBy)
	}

	events, err := call.Do()
	if err != nil {
		return fmt.Errorf("failed to list events: %w", err)
	}

	var result []eventJSON
	for _, e := range events.Items {
		result = append(result, convertEvent(e))
	}
	if result == nil {
		result = []eventJSON{}
	}
	return json.NewEncoder(outputWriter).Encode(result)
}

func getEvent(svc *calendar.Service, calendarID, eventID string) error {
	if calendarID == "" {
		calendarID = "primary"
	}
	e, err := svc.Events.Get(calendarID, eventID).Do()
	if err != nil {
		return fmt.Errorf("failed to get event: %w", err)
	}
	return json.NewEncoder(outputWriter).Encode(convertEvent(e))
}

func searchEvents(svc *calendar.Service, calendarID, query, timeMin, timeMax string, maxResults int64) error {
	if calendarID == "" {
		calendarID = "primary"
	}
	now := time.Now()
	if timeMin == "" {
		timeMin = now.Format(time.RFC3339)
	}
	if timeMax == "" {
		timeMax = now.AddDate(0, 0, 7).Format(time.RFC3339)
	}
	if maxResults <= 0 {
		maxResults = 50
	}

	events, err := svc.Events.List(calendarID).
		Q(query).
		TimeMin(timeMin).
		TimeMax(timeMax).
		MaxResults(maxResults).
		SingleEvents(true).
		OrderBy("startTime").
		Do()
	if err != nil {
		return fmt.Errorf("failed to search events: %w", err)
	}

	var result []eventJSON
	for _, e := range events.Items {
		result = append(result, convertEvent(e))
	}
	if result == nil {
		result = []eventJSON{}
	}
	return json.NewEncoder(outputWriter).Encode(result)
}

func createEvent(svc *calendar.Service, calendarID, summary, description, location, start, end, timezone, attendees string) error {
	if calendarID == "" {
		calendarID = "primary"
	}

	event := &calendar.Event{
		Summary:     summary,
		Description: description,
		Location:    location,
	}

	// Determine if all-day or timed event
	if len(start) == 10 { // YYYY-MM-DD format
		event.Start = &calendar.EventDateTime{Date: start}
	} else {
		event.Start = &calendar.EventDateTime{DateTime: start}
	}
	if timezone != "" && event.Start != nil {
		event.Start.TimeZone = timezone
	}

	if len(end) == 10 {
		event.End = &calendar.EventDateTime{Date: end}
	} else {
		event.End = &calendar.EventDateTime{DateTime: end}
	}
	if timezone != "" && event.End != nil {
		event.End.TimeZone = timezone
	}

	if attendees != "" {
		for _, email := range strings.Split(attendees, ",") {
			email = strings.TrimSpace(email)
			if email != "" {
				event.Attendees = append(event.Attendees, &calendar.EventAttendee{Email: email})
			}
		}
	}

	created, err := svc.Events.Insert(calendarID, event).Do()
	if err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}
	return json.NewEncoder(outputWriter).Encode(convertEvent(created))
}

func updateEvent(svc *calendar.Service, calendarID, eventID string, updates map[string]string) error {
	if calendarID == "" {
		calendarID = "primary"
	}

	// Fetch current event
	existing, err := svc.Events.Get(calendarID, eventID).Do()
	if err != nil {
		return fmt.Errorf("failed to get event for update: %w", err)
	}

	if v, ok := updates["summary"]; ok {
		existing.Summary = v
	}
	if v, ok := updates["description"]; ok {
		existing.Description = v
	}
	if v, ok := updates["location"]; ok {
		existing.Location = v
	}
	if v, ok := updates["start"]; ok {
		if len(v) == 10 {
			existing.Start = &calendar.EventDateTime{Date: v}
		} else {
			existing.Start = &calendar.EventDateTime{DateTime: v}
		}
	}
	if v, ok := updates["end"]; ok {
		if len(v) == 10 {
			existing.End = &calendar.EventDateTime{Date: v}
		} else {
			existing.End = &calendar.EventDateTime{DateTime: v}
		}
	}
	if v, ok := updates["attendees"]; ok {
		existing.Attendees = nil
		for _, email := range strings.Split(v, ",") {
			email = strings.TrimSpace(email)
			if email != "" {
				existing.Attendees = append(existing.Attendees, &calendar.EventAttendee{Email: email})
			}
		}
	}

	updated, err := svc.Events.Update(calendarID, eventID, existing).Do()
	if err != nil {
		return fmt.Errorf("failed to update event: %w", err)
	}
	return json.NewEncoder(outputWriter).Encode(convertEvent(updated))
}

func deleteEvent(svc *calendar.Service, calendarID, eventID string) error {
	if calendarID == "" {
		calendarID = "primary"
	}
	err := svc.Events.Delete(calendarID, eventID).Do()
	if err != nil {
		return fmt.Errorf("failed to delete event: %w", err)
	}
	return json.NewEncoder(outputWriter).Encode(map[string]string{
		"status":  "deleted",
		"eventId": eventID,
	})
}

func respondToEvent(svc *calendar.Service, calendarID, eventID, response string) error {
	if calendarID == "" {
		calendarID = "primary"
	}

	// Valid responses: accepted, declined, tentative
	switch response {
	case "accepted", "declined", "tentative":
	default:
		return fmt.Errorf("invalid response: %s (must be accepted, declined, or tentative)", response)
	}

	// Get current event to find self attendee
	event, err := svc.Events.Get(calendarID, eventID).Do()
	if err != nil {
		return fmt.Errorf("failed to get event: %w", err)
	}

	// Find self in attendees and update response
	found := false
	for _, a := range event.Attendees {
		if a.Self {
			a.ResponseStatus = response
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("you are not an attendee of this event")
	}

	updated, err := svc.Events.Update(calendarID, eventID, event).SendUpdates("all").Do()
	if err != nil {
		return fmt.Errorf("failed to update response: %w", err)
	}
	return json.NewEncoder(outputWriter).Encode(convertEvent(updated))
}

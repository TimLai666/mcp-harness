package harness

import (
	"context"
	"sync"
	"time"
)

// Event is one live activity record broadcast to subscribers (the Web UI over
// SSE). It carries terminal output chunks as they are produced plus tool
// lifecycle, history, and approval signals so the console can update in real
// time instead of polling.
type Event struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	CallID    string `json:"call_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	Tool      string `json:"tool,omitempty"`
	Command   string `json:"command,omitempty"`
	Stream    string `json:"stream,omitempty"`
	Data      string `json:"data,omitempty"`
	Status    string `json:"status,omitempty"`
	Error     string `json:"error,omitempty"`
	Payload   any    `json:"payload,omitempty"`
}

// Event type constants.
const (
	EventToolStart      = "tool_start"
	EventToolEnd        = "tool_end"
	EventTerminalOutput = "terminal_output"
	EventHistory        = "history"
	EventApproval       = "approval"
)

// Broker fans out events to subscribers. Sends are non-blocking: a slow or dead
// subscriber drops events rather than stalling the command that is running.
type Broker struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

var defaultBroker = &Broker{subs: map[chan Event]struct{}{}}

// DefaultBroker is the process-wide event broker.
func DefaultBroker() *Broker { return defaultBroker }

// Subscribe registers a new subscriber and returns its channel and an
// unsubscribe function. The caller must call unsubscribe when done.
func (b *Broker) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 256)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			close(ch)
			b.mu.Unlock()
		})
	}
	return ch, cancel
}

// Publish broadcasts an event to all current subscribers without blocking.
func (b *Broker) Publish(ev Event) {
	if ev.Timestamp == "" {
		ev.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- ev:
		default:
			// Subscriber is full; drop this event for it.
		}
	}
}

type ctxKey int

const callIDKey ctxKey = iota

// WithCallID attaches a call id to the context so streaming handlers can tag
// their output events.
func WithCallID(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, callIDKey, callID)
}

// CallIDFromContext returns the call id attached by WithCallID, if any.
func CallIDFromContext(ctx context.Context) string {
	if value, ok := ctx.Value(callIDKey).(string); ok {
		return value
	}
	return ""
}

func projectIDOf(workspace Workspace) string {
	if workspace.Project != nil {
		return workspace.Project.ID
	}
	return ""
}

func publishToolStart(callID string, workspace Workspace, sessionID, tool, command string) {
	defaultBroker.Publish(Event{
		Type:      EventToolStart,
		CallID:    callID,
		SessionID: sessionID,
		ProjectID: projectIDOf(workspace),
		Tool:      tool,
		Command:   command,
	})
}

func publishToolEnd(callID string, workspace Workspace, sessionID, tool, status, errText string) {
	defaultBroker.Publish(Event{
		Type:      EventToolEnd,
		CallID:    callID,
		SessionID: sessionID,
		ProjectID: projectIDOf(workspace),
		Tool:      tool,
		Status:    status,
		Error:     errText,
	})
}

func publishHistory(event HistoryEvent) {
	defaultBroker.Publish(Event{
		Type:      EventHistory,
		SessionID: event.SessionID,
		ProjectID: event.ProjectID,
		Tool:      event.Tool,
		Status:    event.Status,
		Payload:   event,
	})
}

func publishApproval(record ApprovalRecord) {
	defaultBroker.Publish(Event{
		Type:      EventApproval,
		SessionID: record.SessionID,
		ProjectID: record.Project,
		Tool:      record.Tool,
		Status:    string(record.Status),
		Payload:   record,
	})
}

// PublishTerminalOutput streams one chunk of terminal output to subscribers.
func PublishTerminalOutput(callID, sessionID string, workspace Workspace, command, stream, data string) {
	defaultBroker.Publish(Event{
		Type:      EventTerminalOutput,
		CallID:    callID,
		SessionID: sessionID,
		ProjectID: projectIDOf(workspace),
		Tool:      "terminal.run",
		Command:   command,
		Stream:    stream,
		Data:      data,
	})
}

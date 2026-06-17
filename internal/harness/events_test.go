package harness

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBrokerDeliversAndUnsubscribes(t *testing.T) {
	broker := &Broker{subs: map[chan Event]struct{}{}}
	ch, cancel := broker.Subscribe()
	broker.Publish(Event{Type: "test", Data: "hello"})
	select {
	case ev := <-ch:
		if ev.Type != "test" || ev.Data != "hello" {
			t.Fatalf("unexpected event: %#v", ev)
		}
		if ev.Timestamp == "" {
			t.Fatal("expected timestamp to be stamped")
		}
	case <-time.After(time.Second):
		t.Fatal("expected to receive event")
	}
	cancel()
	cancel() // must be idempotent
	broker.Publish(Event{Type: "after-unsub"})
}

func TestTerminalRunStreamsLiveOutput(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MCP_HARNESS_HOME", home)

	events, cancel := DefaultBroker().Subscribe()
	defer cancel()

	done := make(chan map[string]bool, 1)
	go func() {
		seen := map[string]bool{}
		timeout := time.After(20 * time.Second)
		for {
			select {
			case ev := <-events:
				switch ev.Type {
				case EventToolStart:
					seen["start"] = true
				case EventTerminalOutput:
					if strings.Contains(ev.Data, "harness-live") {
						seen["output"] = true
					}
				case EventToolEnd:
					seen["end"] = true
					done <- seen
					return
				}
			case <-timeout:
				done <- seen
				return
			}
		}
	}()

	res, err := NewRuntime().ExecuteTool(context.Background(), ToolCallRequest{
		Tool: "terminal.run",
		Args: map[string]any{"command": "echo harness-live"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "ok" {
		t.Fatalf("expected ok terminal result, got %#v", res)
	}

	seen := <-done
	if !seen["start"] || !seen["output"] || !seen["end"] {
		t.Fatalf("expected start, streamed output, and end events, got %#v", seen)
	}
}

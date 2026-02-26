package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestSSEParseMessages(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)

		sr := StreamResponse{
			Messages: []APIMessage{
				{MessageID: "msg-1", ConversationID: "conv-1", SequenceID: 1, Type: "user"},
				{MessageID: "msg-2", ConversationID: "conv-1", SequenceID: 2, Type: "agent"},
			},
			Conversation: Conversation{ConversationID: "conv-1"},
		}
		data, _ := json.Marshal(sr)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}))
	defer ts.Close()

	events := make(chan StreamEvent, 10)
	stream := NewSSEStream(ts.URL+"/api/conversation/conv-1/stream", events)
	go stream.Connect()
	defer stream.Close()

	select {
	case ev := <-events:
		if ev.Err != nil {
			t.Fatal(ev.Err)
		}
		if len(ev.Response.Messages) != 2 {
			t.Fatalf("got %d messages", len(ev.Response.Messages))
		}
		if ev.Response.Messages[0].MessageID != "msg-1" {
			t.Errorf("got %q", ev.Response.Messages[0].MessageID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SSE event")
	}
}

func TestSSEHeartbeat(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		sr := StreamResponse{
			Heartbeat:    true,
			Conversation: Conversation{ConversationID: "conv-1"},
		}
		data, _ := json.Marshal(sr)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}))
	defer ts.Close()

	events := make(chan StreamEvent, 10)
	stream := NewSSEStream(ts.URL+"/api/conversation/conv-1/stream", events)
	go stream.Connect()
	defer stream.Close()

	select {
	case ev := <-events:
		if ev.Err != nil {
			t.Fatal(ev.Err)
		}
		if !ev.Response.Heartbeat {
			t.Error("expected heartbeat=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSSESequenceIDTracking(t *testing.T) {
	var mu sync.Mutex
	var requestCount int

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		count := requestCount
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		if count == 1 {
			sr := StreamResponse{
				Messages: []APIMessage{
					{MessageID: "msg-1", SequenceID: 5, Type: "agent"},
					{MessageID: "msg-2", SequenceID: 10, Type: "agent"},
				},
				Conversation: Conversation{ConversationID: "conv-1"},
			}
			data, _ := json.Marshal(sr)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		// Close connection to trigger reconnection attempt
	}))
	defer ts.Close()

	events := make(chan StreamEvent, 10)
	stream := NewSSEStream(ts.URL+"/api/conversation/conv-1/stream", events)
	go stream.Connect()
	defer stream.Close()

	// Wait for first event
	select {
	case ev := <-events:
		if ev.Err != nil {
			t.Fatal(ev.Err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	// Verify sequence tracking
	if stream.LastSeqID() != 10 {
		t.Errorf("got lastSeqID %d, want 10", stream.LastSeqID())
	}
}

func TestSSEMultipleEvents(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		for i := 1; i <= 3; i++ {
			sr := StreamResponse{
				Messages: []APIMessage{
					{MessageID: fmt.Sprintf("msg-%d", i), SequenceID: int64(i), Type: "agent"},
				},
				Conversation: Conversation{ConversationID: "conv-1"},
			}
			data, _ := json.Marshal(sr)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}))
	defer ts.Close()

	events := make(chan StreamEvent, 10)
	stream := NewSSEStream(ts.URL+"/api/conversation/conv-1/stream", events)
	go stream.Connect()
	defer stream.Close()

	for i := 1; i <= 3; i++ {
		select {
		case ev := <-events:
			if ev.Err != nil {
				t.Fatal(ev.Err)
			}
			expected := fmt.Sprintf("msg-%d", i)
			if ev.Response.Messages[0].MessageID != expected {
				t.Errorf("event %d: got %q, want %q", i, ev.Response.Messages[0].MessageID, expected)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for event %d", i)
		}
	}
}

func TestSSEConnectionError(t *testing.T) {
	// Connect to a server that immediately closes
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	events := make(chan StreamEvent, 10)
	stream := NewSSEStream(ts.URL+"/api/conversation/conv-1/stream", events)
	go stream.Connect()
	defer stream.Close()

	select {
	case ev := <-events:
		if ev.Err == nil {
			t.Error("expected error event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
}

func TestSSEClose(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		// Send one event, then keep connection open
		sr := StreamResponse{
			Conversation: Conversation{ConversationID: "conv-1"},
			Heartbeat:    true,
		}
		data, _ := json.Marshal(sr)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()

		// Block until client disconnects
		<-r.Context().Done()
	}))
	defer ts.Close()

	events := make(chan StreamEvent, 10)
	stream := NewSSEStream(ts.URL+"/api/conversation/conv-1/stream", events)
	go stream.Connect()

	// Wait for the heartbeat
	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	// Close should not hang
	stream.Close()
}

func TestSSECloseWithoutConnect(t *testing.T) {
	events := make(chan StreamEvent, 10)
	stream := NewSSEStream("http://localhost/test", events)
	// Close without calling Connect must not block.
	done := make(chan struct{})
	go func() {
		stream.Close()
		close(done)
	}()
	select {
	case <-done:
		// OK
	case <-time.After(1 * time.Second):
		t.Fatal("Close blocked on unconnected stream")
	}
}

func TestSSEResumeWithSequenceID(t *testing.T) {
	var lastSeqParam string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastSeqParam = r.URL.Query().Get("last_sequence_id")
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)

		sr := StreamResponse{
			Messages:     []APIMessage{{MessageID: "msg-1", SequenceID: 1}},
			Conversation: Conversation{ConversationID: "conv-1"},
		}
		data, _ := json.Marshal(sr)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}))
	defer ts.Close()

	events := make(chan StreamEvent, 10)
	stream := NewSSEStream(ts.URL+"/api/conversation/conv-1/stream", events)
	stream.SetLastSeqID(42)
	go stream.Connect()
	defer stream.Close()

	select {
	case <-events:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	if lastSeqParam != "42" {
		t.Errorf("got last_sequence_id param %q, want %q", lastSeqParam, "42")
	}
}

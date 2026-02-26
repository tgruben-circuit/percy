package tui

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// StreamEvent is delivered from the SSE stream to the Bubble Tea model.
type StreamEvent struct {
	Response StreamResponse
	Err      error
}

// SSEStream reads SSE events from a Percy conversation stream endpoint.
type SSEStream struct {
	url       string
	events    chan<- StreamEvent
	lastSeqID atomic.Int64
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// NewSSEStream creates a new SSE stream that delivers events to the given channel.
func NewSSEStream(url string, events chan<- StreamEvent) *SSEStream {
	return &SSEStream{
		url:    url,
		events: events,
		done:   make(chan struct{}),
	}
}

// LastSeqID returns the highest sequence ID seen so far.
func (s *SSEStream) LastSeqID() int64 {
	return s.lastSeqID.Load()
}

// SetLastSeqID sets the last sequence ID for resumption.
func (s *SSEStream) SetLastSeqID(id int64) {
	s.lastSeqID.Store(id)
}

// Connect starts reading from the SSE stream. It blocks until the stream
// is closed or an unrecoverable error occurs. Call Close() to stop.
func (s *SSEStream) Connect() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	defer close(s.done)

	s.readStream(ctx)
}

// Close stops the SSE stream.
func (s *SSEStream) Close() {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
			<-s.done
		}
	})
}

func (s *SSEStream) readStream(ctx context.Context) {
	url := s.url
	if seq := s.lastSeqID.Load(); seq > 0 {
		sep := "?"
		if strings.Contains(url, "?") {
			sep = "&"
		}
		url += sep + "last_sequence_id=" + strconv.FormatInt(seq, 10)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		s.send(ctx, StreamEvent{Err: fmt.Errorf("create request: %w", err)})
		return
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.send(ctx, StreamEvent{Err: fmt.Errorf("connect: %w", err)})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		s.send(ctx, StreamEvent{Err: fmt.Errorf("server returned %d", resp.StatusCode)})
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	// Allow up to 1MB per line for large messages
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		var sr StreamResponse
		if err := json.Unmarshal([]byte(data), &sr); err != nil {
			s.send(ctx, StreamEvent{Err: fmt.Errorf("parse SSE data: %w", err)})
			continue
		}

		// Track highest sequence ID
		for _, msg := range sr.Messages {
			if msg.SequenceID > s.lastSeqID.Load() {
				s.lastSeqID.Store(msg.SequenceID)
			}
		}

		s.send(ctx, StreamEvent{Response: sr})
	}
}

func (s *SSEStream) send(ctx context.Context, ev StreamEvent) {
	select {
	case s.events <- ev:
	case <-ctx.Done():
	}
}

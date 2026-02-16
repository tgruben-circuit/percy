package cluster

import (
	"context"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestEmbeddedNATS(t *testing.T) {
	dir := t.TempDir()
	srv, err := StartEmbeddedNATS(dir, 0)
	if err != nil {
		t.Fatalf("StartEmbeddedNATS: %v", err)
	}
	defer srv.Shutdown()

	url := srv.ClientURL()
	if url == "" {
		t.Fatal("ClientURL returned empty string")
	}

	ctx := context.Background()
	nc, err := Connect(ctx, url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer nc.Close()

	if !nc.IsConnected() {
		t.Fatal("expected connection to be connected")
	}
}

func TestPubSub(t *testing.T) {
	dir := t.TempDir()
	srv, err := StartEmbeddedNATS(dir, 0)
	if err != nil {
		t.Fatalf("StartEmbeddedNATS: %v", err)
	}
	defer srv.Shutdown()

	ctx := context.Background()
	nc, err := Connect(ctx, srv.ClientURL())
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer nc.Close()

	received := make(chan string, 1)
	sub, err := nc.Subscribe("test.subject", func(msg *nats.Msg) {
		received <- string(msg.Data)
	})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	defer func() { _ = sub.Unsubscribe() }()

	if err := nc.Publish("test.subject", []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := nc.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	select {
	case msg := <-received:
		if msg != "hello" {
			t.Fatalf("expected %q, got %q", "hello", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for message")
	}
}

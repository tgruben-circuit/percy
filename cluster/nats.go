// Package cluster provides NATS-based clustering for Percy instances.
package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
)

// EmbeddedNATS wraps an in-process NATS server with JetStream enabled.
type EmbeddedNATS struct {
	server *server.Server
}

// StartEmbeddedNATS starts an embedded NATS server with JetStream.
// Pass port 0 to pick a random available port.
func StartEmbeddedNATS(storeDir string, port int) (*EmbeddedNATS, error) {
	opts := &server.Options{
		Port:      port,
		JetStream: true,
		StoreDir:  storeDir,
		NoLog:     true,
		NoSigs:    true,
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("new nats server: %w", err)
	}

	ns.Start()

	if !ns.ReadyForConnections(5 * time.Second) {
		ns.Shutdown()
		return nil, fmt.Errorf("nats server not ready for connections")
	}

	return &EmbeddedNATS{server: ns}, nil
}

// ClientURL returns the URL clients should use to connect to this server.
func (e *EmbeddedNATS) ClientURL() string {
	return e.server.ClientURL()
}

// Shutdown gracefully stops the embedded NATS server.
func (e *EmbeddedNATS) Shutdown() {
	e.server.Shutdown()
	e.server.WaitForShutdown()
}

// Connect establishes a connection to a NATS server with auto-reconnect
// configured for infinite retries with a 1-second wait between attempts.
func Connect(ctx context.Context, url string) (*nats.Conn, error) {
	nc, err := nats.Connect(url,
		nats.MaxReconnects(-1),
		nats.ReconnectWait(1*time.Second),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	return nc, nil
}

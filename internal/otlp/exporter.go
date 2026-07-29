// Package otlp is a minimal OTLP/gRPC trace exporter used to push replayed
// spans into a local collector.
package otlp

import (
	"context"
	"time"

	coltracepb "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Client sends OTLP export requests over an insecure gRPC channel.
type Client struct {
	conn *grpc.ClientConn
	svc  coltracepb.TraceServiceClient
}

// Dial prepares a lazy gRPC connection to an OTLP collector endpoint such as
// "localhost:4317". The connection is established on the first Export, so a
// down collector surfaces as an export error rather than a dial failure.
func Dial(endpoint string) (*Client, error) {
	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	return &Client{conn: conn, svc: coltracepb.NewTraceServiceClient(conn)}, nil
}

// Export sends one batch of spans, bounded by a 10s timeout.
func (c *Client) Export(ctx context.Context, req *coltracepb.ExportTraceServiceRequest) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err := c.svc.Export(ctx, req)
	return err
}

// Close releases the underlying connection.
func (c *Client) Close() error { return c.conn.Close() }

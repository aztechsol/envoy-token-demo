package xds

import (
	"context"
	"log/slog"
	"sync/atomic"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
)

type Callbacks struct {
	logger    *slog.Logger
	connected atomic.Int64
}

func NewCallbacks(logger *slog.Logger) *Callbacks { return &Callbacks{logger: logger} }

func (c *Callbacks) OnStreamOpen(_ context.Context, id int64, typ string) error {
	clients := c.connected.Add(1)
	c.logger.Info("xDS stream connected", "stream_id", id, "type_url", typ, "connected_clients", clients)
	return nil
}
func (c *Callbacks) OnStreamClosed(id int64, node *corev3.Node) {
	clients := c.connected.Add(-1)
	c.logger.Info("xDS stream disconnected", "stream_id", id, "node", nodeID(node), "connected_clients", clients)
}
func (c *Callbacks) OnStreamRequest(id int64, req *discoveryv3.DiscoveryRequest) error {
	logRequest(c.logger, id, req)
	return nil
}
func (c *Callbacks) OnStreamResponse(context.Context, int64, *discoveryv3.DiscoveryRequest, *discoveryv3.DiscoveryResponse) {
}
func (c *Callbacks) OnFetchRequest(context.Context, *discoveryv3.DiscoveryRequest) error           { return nil }
func (c *Callbacks) OnFetchResponse(*discoveryv3.DiscoveryRequest, *discoveryv3.DiscoveryResponse) {}
func (c *Callbacks) OnDeltaStreamOpen(_ context.Context, id int64, typ string) error {
	clients := c.connected.Add(1)
	c.logger.Info("xDS delta stream connected", "stream_id", id, "type_url", typ, "connected_clients", clients)
	return nil
}
func (c *Callbacks) OnDeltaStreamClosed(id int64, node *corev3.Node) {
	clients := c.connected.Add(-1)
	c.logger.Info("xDS delta stream disconnected", "stream_id", id, "node", nodeID(node), "connected_clients", clients)
}
func (c *Callbacks) OnStreamDeltaRequest(int64, *discoveryv3.DeltaDiscoveryRequest) error { return nil }
func (c *Callbacks) OnStreamDeltaResponse(int64, *discoveryv3.DeltaDiscoveryRequest, *discoveryv3.DeltaDiscoveryResponse) {
}

func logRequest(logger *slog.Logger, streamID int64, req *discoveryv3.DiscoveryRequest) {
	if req.ResponseNonce == "" {
		return
	}
	attrs := []any{
		"stream_id", streamID,
		"node", nodeID(req.Node),
		"resource", ResourceName,
		"type_url", req.TypeUrl,
		"version", req.VersionInfo,
	}
	if req.ErrorDetail != nil {
		attrs = append(attrs, "event", "NACK", "error", req.ErrorDetail.Message)
		logger.Error("xDS response", attrs...)
		return
	}
	attrs = append(attrs, "event", "ACK")
	logger.Info("xDS response", attrs...)
}

func nodeID(node *corev3.Node) string {
	if node == nil {
		return ""
	}
	return node.Id
}

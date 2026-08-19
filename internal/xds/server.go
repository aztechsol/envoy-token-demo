package xds

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	discoverygrpc "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	serverv3 "github.com/envoyproxy/go-control-plane/pkg/server/v3"
	"google.golang.org/grpc"
)

type cacheLogger struct{ logger *slog.Logger }

func (l cacheLogger) Debugf(format string, args ...interface{}) {
	l.logger.Debug(fmt.Sprintf(format, args...))
}
func (l cacheLogger) Infof(format string, args ...interface{}) {
	l.logger.Info(fmt.Sprintf(format, args...))
}
func (l cacheLogger) Warnf(format string, args ...interface{}) {
	l.logger.Warn(fmt.Sprintf(format, args...))
}
func (l cacheLogger) Errorf(format string, args ...interface{}) {
	l.logger.Error(fmt.Sprintf(format, args...))
}

func NewCache(ctx context.Context, logger *slog.Logger) (cachev3.SnapshotCache, *grpc.Server) {
	cache := cachev3.NewSnapshotCache(false, cachev3.IDHash{}, cacheLogger{logger})
	callbacks := NewCallbacks(logger)
	xdsServer := serverv3.NewServer(ctx, cache, callbacks)
	grpcServer := grpc.NewServer()
	discoverygrpc.RegisterAggregatedDiscoveryServiceServer(grpcServer, xdsServer)
	return cache, grpcServer
}

func ServeGRPC(ctx context.Context, address string, server *grpc.Server, logger *slog.Logger) error {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	go func() {
		<-ctx.Done()
		server.GracefulStop()
	}()
	logger.Info("xDS gRPC server started", "address", address)
	if err := server.Serve(listener); err != nil {
		return fmt.Errorf("serve gRPC: %w", err)
	}
	return nil
}

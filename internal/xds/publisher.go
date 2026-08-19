package xds

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	luav3 "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/lua/v3"
	cachetypes "github.com/envoyproxy/go-control-plane/pkg/cache/types"
	cachev3 "github.com/envoyproxy/go-control-plane/pkg/cache/v3"
	resourcev3 "github.com/envoyproxy/go-control-plane/pkg/resource/v3"
	"google.golang.org/protobuf/types/known/anypb"
)

const (
	NodeID       = "edge-envoy"
	ResourceName = "tenant-auth"
)

type Publisher struct {
	cache  cachev3.SnapshotCache
	logger *slog.Logger
}

func NewPublisher(cache cachev3.SnapshotCache, logger *slog.Logger) *Publisher {
	return &Publisher{cache: cache, logger: logger}
}

func (p *Publisher) Publish(ctx context.Context, version uint64, source string, tokenCount int) error {
	// anypb.New uses the concrete luav3.Lua value's protobuf descriptor to set
	// luaAny.TypeUrl to:
	// type.googleapis.com/envoy.extensions.filters.http.lua.v3.Lua
	// Envoy uses that URL to decode the generic Any value as Lua configuration.
	luaAny, err := anypb.New(&luav3.Lua{DefaultSourceCode: &corev3.DataSource{
		Specifier: &corev3.DataSource_InlineString{InlineString: source},
	}})
	if err != nil {
		return fmt.Errorf("marshal Lua config: %w", err)
	}
	// ECDS identifies the resource by name, while TypedConfig carries its
	// concrete type and serialized value in the protobuf Any above.
	extension := &corev3.TypedExtensionConfig{Name: ResourceName, TypedConfig: luaAny}
	versionString := strconv.FormatUint(version, 10)
	snapshot, err := cachev3.NewSnapshot(versionString, map[resourcev3.Type][]cachetypes.Resource{
		resourcev3.ExtensionConfigType: {extension},
	})
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	if err := snapshot.Consistent(); err != nil {
		return fmt.Errorf("validate snapshot: %w", err)
	}
	if err := p.cache.SetSnapshot(ctx, NodeID, snapshot); err != nil {
		return fmt.Errorf("set snapshot: %w", err)
	}
	p.logger.Info("configuration published", "config_version", versionString, "token_count", tokenCount, "resource", ResourceName)
	return nil
}

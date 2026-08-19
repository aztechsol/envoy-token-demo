package config

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const defaultAppConfigPollInterval = 5 * time.Second

// AppConfigSource reads a deployed free-form configuration from the local AWS
// AppConfig Agent. The agent, not this process, handles AWS authentication,
// caching, and polling AppConfig for new deployment versions.
//
// AgentURL normally looks like:
// http://127.0.0.1:2772/applications/{app}/environments/{env}/configurations/{profile}
type AppConfigSource struct {
	AgentURL     string
	Client       *http.Client
	Logger       *slog.Logger
	PollInterval time.Duration
	ctx          context.Context
	version      string
}

func NewAppConfigSource(ctx context.Context, agentURL string, logger *slog.Logger) *AppConfigSource {
	return &AppConfigSource{
		AgentURL:     agentURL,
		Client:       &http.Client{Timeout: 5 * time.Second},
		Logger:       logger,
		PollInterval: defaultAppConfigPollInterval,
		ctx:          ctx,
	}
}

// Load retrieves and validates the current configuration from the local agent.
func (s *AppConfigSource) Load() (*TenantConfig, error) {
	cfg, version, err := s.load()
	if err != nil {
		return nil, err
	}
	s.version = version
	return cfg, nil
}

// Watch polls the local agent, which serves its cached AppConfig deployment.
// A new configuration is sent only when the agent reports a new version.
func (s *AppConfigSource) Watch(updates chan<- *TenantConfig) error {
	interval := s.PollInterval
	if interval <= 0 {
		interval = defaultAppConfigPollInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return nil
		case <-ticker.C:
			cfg, version, err := s.load()
			if err != nil {
				s.Logger.Error("AppConfig refresh failed; keeping last-known-good", "error", err)
				continue
			}
			if version == s.version {
				continue
			}
			s.version = version
			select {
			case updates <- cfg:
			case <-s.ctx.Done():
				return nil
			}
		}
	}
}

func (s *AppConfigSource) load() (*TenantConfig, string, error) {
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.AgentURL, nil)
	if err != nil {
		return nil, "", fmt.Errorf("create AppConfig request: %w", err)
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("request AppConfig Agent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, "", fmt.Errorf("request AppConfig Agent: unexpected status %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, "", fmt.Errorf("read AppConfig response: %w", err)
	}
	cfg, err := Parse(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parse AppConfig response: %w", err)
	}
	version := resp.Header.Get("Configuration-Version")
	if version == "" {
		version = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	return cfg, version, nil
}

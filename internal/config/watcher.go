package config

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

type FileSource struct {
	Path   string
	Logger *slog.Logger
	ctx    context.Context
}

func NewFileSource(ctx context.Context, path string, logger *slog.Logger) *FileSource {
	return &FileSource{Path: path, Logger: logger, ctx: ctx}
}

func (s *FileSource) Load() (*TenantConfig, error) { return LoadFile(s.Path) }

// Watch monitors the containing directory rather than only the file so that
// editor-style atomic rename/write operations are detected as well.
func (s *FileSource) Watch(updates chan<- *TenantConfig) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(filepath.Dir(s.Path)); err != nil {
		return err
	}

	var debounce <-chan time.Time
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case err := <-w.Errors:
			s.Logger.Error("config watch error", "error", err)
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if filepath.Clean(ev.Name) == filepath.Clean(s.Path) && ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
				s.Logger.Info("config change detected")
				debounce = time.After(150 * time.Millisecond)
			}
		case <-debounce:
			debounce = nil
			cfg, err := s.Load()
			if err != nil {
				s.Logger.Error("configuration rejected; keeping last-known-good", "error", err)
				continue
			}
			select {
			case updates <- cfg:
			case <-s.ctx.Done():
				return nil
			}
		}
	}
}

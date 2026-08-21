package config

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// InitLogger 根据配置初始化 slog。
//
// 如果 log.file 不为空，日志同时写入文件（追加）和 stderr。
// 如果 log.file 为空，只输出到 stderr。
func InitLogger(cfg *Config) error {
	level := slog.LevelInfo

	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	var writer io.Writer = os.Stderr

	if cfg.Log.File != "" {
		logDir := filepath.Dir(cfg.Log.File)
		if logDir != "." {
			if err := os.MkdirAll(logDir, 0o755); err != nil {
				return err
			}
		}

		f, err := os.OpenFile(
			cfg.Log.File,
			os.O_CREATE|os.O_WRONLY|os.O_APPEND,
			0o644,
		)
		if err != nil {
			return err
		}

		writer = io.MultiWriter(os.Stderr, f)
	}

	handler := slog.NewTextHandler(
		writer,
		&slog.HandlerOptions{
			Level: level,
		},
	)

	slog.SetDefault(slog.New(handler))

	return nil
}

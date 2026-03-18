package logx

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
)

const envLogLevel = "WACHT_LOG_LEVEL"

// Configure installs a text slog logger as the process default and bridges the
// stdlib log package into the same handler during the migration away from
// log.Printf.
func Configure(service string) *slog.Logger {
	logger := New(service, os.Stderr, os.Getenv(envLogLevel))
	slog.SetDefault(logger)

	stdlog := slog.NewLogLogger(logger.Handler(), slog.LevelInfo)
	log.SetFlags(0)
	log.SetOutput(stdlog.Writer())

	return logger
}

// New creates a text slog logger with the given service name.
func New(service string, w io.Writer, level string) *slog.Logger {
	if w == nil {
		w = io.Discard
	}
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: parseLevel(level),
	})
	return slog.New(handler).With("service", service)
}

func parseLevel(raw string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// EmailHash returns a short stable hash so auth events can be correlated
// without logging raw email addresses.
func EmailHash(email string) string {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if normalized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:8])
}

// URLHost returns only the host portion of a URL for safer logging.
func URLHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Hostname()
}

// TargetHost reduces a check target to its host-ish portion for structured
// logs. It preserves enough context for debugging without logging full URLs.
func TargetHost(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		return u.Hostname()
	}
	if host, _, err := net.SplitHostPort(target); err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.TrimSuffix(target, ".")
}

package log

import (
	"context"
	"log/slog"
)

// LineHandler is a slog.Handler that renders each record into a single
// human-readable line (timestamp + level + message + attributes) and forwards
// it to OnLine. OnLine can send the line to stdout, a GUI log buffer, etc.
type LineHandler struct {
	onLine func(line string)
	level  slog.Leveler
}

// NewLineHandler builds a handler that calls onLine for every enabled record.
func NewLineHandler(onLine func(line string)) *LineHandler {
	return &LineHandler{onLine: onLine, level: slog.LevelInfo}
}

// NewLogger builds a *slog.Logger that formats lines via onLine.
func NewLogger(onLine func(line string)) *slog.Logger {
	return slog.New(NewLineHandler(onLine))
}

func (h *LineHandler) Enabled(_ context.Context, l slog.Level) bool {
	return l >= h.level.Level()
}

func (h *LineHandler) Handle(_ context.Context, r slog.Record) error {
	ts := r.Time.Format("2006-01-02 15:04:05")
	line := ts + " [" + r.Level.String() + "] " + r.Message
	r.Attrs(func(a slog.Attr) bool {
		line += " " + a.Key + "=" + a.Value.String()
		return true
	})
	h.onLine(line)
	return nil
}

func (h *LineHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Base attributes are folded into the message by the caller when needed.
	return h
}

func (h *LineHandler) WithGroup(name string) slog.Handler {
	return h
}

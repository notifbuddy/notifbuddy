// Package logging configures the process-wide structured logger (log/slog).
//
// Production emits one JSON object per line on stdout with Datadog-friendly
// keys: `level`/`msg` map onto Datadog's status/message remappers as-is, the
// slog `time` key is renamed to `timestamp` (which Datadog's date remapper
// recognizes), and every record logged through a *Context call carries the
// active OpenTelemetry `trace_id`/`span_id`, so logs correlate with traces.
// Local dev uses the human-readable text handler instead.
//
// Do NOT capture the logger at package init (e.g. a package-level
// `slog.With(...)` variable): that snapshots the default logger before Setup
// runs in main. Call slog.Info/slog.ErrorContext/... directly, or derive
// loggers inside constructors.
package logging

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// Setup builds the logger described by format ("text" or "json") and level
// ("debug", "info", "warn", "error"), installs it as the slog default (which
// also reroutes the stdlib log package through it), and returns it. Empty
// format/level default to "text"/"info".
func Setup(format, level string) *slog.Logger {
	logger := New(format, level)
	slog.SetDefault(logger)
	return logger
}

// New builds a logger without installing it as the default. See Setup.
func New(format, level string) *slog.Logger {
	var h slog.Handler
	switch format {
	case "json":
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     parseLevel(level),
			AddSource: true,
			// Datadog's default date remapper does not recognize slog's
			// `time` key; `timestamp` it does.
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if len(groups) == 0 && a.Key == slog.TimeKey {
					a.Key = "timestamp"
				}
				return a
			},
		})
	default:
		h = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLevel(level)})
	}
	return slog.New(traceHandler{h})
}

func parseLevel(level string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil || level == "" {
		return slog.LevelInfo
	}
	return l
}

// traceHandler stamps the active OTel span's trace_id/span_id onto every
// record whose context carries one, which is what lets Datadog show the logs
// alongside the trace. It only fires for the *Context logging variants —
// plain slog.Info has no context to read.
type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}

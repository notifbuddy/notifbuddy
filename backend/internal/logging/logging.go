// Package logging configures the process-wide structured logger (log/slog).
//
// Production emits one JSON object per line on stdout with Datadog-friendly
// keys: `level`/`msg` map onto Datadog's status/message remappers as-is, the
// slog `time` key is renamed to `timestamp` (which Datadog's date remapper
// recognizes), and every record logged through a *Context call carries the
// active OpenTelemetry `trace_id`/`span_id`, so logs correlate with traces.
// Local dev uses the human-readable text handler instead.
//
// When cfg carries an Axiom token + dataset, every record is additionally
// shipped to Axiom (batched in the background by axiom-go); stdout stays the
// source of truth either way, so Cloud Logging keeps working as the fallback.
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

	adapter "github.com/axiomhq/axiom-go/adapters/slog"
	"github.com/axiomhq/axiom-go/axiom"
	"go.opentelemetry.io/otel/trace"

	"xolo/backend/internal/config"
)

// Setup builds the logger described by cfg, installs it as the slog default
// (which also reroutes the stdlib log package through it), and returns it plus
// a close function that flushes any buffered Axiom records — call it on
// shutdown (it is a no-op when Axiom is not configured).
func Setup(cfg config.LoggingConfig) (*slog.Logger, func()) {
	logger, closeFn := New(cfg)
	slog.SetDefault(logger)
	return logger, closeFn
}

// New builds a logger without installing it as the default. See Setup.
func New(cfg config.LoggingConfig) (*slog.Logger, func()) {
	var h slog.Handler
	switch cfg.Format {
	case "json":
		h = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level:     parseLevel(cfg.Level),
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
		h = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: parseLevel(cfg.Level)})
	}

	// AxiomEnabled is the single switch; validate() has already guaranteed
	// token+dataset are present when it is true.
	closeFn := func() {}
	if cfg.AxiomEnabled {
		ax, err := adapter.New(
			adapter.SetDataset(cfg.AxiomDataset),
			adapter.SetClientOptions(axiom.SetToken(cfg.AxiomToken)),
			adapter.SetLevel(parseLevel(cfg.Level)),
			adapter.SetAddSource(),
		)
		if err != nil {
			// Observability must not take the service down: log the failure
			// through the stdout handler and carry on without Axiom.
			slog.New(h).Error("axiom log shipping disabled", "error", err)
		} else {
			h = fanoutHandler{h, ax}
			closeFn = ax.Close
		}
	}

	return slog.New(traceHandler{h}), closeFn
}

func parseLevel(level string) slog.Level {
	var l slog.Level
	if err := l.UnmarshalText([]byte(level)); err != nil || level == "" {
		return slog.LevelInfo
	}
	return l
}

// fanoutHandler delivers each record to every handler that wants it (stdout +
// Axiom). Enabled short-circuits on the first taker so level filtering still
// works per handler.
type fanoutHandler []slog.Handler

func (f fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range f {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (f fanoutHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range f {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r.Clone()); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (f fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := make(fanoutHandler, len(f))
	for i, h := range f {
		next[i] = h.WithAttrs(attrs)
	}
	return next
}

func (f fanoutHandler) WithGroup(name string) slog.Handler {
	next := make(fanoutHandler, len(f))
	for i, h := range f {
		next[i] = h.WithGroup(name)
	}
	return next
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

package observability

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// --- ANSI colors ---
const (
	cReset   = "\033[0m"
	cBold    = "\033[1m"
	cDim     = "\033[2m"
	cRed     = "\033[31m"
	cGreen   = "\033[32m"
	cYellow  = "\033[33m"
	cBlue    = "\033[34m"
	cMagenta = "\033[35m"
	cCyan    = "\033[36m"
)

func color(s, c string) string { return c + s + cReset }
func dim(s string) string      { return color(s, cDim) }
func bold(s string) string     { return color(s, cBold) }

func colorStatus(code int) string {
	txt := fmt.Sprintf("%d %s", code, http.StatusText(code))
	switch {
	case code >= 500:
		return color(txt, cRed)
	case code >= 400:
		return color(txt, cYellow)
	case code >= 300:
		return color(txt, cMagenta)
	default: // 2xx
		return color(txt, cGreen)
	}
}

func colorMethod(m string) string {
	switch m {
	case http.MethodGet:
		return color(m, cCyan)
	case http.MethodPost:
		return color(m, cBlue)
	case http.MethodPut:
		return color(m, cMagenta)
	case http.MethodDelete:
		return color(m, cRed)
	default:
		return color(m, cYellow)
	}
}

const (
	ansiDim   = "\x1b[2m"
	ansiGreen = "\x1b[32m"
	ansiCyan  = "\x1b[36m"
	ansiReset = "\x1b[0m"
)

func LogSweeper(every time.Duration, removed int, dur time.Duration) {
	ts := time.Now().UTC().Format("2006/01/02 15:04:05")
	countColor := ansiGreen
	if removed == 0 {
		countColor = ansiDim
	}
	fmt.Printf("%s %sSWEEP%s  interval=%s  %sremoved=%d%s  %s(%s)%s\n",
		dim(ts), ansiCyan, ansiReset, every, countColor, removed, ansiReset, ansiDim, dur, ansiReset)
}

// PrettyHTTPLogger logs a human-friendly, colorized single line per request.
func PrettyHTTPLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w} // from your existing file
			next.ServeHTTP(sw, r)

			if sw.status == 0 {
				sw.status = http.StatusOK // implicit 200 if Write called w/o WriteHeader
			}
			dur := time.Since(start)
			ts := time.Now().Format("2006-01-02 15:04:05.000")

			method := colorMethod(r.Method)
			path := bold(r.URL.Path)
			if r.URL.RawQuery != "" {
				path += color("?"+r.URL.RawQuery, cDim)
			}
			remote := dim(r.RemoteAddr)
			ua := dim(r.UserAgent())
			bytes := fmt.Sprintf("%dB", sw.bytes)

			line := fmt.Sprintf("%s  %s  %s  → %s  (%s, %s)  %s  %s",
				dim(ts), method, path, colorStatus(sw.status),
				bytes, dur.Truncate(time.Microsecond), remote, ua,
			)
			fmt.Println(line)
		})
	}
}

type NowFunc func() time.Time

func PrettyHTTPLoggerWithClock(now NowFunc) func(http.Handler) http.Handler {
	if now == nil {
		now = time.Now
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			if sw.status == 0 {
				sw.status = http.StatusOK
			}

			ts := now().UTC().Format(time.RFC3339Nano) // ← uses injected clock
			dur := time.Since(start)

			method := colorMethod(r.Method)
			path := bold(r.URL.Path)
			if r.URL.RawQuery != "" {
				path += color("?"+r.URL.RawQuery, cDim)
			}
			remote := dim(r.RemoteAddr)
			ua := dim(r.UserAgent())
			bytes := fmt.Sprintf("%dB", sw.bytes)

			line := fmt.Sprintf("%s  %s  %s  → %s  (%s, %s)  %s  %s",
				dim(ts), method, path, colorStatus(sw.status),
				bytes, dur.Truncate(time.Microsecond), remote, ua,
			)
			fmt.Println(line)
		})
	}
}

// statusWriter captures status code and bytes written.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		// Write was called without WriteHeader -> implicit 200
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

// HTTPLogger returns middleware that logs each request after it completes.
func HTTPLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)

			logger.Info("http_request",
				"method", r.Method,
				"path", r.URL.Path,
				"query", r.URL.RawQuery,
				"status", sw.status,
				"bytes", sw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
				"ua", r.UserAgent(),
			)
		})
	}
}

// Recoverer converts panics to 500s and logs the panic.
func Recoverer(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic", "err", rec)
					http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

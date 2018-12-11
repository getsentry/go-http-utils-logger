package logger

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DataDog/datadog-go/statsd"
)

// Type represents logger's type
type Type int

const (
	// Version is this package's version
	Version = "0.3.0"

	// CombineLoggerType is the standard Apache combined log output
	//
	// format:
	//
	// :remote-addr - :remote-user [:date[clf]] ":method :url
	// HTTP/:http-version" :status :res[content-length] ":referrer" ":user-agent"
	CombineLoggerType Type = iota
	// CommonLoggerType is the standard Apache common log output
	//
	// format:
	//
	// :remote-addr - :remote-user [:date[clf]] ":method :url
	// HTTP/:http-version" :status :res[content-length]
	CommonLoggerType
	// DevLoggerType is useful for development
	//
	// format:
	//
	// :method :url :status :response-time ms - :res[content-length]
	DevLoggerType
	// ShortLoggerType is shorter than common, including response time
	//
	// format:
	//
	// :remote-addr :remote-user :method :url HTTP/:http-version :status
	// :res[content-length] - :response-time ms
	ShortLoggerType
	// TinyLoggerType is the minimal ouput
	//
	// format:
	//
	// :method :url :status :res[content-length] - :response-time ms
	TinyLoggerType

	timeFormat = "02/Jan/2006:15:04:05 -0700"
)

type responseLogger struct {
	rw     http.ResponseWriter
	start  time.Time
	status int
	size   int
}

func (rl *responseLogger) Header() http.Header {
	return rl.rw.Header()
}

func (rl *responseLogger) Write(bytes []byte) (int, error) {
	if rl.status == 0 {
		rl.status = http.StatusOK
	}

	size, err := rl.rw.Write(bytes)

	rl.size += size

	return size, err
}

func (rl *responseLogger) WriteHeader(status int) {
	rl.status = status

	rl.rw.WriteHeader(status)
}

func (rl *responseLogger) Flush() {
	f, ok := rl.rw.(http.Flusher)

	if ok {
		f.Flush()
	}
}

type loggerHandler struct {
	h          http.Handler
	formatType Type
	writer     io.Writer
	logFn      func(io.Writer, *responseLogger, *http.Request)
	stats      *statsd.Client
}

func (rh loggerHandler) ServeHTTP(res http.ResponseWriter, req *http.Request) {
	rl := &responseLogger{rw: res, start: time.Now()}

	rh.h.ServeHTTP(rl, req)

	rh.logFn(rh.writer, rl, req)

	if rh.stats != nil {
		tags := []string{
			"status:" + strconv.Itoa(rl.status),
			"method:" + req.Method,
		}

		rh.stats.Incr("http.response", tags, 1)
		rh.stats.Gauge("http.size", float64(rl.size), tags, 1)
		rh.stats.Timing("http.response", time.Now().Sub(rl.start), tags, 1)
	}
}

func extractUsername(req *http.Request) string {
	username := "-"

	if req.URL.User != nil {
		if name := req.URL.User.Username(); name != "" {
			username = name
		}
	}

	return username
}

func extractRemoteIP(req *http.Request) string {
	host, _, _ := net.SplitHostPort(req.RemoteAddr)
	return host
}

func parseResponseTime(start time.Time) string {
	return fmt.Sprintf("%.3f ms", time.Now().Sub(start).Seconds()/1e6)
}

// DefaultHandler returns a http.Handler that wraps h by using
// Apache combined log output and print to os.Stdout
func DefaultHandler(h http.Handler) http.Handler {
	return Handler(h, os.Stdout, CombineLoggerType, nil)
}

// Handler returns a http.Hanlder that wraps h by using t type log output
// and print to writer
func Handler(h http.Handler, writer io.Writer, t Type, stats *statsd.Client) http.Handler {
	return loggerHandler{
		h:      h,
		writer: writer,
		logFn:  logFnForType(t),
		stats:  stats,
	}
}

func logFnForType(t Type) func(io.Writer, *responseLogger, *http.Request) {
	switch t {
	case CombineLoggerType:
		return func(w io.Writer, rl *responseLogger, req *http.Request) {
			fmt.Fprintln(w, strings.Join([]string{
				extractRemoteIP(req),
				"-",
				extractUsername(req),
				"[" + rl.start.Format(timeFormat) + "]",
				`"` + req.Method,
				req.RequestURI,
				req.Proto + `"`,
				strconv.Itoa(rl.status),
				strconv.Itoa(rl.size),
				`"` + req.Referer() + `"`,
				`"` + req.UserAgent() + `"`,
			}, " "))
		}
	case CommonLoggerType:
		return func(w io.Writer, rl *responseLogger, req *http.Request) {
			fmt.Fprintln(w, strings.Join([]string{
				extractRemoteIP(req),
				"-",
				extractUsername(req),
				"[" + rl.start.Format(timeFormat) + "]",
				`"` + req.Method,
				req.RequestURI,
				req.Proto + `"`,
				strconv.Itoa(rl.status),
				strconv.Itoa(rl.size),
			}, " "))
		}
	case DevLoggerType:
		return func(w io.Writer, rl *responseLogger, req *http.Request) {
			fmt.Fprintln(w, strings.Join([]string{
				req.Method,
				req.RequestURI,
				strconv.Itoa(rl.status),
				parseResponseTime(rl.start),
				"-",
				strconv.Itoa(rl.size),
			}, " "))
		}
	case ShortLoggerType:
		return func(w io.Writer, rl *responseLogger, req *http.Request) {
			fmt.Fprintln(w, strings.Join([]string{
				extractRemoteIP(req),
				extractUsername(req),
				req.Method,
				req.RequestURI,
				req.Proto,
				strconv.Itoa(rl.status),
				strconv.Itoa(rl.size),
				"-",
				parseResponseTime(rl.start),
			}, " "))
		}
	case TinyLoggerType:
		return func(w io.Writer, rl *responseLogger, req *http.Request) {
			fmt.Fprintln(w, strings.Join([]string{
				req.Method,
				req.RequestURI,
				strconv.Itoa(rl.status),
				strconv.Itoa(rl.size),
				"-",
				parseResponseTime(rl.start),
			}, " "))
		}
	}
	panic("Should never get here.")
}

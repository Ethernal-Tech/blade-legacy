package jsonrpc

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"runtime/pprof"
	"runtime/trace"
	"strings"
	"sync"

	"github.com/hashicorp/go-bexpr"
)

// HandlerT implements the debugging API.
// Do not create values of this type, use the one
// in the Handler variable instead.
type HandlerT struct {
	mu        sync.Mutex
	cpuW      io.WriteCloser
	cpuFile   string
	traceW    io.WriteCloser
	traceFile string
}

// StartCPUProfile turns on CPU profiling, writing to the given file.
func (h *HandlerT) StartCPUProfile(file string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cpuW != nil {
		return errors.New("CPU profiling already in progress")
	}

	f, err := os.Create(expandHome(file))
	if err != nil {
		return err
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()

		return err
	}

	h.cpuW = f
	h.cpuFile = file

	return nil
}

// StopCPUProfile stops an ongoing CPU profile.
func (h *HandlerT) StopCPUProfile() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cpuW == nil {
		return errors.New("CPU profiling not in progress")
	}

	pprof.StopCPUProfile()

	h.cpuW.Close()
	h.cpuW = nil
	h.cpuFile = ""

	return nil
}

// StartGoTrace turns on tracing, writing to the given file.
func (h *HandlerT) StartGoTrace(file string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.traceW != nil {
		return errors.New("trace already in progress")
	}

	f, err := os.Create(expandHome(file))

	if err != nil {
		return err
	}

	if err := trace.Start(f); err != nil {
		f.Close()

		return err
	}

	h.traceW = f
	h.traceFile = file

	return nil
}

// StopTrace stops an ongoing trace.
func (h *HandlerT) StopGoTrace() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.traceW == nil {
		return errors.New("trace not in progress")
	}

	trace.Stop()

	h.traceW.Close()
	h.traceW = nil
	h.traceFile = ""

	return nil
}

// Stacks returns a printed representation of the stacks of all goroutines. It
// also permits the following optional filters to be used:
//   - filter: boolean expression of packages to filter for
func (*HandlerT) Stacks(filter *string) (string, error) {
	buf := new(bytes.Buffer)
	err := pprof.Lookup("goroutine").WriteTo(buf, 2)

	if err != nil {
		return "", err
	}

	// If any filtering was requested, execute them now
	if filter != nil && len(*filter) > 0 {
		expanded := *filter

		// The input filter is a logical expression of package names. Transform
		// it into a proper boolean expression that can be fed into a parser and
		// interpreter:
		//
		// E.g. (eth || snap) && !p2p -> (eth in Value || snap in Value) && p2p not in Value
		expanded = regexp.MustCompile(`[:/\.A-Za-z0-9_-]+`).ReplaceAllString(expanded, "`$0` in Value")
		expanded = regexp.MustCompile("!(`[:/\\.A-Za-z0-9_-]+`)").ReplaceAllString(expanded, "$1 not")
		expanded = strings.ReplaceAll(expanded, "||", "or")
		expanded = strings.ReplaceAll(expanded, "&&", "and")

		expr, err := bexpr.CreateEvaluator(expanded)
		if err != nil {
			return "", fmt.Errorf("failed to parse filter expression: expanded=%v, err=%w", expanded, err)
		}
		// Split the goroutine dump into segments and filter each
		dump := buf.String()
		buf.Reset()

		for _, trace := range strings.Split(dump, "\n\n") {
			if ok, _ := expr.Evaluate(map[string]string{"Value": trace}); ok {
				buf.WriteString(trace)
				buf.WriteString("\n\n")
			}
		}
	}

	return buf.String(), nil
}

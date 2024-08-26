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

var (
	errCPUProfilingInProgress    = errors.New("CPU profiling already in progress")
	errCPUProfilingNotInProgress = errors.New("CPU profiling not in progress")
	errTraceAlreadyInProgress    = errors.New("trace already in progress")
	errTraceNotInProgress        = errors.New("trace not in progress")
	identifierRegex              = regexp.MustCompile(`[:/\.A-Za-z0-9_-]+`)
	notRegex                     = regexp.MustCompile(`!([:/\.A-Za-z0-9_-]+)`)
)

// DebugHandler implements the debugging API.
// Do not create values of this type, use the one
// in the Handler variable instead.
type DebugHandler struct {
	mux            sync.Mutex
	cpuW           io.WriteCloser
	cpuProfileFile string
	traceW         io.WriteCloser
	traceFile      string
}

// StartCPUProfile turns on CPU profiling, writing to the given file.
func (debug *DebugHandler) StartCPUProfile(file string) error {
	debug.mux.Lock()
	defer debug.mux.Unlock()

	if debug.cpuW != nil {
		return errCPUProfilingInProgress
	}

	f, err := os.Create(expandHome(file))
	if err != nil {
		return err
	}

	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()

		return err
	}

	debug.cpuW = f
	debug.cpuProfileFile = file

	return nil
}

// StopCPUProfile stops an ongoing CPU profile.
func (debug *DebugHandler) StopCPUProfile() error {
	debug.mux.Lock()
	defer debug.mux.Unlock()

	if debug.cpuW == nil {
		return errCPUProfilingNotInProgress
	}

	pprof.StopCPUProfile()

	debug.cpuW.Close()
	debug.cpuW = nil
	debug.cpuProfileFile = ""

	return nil
}

// StartGoTrace turns on tracing, writing to the given file.
func (debug *DebugHandler) StartGoTrace(file string) error {
	debug.mux.Lock()
	defer debug.mux.Unlock()

	if debug.traceW != nil {
		return errTraceAlreadyInProgress
	}

	f, err := os.Create(expandHome(file))

	if err != nil {
		return err
	}

	if err := trace.Start(f); err != nil {
		f.Close()

		return err
	}

	debug.traceW = f
	debug.traceFile = file

	return nil
}

// StopTrace stops an ongoing trace.
func (debug *DebugHandler) StopGoTrace() error {
	debug.mux.Lock()
	defer debug.mux.Unlock()

	if debug.traceW == nil {
		return errTraceNotInProgress
	}

	trace.Stop()

	debug.traceW.Close()
	debug.traceW = nil
	debug.traceFile = ""

	return nil
}

// Stacks returns a printed representation of the stacks of all goroutines. It
// also permits the following optional filters to be used:
//   - filter: boolean expression of packages to filter for
func (*DebugHandler) Stacks(filter *string) (string, error) {
	buf := new(bytes.Buffer)
	if err := pprof.Lookup("goroutine").WriteTo(buf, 2); err != nil {
		return "", err
	}

	// Apply filtering if a filter is provided
	if filter != nil && len(*filter) > 0 {
		expanded, err := expandFilter(*filter)
		if err != nil {
			return "", fmt.Errorf("failed to parse filter expression: expanded=%v, err=%w", expanded, err)
		}

		// Filter the goroutine stack trace
		if err := filterStackTrace(buf, expanded); err != nil {
			return "", err
		}
	}

	return buf.String(), nil
}

func expandFilter(filter string) (string, error) {
	expanded := identifierRegex.ReplaceAllString(filter, "$0 in Value")
	expanded = notRegex.ReplaceAllString(expanded, "$1 not")

	expanded = strings.ReplaceAll(expanded, "||", "or")
	expanded = strings.ReplaceAll(expanded, "&&", "and")

	return expanded, nil
}

func filterStackTrace(buf *bytes.Buffer, expanded string) error {
	expr, err := bexpr.CreateEvaluator(expanded)
	if err != nil {
		return err
	}

	dump := buf.String()
	buf.Reset()

	for _, trace := range strings.Split(dump, "\n\n") {
		if ok, _ := expr.Evaluate(map[string]string{"Value": trace}); ok {
			buf.WriteString(trace)
			buf.WriteString("\n\n")
		}
	}

	return nil
}

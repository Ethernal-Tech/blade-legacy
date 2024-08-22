package jsonrpc

import (
	"errors"
	"io"
	"os"
	"runtime/pprof"
	"runtime/trace"
	"sync"
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

	pprof.StopCPUProfile()

	if h.cpuW == nil {
		return errors.New("CPU profiling not in progress")
	}

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

	trace.Stop()

	if h.traceW == nil {
		return errors.New("trace not in progress")
	}

	h.traceW.Close()
	h.traceW = nil
	h.traceFile = ""

	return nil
}

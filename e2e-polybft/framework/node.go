package framework

import (
	"io"
	"os"
	"os/exec"
	"sync/atomic"
)

type Node struct {
	shuttingDown atomic.Bool
	cmd          *exec.Cmd
	doneCh       chan struct{}
	exitResult   *exitResult
}

func NewNode(binary string, args []string, stdout io.Writer) (*Node, error) {
	cmd := exec.Command(binary, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stdout

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	n := &Node{
		cmd:    cmd,
		doneCh: make(chan struct{}),
	}
	go n.run()

	return n, nil
}

func (n *Node) ExitResult() *exitResult {
	return n.exitResult
}

func (n *Node) Wait() <-chan struct{} {
	return n.doneCh
}

func (n *Node) run() {
	err := n.cmd.Wait()

	n.exitResult = &exitResult{
		Signaled: n.IsShuttingDown(),
		Err:      err,
	}
	close(n.doneCh)
	n.cmd = nil
}

func (n *Node) IsShuttingDown() bool {
	return n.shuttingDown.Load()
}

func (n *Node) Stop() error {
	if n.cmd == nil {
		// the server is already stopped
		return nil
	}

	if err := n.cmd.Process.Signal(os.Interrupt); err != nil {
		return err
	}

	n.shuttingDown.Store(true)
	<-n.Wait()

	return nil
}

type exitResult struct {
	Signaled bool
	Err      error
}

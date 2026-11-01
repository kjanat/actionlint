package analyzerhost

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
)

type outputBudget struct {
	mu       sync.Mutex
	limit    int64
	used     int64
	exceeded bool
	cancel   context.CancelFunc
}

type boundedCapture struct {
	budget *outputBudget
	data   bytes.Buffer
}

func (w *boundedCapture) Write(data []byte) (int, error) {
	w.budget.mu.Lock()
	defer w.budget.mu.Unlock()
	remaining := w.budget.limit - w.budget.used
	if int64(len(data)) > remaining {
		n, _ := w.data.Write(data[:int(remaining)])
		w.budget.used += int64(n)
		w.budget.exceeded = true
		w.budget.cancel()
		return n, fmt.Errorf("command output exceeded %d bytes", w.budget.limit)
	}
	n, err := w.data.Write(data)
	w.budget.used += int64(n)
	return n, err
}

func boundedCommandOutput(cmd *exec.Cmd, limit int64, combined bool, cancel context.CancelFunc) ([]byte, []byte, error) {
	budget := outputBudget{limit: limit, cancel: cancel}
	stdout, stderr := &boundedCapture{budget: &budget}, &boundedCapture{budget: &budget}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if combined {
		cmd.Stderr = stdout
	}
	err := cmd.Run()
	if budget.exceeded {
		err = fmt.Errorf("command output exceeded %d bytes", limit)
	}
	return stdout.data.Bytes(), stderr.data.Bytes(), err
}

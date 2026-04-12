//go:build !windows

package claude_runtime

import (
	"os/exec"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
)

const gracefulShutdownDelay = 500 * time.Millisecond

// procGroupCleanup kills the entire process tree on context cancel and after normal exit.
// Prevents orphaned node subagents and MCP servers from accumulating.
type procGroupCleanup struct {
	cmd      *exec.Cmd
	done     chan struct{}
	once     sync.Once
	killOnce sync.Once
	err      error
	log      *zap.Logger
}

func newProcGroupCleanup(cmd *exec.Cmd, cancelCh <-chan struct{}, log *zap.Logger) *procGroupCleanup {
	pg := &procGroupCleanup{cmd: cmd, done: make(chan struct{}), log: log}
	go pg.watchCancel(cancelCh)
	return pg
}

func (pg *procGroupCleanup) watchCancel(cancelCh <-chan struct{}) {
	select {
	case <-cancelCh:
		pg.killOnce.Do(pg.kill)
	case <-pg.done:
	}
}

func (pg *procGroupCleanup) kill() {
	proc := pg.cmd.Process
	if proc == nil || proc.Pid <= 0 {
		return
	}
	pgid := -proc.Pid

	// check if the process group still exists (signal 0 = check only)
	if err := syscall.Kill(pgid, 0); err == syscall.ESRCH {
		return // already gone - skip SIGTERM/sleep/SIGKILL
	}

	if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			return
		}
		pg.log.Warn("SIGTERM process group failed", zap.Int("pgid", pgid), zap.Error(err))
	}
	time.Sleep(gracefulShutdownDelay)
	if err := syscall.Kill(pgid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		pg.log.Warn("SIGKILL process group failed", zap.Int("pgid", pgid), zap.Error(err))
	}
}

func (pg *procGroupCleanup) Wait() error {
	pg.once.Do(func() {
		pg.err = pg.cmd.Wait()
		close(pg.done)
		pg.killOnce.Do(pg.kill) // reap any orphaned descendants after normal exit too
	})
	return pg.err
}

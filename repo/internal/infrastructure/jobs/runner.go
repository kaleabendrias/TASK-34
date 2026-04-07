package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Job is a unit of background work scheduled by Runner.
type Job struct {
	Name     string
	Interval time.Duration
	Run      func(ctx context.Context) error
}

// Runner periodically invokes jobs on independent tickers. It is the only
// place in the codebase that owns long-running goroutines so shutdown is
// deterministic.
type Runner struct {
	log    *slog.Logger
	jobs   []Job
	wg     sync.WaitGroup
	cancel context.CancelFunc
}

func NewRunner(log *slog.Logger) *Runner {
	return &Runner{log: log}
}

func (r *Runner) Add(j Job) {
	r.jobs = append(r.jobs, j)
}

func (r *Runner) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	r.cancel = cancel
	for _, j := range r.jobs {
		r.wg.Add(1)
		go r.loop(ctx, j)
	}
}

func (r *Runner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
	r.wg.Wait()
}

func (r *Runner) loop(ctx context.Context, j Job) {
	defer r.wg.Done()
	r.runOnce(ctx, j) // run immediately at startup
	t := time.NewTicker(j.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.runOnce(ctx, j)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context, j Job) {
	start := time.Now()
	if err := j.Run(ctx); err != nil {
		r.log.Warn("background job failed", "job", j.Name, "error", err, "took_ms", time.Since(start).Milliseconds())
		return
	}
	r.log.Debug("background job ran", "job", j.Name, "took_ms", time.Since(start).Milliseconds())
}

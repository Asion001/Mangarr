package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/db"
	"github.com/Asion001/mangarr/internal/model"
)

// Task is a command pushed on an interval. Task name == command name.
type Task struct {
	Name     string
	Interval time.Duration
	Body     map[string]any
	// RunOnStart runs a newly created task immediately instead of after one interval.
	RunOnStart bool
}

type Scheduler struct {
	db    *db.DB
	queue *Queue
	log   *slog.Logger
	tick  time.Duration

	mu    sync.Mutex
	tasks map[string]*Task
}

func NewScheduler(d *db.DB, q *Queue, log *slog.Logger) *Scheduler {
	s := &Scheduler{db: d, queue: q, log: log, tick: 30 * time.Second, tasks: map[string]*Task{}}
	q.OnDone(s.commandDone)
	return s
}

// Add registers (or updates the interval of) a scheduled task.
func (s *Scheduler) Add(ctx context.Context, t Task) error {
	s.mu.Lock()
	s.tasks[t.Name] = &t
	s.mu.Unlock()
	row := &model.ScheduledTask{Name: t.Name, IntervalMinutes: int(t.Interval / time.Minute)}
	if !t.RunOnStart {
		now := time.Now().UTC()
		row.LastExecution = &now // first run after one interval
	}
	_, err := s.db.NewInsert().Model(row).
		On("CONFLICT (name) DO UPDATE").Set("interval_minutes = EXCLUDED.interval_minutes").
		Exec(ctx)
	return err
}

// SetInterval changes a task interval at runtime.
func (s *Scheduler) SetInterval(ctx context.Context, name string, d time.Duration) error {
	s.mu.Lock()
	t, ok := s.tasks[name]
	if ok {
		t.Interval = d
	}
	s.mu.Unlock()
	if !ok {
		return nil
	}
	_, err := s.db.NewUpdate().Model((*model.ScheduledTask)(nil)).
		Set("interval_minutes = ?", int(d/time.Minute)).Where("name = ?", name).Exec(ctx)
	return err
}

func (s *Scheduler) Tasks(ctx context.Context) ([]model.ScheduledTask, error) {
	var out []model.ScheduledTask
	err := s.db.NewSelect().Model(&out).Order("name").Scan(ctx)
	return out, err
}

func (s *Scheduler) Run(ctx context.Context) {
	s.check(ctx)
	t := time.NewTicker(s.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.check(ctx)
		}
	}
}

func (s *Scheduler) check(ctx context.Context) {
	var rows []model.ScheduledTask
	if err := s.db.NewSelect().Model(&rows).Scan(ctx); err != nil {
		s.log.Error("scheduler: load tasks", "err", err)
		return
	}
	now := time.Now().UTC()
	for _, r := range rows {
		s.mu.Lock()
		t, ok := s.tasks[r.Name]
		s.mu.Unlock()
		if !ok || t.Interval <= 0 {
			continue
		}
		if r.LastExecution != nil && r.LastExecution.Add(t.Interval).After(now) {
			continue
		}
		if r.LastStart != nil && r.LastStart.After(now.Add(-t.Interval)) && (r.LastExecution == nil || r.LastStart.After(*r.LastExecution)) {
			continue // already pushed and not finished yet
		}
		if _, err := s.queue.Push(ctx, t.Name, t.Body, "scheduled"); err != nil {
			s.log.Error("scheduler: push", "task", t.Name, "err", err)
			continue
		}
		_, _ = s.db.NewUpdate().Model((*model.ScheduledTask)(nil)).Set("last_start = ?", now).Where("name = ?", r.Name).Exec(ctx)
	}
}

// commandDone records the execution time for tasks (manual runs count too).
func (s *Scheduler) commandDone(cmd *model.Command) {
	s.mu.Lock()
	_, ok := s.tasks[cmd.Name]
	s.mu.Unlock()
	if !ok || cmd.EndedAt == nil {
		return
	}
	_, _ = s.db.NewUpdate().Model((*model.ScheduledTask)(nil)).
		Set("last_execution = ?", *cmd.EndedAt).Where("name = ?", cmd.Name).Exec(context.Background())
}

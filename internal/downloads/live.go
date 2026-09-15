package downloads

import (
	"sync"
	"time"

	"github.com/Asion001/mangarr/internal/events"
	"github.com/Asion001/mangarr/internal/progress"
)

// EventProgress is published (at most once a second per job) while a job
// downloads or processes pages.
const EventProgress = "processing.progress"

// LiveProgress is a running job's current stage.
type LiveProgress struct {
	JobID    int64  `json:"jobId"`
	Kind     string `json:"kind"`
	Stage    string `json:"stage"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	BytesIn  int64  `json:"bytesIn"`
	BytesOut int64  `json:"bytesOut"`
	// Rate is items per second in this stage; ETA the seconds it still needs.
	Rate         float64   `json:"rate"`
	ETA          float64   `json:"eta"`
	StageStarted time.Time `json:"stageStarted"`
	JobStarted   time.Time `json:"jobStarted"`
}

// Live tracks running jobs' progress in memory (none of it is stored).
type Live struct {
	bus  *events.Bus
	mu   sync.Mutex
	jobs map[int64]*liveJob
}

type liveJob struct {
	p         LiveProgress
	published time.Time
}

func NewLive(bus *events.Bus) *Live { return &Live{bus: bus, jobs: map[int64]*liveJob{}} }

// Start registers a running job.
func (l *Live) Start(jobID int64, kind string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	l.jobs[jobID] = &liveJob{p: LiveProgress{JobID: jobID, Kind: kind, JobStarted: now, StageStarted: now}}
}

// Reporter returns a progress callback for the job.
func (l *Live) Reporter(jobID int64) progress.Func {
	return func(ev progress.Event) { l.Update(jobID, ev) }
}

// Update records ev for the job and publishes it (throttled).
func (l *Live) Update(jobID int64, ev progress.Event) {
	l.mu.Lock()
	j := l.jobs[jobID]
	if j == nil {
		l.mu.Unlock()
		return
	}
	now := time.Now()
	stageChanged := j.p.Stage != ev.Stage
	if stageChanged {
		j.p.StageStarted = now
	}
	j.p.Stage, j.p.Done, j.p.Total, j.p.BytesIn, j.p.BytesOut = ev.Stage, ev.Done, ev.Total, ev.BytesIn, ev.BytesOut
	j.p.Rate, j.p.ETA = 0, 0
	if el := now.Sub(j.p.StageStarted).Seconds(); el > 0.5 && ev.Done > 0 {
		j.p.Rate = float64(ev.Done) / el
		j.p.ETA = float64(ev.Total-ev.Done) / j.p.Rate
	}
	publish := stageChanged || ev.Done >= ev.Total || now.Sub(j.published) >= time.Second
	if publish {
		j.published = now
	}
	snap := j.p
	l.mu.Unlock()
	if publish && l.bus != nil {
		l.bus.Publish(events.Event{Type: EventProgress, Payload: snap})
	}
}

// Finish forgets a job.
func (l *Live) Finish(jobID int64) {
	l.mu.Lock()
	_, ok := l.jobs[jobID]
	delete(l.jobs, jobID)
	l.mu.Unlock()
	if ok && l.bus != nil {
		l.bus.Publish(events.Event{Type: EventProgress, Payload: LiveProgress{JobID: jobID, Stage: "done"}})
	}
}

// Get returns a job's progress.
func (l *Live) Get(jobID int64) (LiveProgress, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	j, ok := l.jobs[jobID]
	if !ok {
		return LiveProgress{}, false
	}
	return j.p, true
}

// All returns every running job's progress.
func (l *Live) All() []LiveProgress {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]LiveProgress, 0, len(l.jobs))
	for _, j := range l.jobs {
		out = append(out, j.p)
	}
	return out
}

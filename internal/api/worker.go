package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Asion001/mangarr/internal/access"
	"github.com/Asion001/mangarr/internal/model"
	"github.com/Asion001/mangarr/internal/worktasks"
)

func init() { register((*Server).registerWorkerProtocol) }

// The worker protocol. A worker dials in, says hello, and then asks for one
// task at a time; the server never connects to it. Every task-scoped call
// checks that this worker still holds the task and answers 409 otherwise,
// which the worker treats as "drop it and ask again".

// WorkerHello is what a worker says about itself when it starts.
type WorkerHello struct {
	Version  string `json:"version,omitempty"`
	Platform string `json:"platform,omitempty"`
	// Roles it is able to do (the server answers with the ones it may).
	Roles []string `json:"roles,omitempty"`
	// Info is anything else worth showing: its upscaling devices, its cores.
	Info map[string]any `json:"info,omitempty"`
}

// WorkerWelcome is what the worker is told in return.
type WorkerWelcome struct {
	WorkerID int64  `json:"workerId"`
	Name     string `json:"name"`
	// Roles are the ones it may actually do here.
	Roles []string `json:"roles"`
	// LeaseSeconds is how long a task is held between heartbeats.
	LeaseSeconds int `json:"leaseSeconds"`
	// PollSeconds is how long a lease request waits before answering empty.
	PollSeconds int `json:"pollSeconds"`
	// Prefetch is how many pages to fetch ahead of what has been uploaded.
	Prefetch int `json:"prefetch"`
	// Concurrent is how many tasks it may hold at once.
	Concurrent int    `json:"concurrent"`
	ServerTime string `json:"serverTime"`
}

// WorkerTaskOutput is one task, as handed to a worker.
type WorkerTaskOutput struct {
	Task *model.WorkerTask `json:"task,omitempty"`
}

// workerPoll is how long a lease request waits for work before answering
// empty. Well under any sensible proxy timeout.
const workerPoll = 25 * time.Second

// pollEvery is how often the server looks for work while a worker waits.
const pollEvery = time.Second

func (s *Server) registerWorkerProtocol() {
	tags := []string{"Workers"}

	huma.Register(s.api, huma.Operation{OperationID: "worker-hello", Method: http.MethodPost, Path: "/api/v1/worker/hello", Tags: tags,
		Summary: "Announce a worker and learn what it may do"},
		func(ctx context.Context, in *struct{ Body WorkerHello }) (*struct{ Body WorkerWelcome }, error) {
			w, err := s.worker(ctx)
			if err != nil {
				return nil, err
			}
			info := in.Body.Info
			if info == nil {
				info = map[string]any{}
			}
			now := time.Now().UTC()
			w.Version, w.Platform, w.Info, w.LastSeenAt = in.Body.Version, in.Body.Platform, info, &now
			if _, err := s.app.DB.NewUpdate().Model(w).Column("version", "platform", "info", "last_seen_at").WherePK().Exec(ctx); err != nil {
				return nil, toHTTPError(err)
			}
			s.app.Auth.InvalidateWorkers()
			dl, _ := s.app.Settings.Downloads(ctx)
			welcome := WorkerWelcome{WorkerID: w.ID, Name: w.Name, Roles: allowedRoles(w, in.Body.Roles),
				LeaseSeconds: int(worktasks.Lease / time.Second), PollSeconds: int(workerPoll / time.Second),
				Prefetch: dl.WorkerPrefetch, Concurrent: max(dl.MaxConcurrentPerWorker, 1), ServerTime: now.Format(time.RFC3339)}
			return &struct{ Body WorkerWelcome }{welcome}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "worker-lease", Method: http.MethodPost, Path: "/api/v1/worker/lease", Tags: tags,
		Summary: "Ask for a task (waits a while when there is none)"},
		func(ctx context.Context, in *struct {
			Body struct {
				// Kinds it is ready to take right now (a busy worker asks for
				// less than it can do).
				Kinds []string `json:"kinds"`
			}
		}) (*struct{ Body WorkerTaskOutput }, error) {
			w, err := s.worker(ctx)
			if err != nil {
				return nil, err
			}
			kinds := allowedRoles(w, in.Body.Kinds)
			deadline := time.Now().Add(workerPoll)
			for {
				task, err := s.app.Tasks.Claim(ctx, w.ID, kinds)
				if err != nil {
					return nil, toHTTPError(err)
				}
				if task != nil {
					return &struct{ Body WorkerTaskOutput }{WorkerTaskOutput{Task: task}}, nil
				}
				if time.Now().After(deadline) {
					return &struct{ Body WorkerTaskOutput }{WorkerTaskOutput{}}, nil
				}
				select {
				case <-ctx.Done():
					return &struct{ Body WorkerTaskOutput }{WorkerTaskOutput{}}, nil
				case <-time.After(pollEvery):
				}
			}
		})

	huma.Register(s.api, huma.Operation{OperationID: "worker-page", Method: http.MethodPut, Path: "/api/v1/worker/tasks/{id}/pages/{n}", Tags: tags,
		Summary: "Upload one finished page of a download task", DefaultStatus: http.StatusNoContent,
		MaxBodyBytes: 256 << 20},
		func(ctx context.Context, in *struct {
			ID      int64  `path:"id"`
			N       int    `path:"n" minimum:"1"`
			RawBody []byte `contentType:"application/octet-stream"`
		}) (*struct{}, error) {
			w, err := s.worker(ctx)
			if err != nil {
				return nil, err
			}
			task, err := s.app.Tasks.Held(ctx, in.ID, w.ID)
			if err != nil {
				return nil, huma.Error409Conflict("this task is not yours any more")
			}
			if _, err := s.app.Downloads.AcceptPage(ctx, task.JobID, in.N-1, in.RawBody); err != nil {
				return nil, huma.Error422UnprocessableEntity(fmt.Sprintf("page %d: %v", in.N, err))
			}
			return &struct{}{}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "worker-heartbeat", Method: http.MethodPost, Path: "/api/v1/worker/tasks/{id}/heartbeat", Tags: tags,
		Summary: "Report progress and keep the task"},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				PagesDone  int   `json:"pagesDone"`
				PagesTotal int   `json:"pagesTotal"`
				BytesIn    int64 `json:"bytesIn"`
				BytesOut   int64 `json:"bytesOut"`
			}
		}) (*struct {
			Body struct {
				// Cancel: stop and hand the task back.
				Cancel bool `json:"cancel"`
			}
		}, error) {
			w, err := s.worker(ctx)
			if err != nil {
				return nil, err
			}
			p := worktasks.Progress{PagesDone: in.Body.PagesDone, PagesTotal: in.Body.PagesTotal, BytesIn: in.Body.BytesIn, BytesOut: in.Body.BytesOut}
			cancel, err := s.app.Tasks.Heartbeat(ctx, in.ID, w.ID, p)
			if err != nil {
				return nil, workerConflict(err)
			}
			if task, err := s.app.Tasks.Held(ctx, in.ID, w.ID); err == nil && task.Kind == model.TaskDownload {
				s.app.Downloads.TaskProgress(*task, in.Body.PagesDone, in.Body.PagesTotal)
			}
			out := &struct {
				Body struct {
					Cancel bool `json:"cancel"`
				}
			}{}
			out.Body.Cancel = cancel
			return out, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "worker-complete", Method: http.MethodPost, Path: "/api/v1/worker/tasks/{id}/complete", Tags: tags,
		Summary: "Say a task is done", DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				Pages    int   `json:"pages"`
				BytesIn  int64 `json:"bytesIn"`
				BytesOut int64 `json:"bytesOut"`
			}
		}) (*struct{}, error) {
			w, err := s.worker(ctx)
			if err != nil {
				return nil, err
			}
			task, err := s.app.Tasks.Held(ctx, in.ID, w.ID)
			if err != nil {
				return nil, huma.Error409Conflict("this task is not yours any more")
			}
			p := worktasks.Progress{PagesDone: in.Body.Pages, PagesTotal: task.PagesTotal, BytesIn: in.Body.BytesIn, BytesOut: in.Body.BytesOut}
			if err := s.app.Tasks.Finish(ctx, in.ID, w.ID, p); err != nil {
				return nil, workerConflict(err)
			}
			// the import runs here, and takes as long as it takes: the worker
			// is free as soon as its pages are in
			go func() {
				bg := context.WithoutCancel(ctx)
				if err := s.app.Downloads.TaskDone(bg, *task); err != nil {
					s.app.Log.Warn("could not finish a chapter a worker downloaded", "task", task.ID, "job", task.JobID, "err", err)
				}
			}()
			return &struct{}{}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "worker-fail", Method: http.MethodPost, Path: "/api/v1/worker/tasks/{id}/fail", Tags: tags,
		Summary: "Say a task could not be done", DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			ID   int64 `path:"id"`
			Body struct {
				Reason string `json:"reason"`
				Pages  int    `json:"pages,omitempty"`
			}
		}) (*struct{}, error) {
			w, err := s.worker(ctx)
			if err != nil {
				return nil, err
			}
			task, err := s.app.Tasks.Held(ctx, in.ID, w.ID)
			if err != nil {
				return nil, huma.Error409Conflict("this task is not yours any more")
			}
			reason := in.Body.Reason
			if reason == "" {
				reason = "the worker gave no reason"
			}
			if err := s.app.Tasks.Fail(ctx, in.ID, w.ID, reason, worktasks.Progress{PagesDone: in.Body.Pages, PagesTotal: task.PagesTotal}); err != nil {
				return nil, workerConflict(err)
			}
			s.app.Downloads.TaskFailed(context.WithoutCancel(ctx), *task, reason)
			return &struct{}{}, nil
		})

	huma.Register(s.api, huma.Operation{OperationID: "worker-bye", Method: http.MethodPost, Path: "/api/v1/worker/bye", Tags: tags,
		Summary: "Hand back everything this worker holds", DefaultStatus: http.StatusNoContent},
		func(ctx context.Context, in *struct {
			Body struct {
				// TaskIDs it is handing back (empty: everything it holds).
				TaskIDs []int64 `json:"taskIds,omitempty"`
			}
		}) (*struct{}, error) {
			w, err := s.worker(ctx)
			if err != nil {
				return nil, err
			}
			n, err := s.app.Tasks.HandBack(ctx, w.ID, in.Body.TaskIDs)
			if err != nil {
				return nil, toHTTPError(err)
			}
			if n > 0 {
				s.app.Log.Info("a worker handed its tasks back", "worker", w.Name, "tasks", n)
			}
			return &struct{}{}, nil
		})
}

// worker is the worker a request is from.
func (s *Server) worker(ctx context.Context) (*model.Worker, error) {
	p := access.From(ctx)
	if p == nil || p.Kind != access.KindWorker || p.WorkerID == 0 {
		return nil, huma.Error401Unauthorized("a worker key is required")
	}
	var w model.Worker
	if err := s.app.DB.NewSelect().Model(&w).Where("id = ?", p.WorkerID).Scan(ctx); err != nil {
		return nil, huma.Error401Unauthorized("this worker is gone")
	}
	if !w.Enabled {
		return nil, huma.Error403Forbidden("this worker is switched off")
	}
	return &w, nil
}

// allowedRoles is what a worker asked for, limited to what it may do.
func allowedRoles(w *model.Worker, asked []string) []string {
	out := []string{}
	for _, r := range model.WorkerRoles {
		if !w.HasRole(r) {
			continue
		}
		if len(asked) > 0 && !contains(asked, r) {
			continue
		}
		out = append(out, r)
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// workerConflict turns "not yours any more" into the 409 a worker knows how
// to handle.
func workerConflict(err error) error {
	if errors.Is(err, worktasks.ErrNotYours) {
		return huma.Error409Conflict("this task is not yours any more")
	}
	return toHTTPError(err)
}

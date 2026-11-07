package semantic

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/opencode"
)

var (
	ErrBusy          = errors.New("semantic generation is active; nothing was queued")
	ErrUnknown       = errors.New("semantic execution is uncertain; do not resend")
	ErrClosed        = errors.New("semantic runtime is unavailable")
	ErrNotFound      = errors.New("semantic generation not found")
	ErrStore         = errors.New("private semantic storage is unavailable")
	ErrConflict      = errors.New("semantic review changed; explicitly read saved state")
	ErrSaveUncertain = errors.New("semantic replacement is uncertain; explicitly read saved state")
)

// Persistence checks predecessor health and replaces validated complete content.
type Persistence interface {
	CheckSemantic(string, uint64) error
	SaveSemanticContent(string, string, Content, uint64) error
}

// Status contains only scoped progress and acknowledgement, never source/native payloads.
type Status struct {
	PRID             string `json:"prID"`
	GenerationID     string `json:"generationID"`
	ReviewID         string `json:"reviewID"`
	RevisionID       string `json:"revisionID"`
	Sequence         string `json:"sequence"`
	State            string `json:"state"`
	CancelRequested  bool   `json:"cancelRequested"`
	ExecutionUnknown bool   `json:"executionUnknown"`
	FailureCode      string `json:"failureCode,omitempty"`
	SavedVersion     string `json:"savedVersion,omitempty"`
}

type generation struct {
	status     Status
	revision   githubpr.Revision
	expected   uint64
	sequence   uint64
	stop       chan struct{}
	stopped    bool
	finished   bool
	committing bool
}

// Runtime owns separate restricted generation and bounded per-PR replacement slots.
type Runtime struct {
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	registry   Registry
	cache      *git.Cache
	store      Persistence
	items      map[string]*generation
	work       sync.WaitGroup
	closed     bool
	startOnce  sync.Once
	ready      chan struct{}
	service    *opencode.Service
	serviceErr error
	closeOnce  sync.Once
	closeErr   error
}

// New creates empty state without source reads, service startup or generation.
func New(registry Registry, cache *git.Cache) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	store, _ := registry.(Persistence)
	return &Runtime{ctx: ctx, cancel: cancel, registry: registry, cache: cache, store: store, items: map[string]*generation{}, ready: make(chan struct{})}
}

// available checks bounded slot admission while the runtime mutex is held.
func (r *Runtime) available(prID string) (*generation, error) {
	if r.closed {
		return nil, ErrClosed
	}
	g := r.items[prID]
	if g != nil {
		if g.status.ExecutionUnknown {
			return nil, ErrUnknown
		}
		if !g.finished {
			return nil, ErrBusy
		}
	} else if len(r.items) >= 32 {
		return nil, ErrLimit
	}
	return g, nil
}

// Begin admits once against an exact saved predecessor, not durable generation progress.
func (r *Runtime) Begin(ctx context.Context, prID string, revision githubpr.Revision, expected uint64) (Status, error) {
	if r.registry == nil || r.cache == nil || revision.Validate(prID) != nil {
		return Status{}, ErrSource
	}
	pr, err := r.registry.ReadPR(prID)
	if err != nil || pr.ID != prID || pr.Validate() != nil {
		return Status{}, ErrSource
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	r.mu.Lock()
	previous, err := r.available(prID)
	r.mu.Unlock()
	if err != nil {
		return Status{}, err
	}
	if r.store == nil {
		return Status{}, ErrStore
	}
	if err := r.store.CheckSemantic(prID, expected); err != nil {
		return Status{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	current, err := r.available(prID)
	if err != nil {
		return Status{}, err
	}
	if current != previous {
		return Status{}, ErrConflict
	}
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return Status{}, ErrClosed
	}
	id, _ := revision.ID()
	g := &generation{status: Status{PRID: prID, GenerationID: hex.EncodeToString(b[:32]), ReviewID: hex.EncodeToString(b[32:]), RevisionID: id, Sequence: "1", State: "pending"}, revision: revision, expected: expected, sequence: 1, stop: make(chan struct{})}
	r.items[prID] = g
	r.work.Add(1)
	go r.execute(g)
	return g.status, nil
}

// advance changes the finite sequence with the runtime mutex held.
func (g *generation) advance() { g.sequence++; g.status.Sequence = strconv.FormatUint(g.sequence, 10) }

// stopOnce signals only the exact generation's session.
func (g *generation) stopOnce() {
	if !g.stopped {
		g.stopped = true
		close(g.stop)
	}
}

// Read observes the exact retained generation without source or native work.
func (r *Runtime) Read(prID, generationID string) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.items[prID]
	if !githubpr.ValidID(generationID) || g == nil || g.status.GenerationID != generationID {
		return Status{}, ErrNotFound
	}
	return g.status, nil
}

// Latest returns current detached feedback without dispatching anything.
func (r *Runtime) Latest(prID string) *Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g := r.items[prID]; g != nil {
		s := g.status
		return &s
	}
	return nil
}

// Cancel records intent before commit entry; it does not prove interruption or rollback.
func (r *Runtime) Cancel(prID, generationID, revisionID string) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.items[prID]
	if !githubpr.ValidID(generationID) || g == nil || g.status.GenerationID != generationID || g.status.RevisionID != revisionID {
		return Status{}, ErrNotFound
	}
	if r.closed {
		return Status{}, ErrClosed
	}
	if !g.finished && !g.committing && !g.status.CancelRequested {
		g.status.CancelRequested = true
		g.stopOnce()
		g.advance()
	}
	return g.status, nil
}

// nativeService starts one independently owned restricted child lazily.
func (r *Runtime) nativeService(ctx context.Context) (*opencode.Service, error) {
	r.mu.Lock()
	if r.closed || ctx.Err() != nil {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	r.startOnce.Do(func() {
		r.work.Add(1)
		go func() {
			defer r.work.Done()
			service, err := opencode.StartInvestigation(r.ctx)
			r.mu.Lock()
			r.service, r.serviceErr = service, err
			r.mu.Unlock()
			close(r.ready)
		}()
	})
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.ready:
		if r.serviceErr != nil {
			return nil, r.serviceErr
		}
		return r.service, r.service.Err()
	}
}

// proceed publishes content-free progress at known pre-dispatch or idle boundaries.
func (r *Runtime) proceed(g *generation, state string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g.status.CancelRequested || r.closed {
		g.status.State = "cancelled"
		g.finished = true
		g.advance()
		return false
	}
	if g.status.State != state {
		g.status.State = state
		g.advance()
	}
	return true
}

// finish preserves unknown outcomes and commit acknowledgements independently of cancellation.
func (r *Runtime) finish(g *generation, state, code string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !g.committing && state != "unknown" && (g.status.CancelRequested || r.closed) {
		state, code = "cancelled", ""
	}
	g.status.State, g.status.FailureCode = state, code
	g.status.ExecutionUnknown = state == "unknown"
	g.finished = true
	g.advance()
}

// fail reports bounded source/native/deadline categories without native diagnostics.
func (r *Runtime) fail(g *generation, ctx context.Context, err error) {
	code := "native-unavailable"
	if errors.Is(err, ErrSource) {
		code = "source-unavailable"
	}
	if errors.Is(err, ErrLimit) {
		code = "source-too-large"
	}
	if ctx.Err() != nil {
		code = "generation-timeout"
	}
	r.finish(g, "failed", code)
}

// execute dispatches one prompt after original-source revalidation and saves only complete results.
func (r *Runtime) execute(g *generation) {
	defer r.work.Done()
	ctx, cancel := context.WithTimeout(r.ctx, 4*time.Minute+50*time.Second)
	defer cancel()
	if !r.proceed(g, "pending") {
		return
	}
	source, err := Resolve(ctx, g.status.PRID, r.registry, r.cache, g.revision)
	if err != nil {
		r.fail(g, ctx, err)
		return
	}
	if !r.proceed(g, "generating") {
		return
	}
	service, err := r.nativeService(ctx)
	if err != nil {
		r.fail(g, ctx, err)
		return
	}
	if !r.proceed(g, "generating") {
		return
	}
	session, err := service.CreateSession(ctx, source.Directory())
	if err != nil {
		if errors.Is(err, opencode.ErrModelUnavailable) {
			r.finish(g, "failed", "native-unavailable")
		} else {
			r.finish(g, "unknown", "session-uncertain")
		}
		return
	}
	if !r.proceed(g, "generating") {
		return
	}
	verified, err := Resolve(ctx, g.status.PRID, r.registry, r.cache, g.revision)
	if err != nil || verified.Prompt() != source.Prompt() || verified.Directory() != source.Directory() {
		r.fail(g, ctx, ErrSource)
		return
	}
	overflow := false
	result := service.Execute(ctx, session, verified.Prompt(), g.stop, func(value string) {
		if len(value) > MaxOutputBytes {
			overflow = true
			r.mu.Lock()
			g.stopOnce()
			r.mu.Unlock()
		}
	})
	if !result.Idle {
		r.finish(g, "unknown", "execution-uncertain")
		return
	}
	if overflow || len(result.Text) > MaxOutputBytes || errors.Is(result.Err, opencode.ErrAnswerLimit) {
		r.finish(g, "failed", "output-too-large")
		return
	}
	if !r.proceed(g, "validating") {
		return
	}
	if ctx.Err() != nil {
		r.fail(g, ctx, ctx.Err())
		return
	}
	if result.State != "completed" || result.Err != nil {
		r.finish(g, "failed", "execution-failed")
		return
	}
	content, err := verified.Parse([]byte(result.Text))
	if err != nil {
		code := "invalid-output"
		if errors.Is(err, ErrLimit) {
			code = "output-too-large"
		}
		r.finish(g, "failed", code)
		return
	}
	if ctx.Err() != nil {
		r.fail(g, ctx, ctx.Err())
		return
	}
	r.mu.Lock()
	if g.status.CancelRequested || r.closed {
		g.status.State = "cancelled"
		g.finished = true
		g.advance()
		r.mu.Unlock()
		return
	}
	g.committing = true
	g.status.State = "saving"
	g.advance()
	r.mu.Unlock()
	err = r.store.SaveSemanticContent(g.status.PRID, g.status.ReviewID, content, g.expected)
	if err != nil {
		code := "save-failed"
		if errors.Is(err, ErrConflict) {
			code = "review-changed"
		} else if errors.Is(err, ErrSaveUncertain) {
			code = "save-uncertain"
		}
		r.finish(g, "failed", code)
		return
	}
	r.mu.Lock()
	g.status.State = "completed"
	g.status.SavedVersion = strconv.FormatUint(g.expected+1, 10)
	g.finished = true
	g.advance()
	r.mu.Unlock()
}

// Close drains owned activity and entered commits without initiating saves or retries.
func (r *Runtime) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.cancel()
		r.mu.Unlock()
		r.work.Wait()
		if r.service != nil {
			r.closeErr = r.service.Close()
		}
	})
	return r.closeErr
}

// String withholds source, generated content and native details in diagnostics.
func (r *Runtime) String() string { return "semantic runtime (private details withheld)" }

// GoString withholds private runtime details during debug formatting.
func (r *Runtime) GoString() string { return r.String() }

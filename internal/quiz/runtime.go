package quiz

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"sync"

	"github.com/SophearithSaing/sheldon/internal/git"
	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/opencode"
)

const maxGenerationPRs = 32

var (
	ErrBusy     = errors.New("another quiz generation is active for this PR; nothing was queued")
	ErrUnknown  = errors.New("quiz execution or session creation is uncertain; do not resend")
	ErrClosed   = errors.New("quiz runtime is closed")
	ErrNotFound = errors.New("quiz generation not found in the requested scope")
	ErrNotReady = errors.New("quiz result is not validated; nothing was saved")
)

// State describes generation only; Validated does not mean a quiz was saved.
type State string

const (
	Pending    State = "pending"
	Generating State = "generating"
	Validating State = "validating"
	Validated  State = "validated"
	Failed     State = "failed"
	Cancelled  State = "cancelled"
	Unknown    State = "unknown"
)

// Status contains bounded public feedback, never structured output, keys or native details.
type Status struct {
	PRID             string `json:"prID"`
	GenerationID     string `json:"generationID"`
	QuizID           string `json:"quizID"`
	RevisionID       string `json:"revisionID"`
	Sequence         string `json:"sequence"`
	State            State  `json:"state"`
	CancelRequested  bool   `json:"cancelRequested"`
	ExecutionUnknown bool   `json:"executionUnknown"`
	FailureCode      string `json:"failureCode,omitempty"`
}

type generation struct {
	status   Status
	revision githubpr.Revision
	sequence uint64
	stop     chan struct{}
	stopped  bool
	finished bool
	content  Content
}

// Runtime owns independent quiz activity and a lazy restricted service, not question context.
type Runtime struct {
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	registry     Registry
	cache        *git.Cache
	generations  map[string]*generation
	serviceOnce  sync.Once
	serviceReady chan struct{}
	service      *opencode.Service
	serviceErr   error
	work         sync.WaitGroup
	closed       bool
	closeOnce    sync.Once
	closeErr     error
}

// New creates empty quiz state without source loading, native startup or disk access.
func New(registry Registry, cache *git.Cache) *Runtime {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runtime{ctx: ctx, cancel: cancel, registry: registry, cache: cache, generations: make(map[string]*generation), serviceReady: make(chan struct{})}
}

// Begin reserves one PR slot and starts explicit app-owned generation, not durable admission.
func (r *Runtime) Begin(ctx context.Context, prID string, revision githubpr.Revision) (Status, error) {
	if !githubpr.ValidID(prID) || revision.Validate(prID) != nil || r.registry == nil || r.cache == nil {
		return Status{}, ErrSource
	}
	pr, err := r.registry.ReadPR(prID)
	if err != nil || pr.Validate() != nil || pr.ID != prID {
		return Status{}, ErrSource
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Status{}, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	previous := r.generations[prID]
	if previous != nil {
		if previous.status.ExecutionUnknown {
			return Status{}, ErrUnknown
		}
		if !previous.finished {
			return Status{}, ErrBusy
		}
	} else if len(r.generations) >= maxGenerationPRs {
		return Status{}, ErrLimit
	}
	ids := make([]byte, 64)
	if _, err := rand.Read(ids); err != nil {
		return Status{}, ErrClosed
	}
	revisionID, _ := revision.ID()
	g := &generation{status: Status{PRID: prID, GenerationID: hex.EncodeToString(ids[:32]), QuizID: hex.EncodeToString(ids[32:]), RevisionID: revisionID, Sequence: "1", State: Pending}, revision: revision, sequence: 1, stop: make(chan struct{})}
	r.generations[prID] = g
	r.work.Add(1)
	go r.execute(g)
	return g.status, nil
}

// lookupLocked verifies exact retained scope with the runtime mutex held.
func (r *Runtime) lookupLocked(prID, generationID string) (*generation, error) {
	if !githubpr.ValidID(prID) || !githubpr.ValidID(generationID) {
		return nil, ErrNotFound
	}
	g := r.generations[prID]
	if g == nil || g.status.GenerationID != generationID {
		return nil, ErrNotFound
	}
	return g, nil
}

// Read returns current exact-generation feedback without source/native work.
func (r *Runtime) Read(prID, generationID string) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, err := r.lookupLocked(prID, generationID)
	if err != nil {
		return Status{}, err
	}
	return g.status, nil
}

// Cancel requests only this exact generation's interruption; acknowledgement is not idle proof.
func (r *Runtime) Cancel(prID, generationID, revisionID string) (Status, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, err := r.lookupLocked(prID, generationID)
	if err != nil || g.status.RevisionID != revisionID {
		return Status{}, ErrNotFound
	}
	if r.closed {
		return Status{}, ErrClosed
	}
	if !g.finished && !g.status.CancelRequested {
		g.status.CancelRequested = true
		g.stopLocked()
		g.advanceLocked()
	}
	return g.status, nil
}

// Content returns a detached private validated result, never a saved or unanswered public DTO.
func (r *Runtime) Content(prID, generationID string) (Content, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	g, err := r.lookupLocked(prID, generationID)
	if err != nil {
		return Content{}, err
	}
	if g.status.State != Validated || r.closed {
		return Content{}, ErrNotReady
	}
	data, err := json.Marshal(g.content)
	if err != nil {
		return Content{}, ErrNotReady
	}
	var content Content
	if json.Unmarshal(data, &content) != nil {
		return Content{}, ErrNotReady
	}
	return content, nil
}

// advanceLocked advances finite status snapshots without retaining an event history.
func (g *generation) advanceLocked() {
	g.sequence++
	g.status.Sequence = strconv.FormatUint(g.sequence, 10)
}

// stopLocked closes the exact session's stop signal once, without asserting cancellation.
func (g *generation) stopLocked() {
	if !g.stopped {
		g.stopped = true
		close(g.stop)
	}
}

// nativeService starts at most one independently owned quiz service for the writer lifetime.
func (r *Runtime) nativeService(ctx context.Context) (*opencode.Service, error) {
	r.mu.Lock()
	if r.closed || ctx.Err() != nil {
		r.mu.Unlock()
		return nil, ErrClosed
	}
	r.serviceOnce.Do(func() {
		r.work.Add(1)
		go func() {
			defer r.work.Done()
			service, err := opencode.Start(r.ctx)
			r.mu.Lock()
			r.service, r.serviceErr = service, err
			r.mu.Unlock()
			close(r.serviceReady)
		}()
	})
	r.mu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.serviceReady:
		if r.serviceErr != nil {
			return nil, r.serviceErr
		}
		return r.service, r.service.Err()
	}
}

// Close cancels/drains owned generation and service resources without writes or retries.
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

// String withholds candidate questions, keys, native identities and source locations.
func (r *Runtime) String() string { return "runtime quiz generation (private details withheld)" }

// GoString keeps private runtime details out of debug formatting.
func (r *Runtime) GoString() string { return r.String() }

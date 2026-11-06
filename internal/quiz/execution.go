package quiz

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/SophearithSaing/sheldon/internal/opencode"
)

// execute prepares and executes once within 4m50s plus the adapter's bounded reconciliation.
func (r *Runtime) execute(g *generation) {
	defer r.work.Done()
	ctx, cancel := context.WithTimeout(r.ctx, 4*time.Minute+50*time.Second)
	defer cancel()
	if !r.proceed(g, Pending) {
		return
	}
	source, err := Resolve(ctx, g.status.PRID, r.registry, r.cache, g.revision)
	if err != nil {
		r.failPreparation(g, ctx, err)
		return
	}
	if !r.proceed(g, Generating) {
		return
	}
	service, err := r.nativeService(ctx)
	if err != nil {
		r.failPreparation(g, ctx, err)
		return
	}
	if !r.proceed(g, Generating) {
		return
	}
	session, err := service.CreateSession(ctx, source.Directory())
	if err != nil {
		if errors.Is(err, opencode.ErrModelUnavailable) {
			r.finish(g, Failed, "native-unavailable")
		} else {
			r.finish(g, Unknown, "session-uncertain")
		}
		return
	}
	if !r.proceed(g, Generating) {
		return
	}
	verified, err := Resolve(ctx, g.status.PRID, r.registry, r.cache, g.revision)
	if err != nil || verified.Prompt() != source.Prompt() || verified.Directory() != source.Directory() {
		r.failPreparation(g, ctx, ErrSource)
		return
	}
	overflow := false
	result := service.Execute(ctx, session, verified.Prompt(), g.stop, func(text string) {
		if len(text) > MaxOutputBytes {
			overflow = true
			r.mu.Lock()
			g.stopLocked()
			r.mu.Unlock()
		}
	})
	if !result.Idle {
		r.finish(g, Unknown, "execution-uncertain")
		return
	}
	if overflow || len(result.Text) > MaxOutputBytes || errors.Is(result.Err, opencode.ErrAnswerLimit) {
		r.finish(g, Failed, "output-too-large")
		return
	}
	if !r.proceed(g, Validating) {
		return
	}
	if ctx.Err() != nil {
		r.failPreparation(g, ctx, ctx.Err())
		return
	}
	if result.State != "completed" || result.Err != nil {
		r.finish(g, Failed, "execution-failed")
		return
	}
	content, err := verified.Parse([]byte(result.Text))
	if err != nil {
		code := "invalid-output"
		if errors.Is(err, ErrLimit) {
			code = "output-too-large"
		}
		r.finish(g, Failed, code)
		return
	}
	if ctx.Err() != nil {
		r.failPreparation(g, ctx, ctx.Err())
		return
	}
	r.save(g, content)
}

// proceed handles known pre-dispatch/idle cancellation and publishes content-free progress.
func (r *Runtime) proceed(g *generation, state State) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g.status.CancelRequested || r.closed {
		g.status.State = Cancelled
		g.finished = true
		g.advanceLocked()
		return false
	}
	if g.status.State != state {
		g.status.State = state
		g.advanceLocked()
	}
	return true
}

// failPreparation reports fixed source/native/deadline failures without raw diagnostics.
func (r *Runtime) failPreparation(g *generation, ctx context.Context, err error) {
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
	r.finish(g, Failed, code)
}

// finish publishes a terminal outcome without reclassifying already-started commit operations.
func (r *Runtime) finish(g *generation, state State, code string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !g.committing && state != Unknown && (g.status.CancelRequested || r.closed) {
		state, code = Cancelled, ""
	}
	g.status.State, g.status.FailureCode = state, code
	g.status.ExecutionUnknown = state == Unknown
	g.finished = true
	g.advanceLocked()
}

// save serializes Cancel/shutdown against commit entry, then saves without holding runtime locks.
func (r *Runtime) save(g *generation, content Content) {
	r.mu.Lock()
	if g.status.CancelRequested || r.closed {
		g.status.State, g.finished = Cancelled, true
		g.advanceLocked()
		r.mu.Unlock()
		return
	}
	g.status.State, g.committing = Saving, true
	g.advanceLocked()
	r.mu.Unlock()
	next := Saved{SchemaVersion: 1, PRID: g.status.PRID, QuizID: g.status.QuizID, Version: g.expected + 1, Content: content}
	err := r.store.SaveQuiz(next, g.expected)
	if err != nil {
		code := "save-failed"
		if errors.Is(err, ErrConflict) {
			code = "quiz-changed"
		} else if errors.Is(err, ErrSaveUncertain) {
			code = "save-uncertain"
		}
		r.finish(g, Failed, code)
		return
	}
	r.mu.Lock()
	g.status.SavedVersion = strconv.FormatUint(next.Version, 10)
	g.status.State, g.finished = Completed, true
	g.advanceLocked()
	r.mu.Unlock()
}

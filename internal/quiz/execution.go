package quiz

import (
	"context"
	"errors"
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
			r.finish(g, Failed, "native-unavailable", Content{})
		} else {
			r.finish(g, Unknown, "session-uncertain", Content{})
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
		r.finish(g, Unknown, "execution-uncertain", Content{})
		return
	}
	if overflow || len(result.Text) > MaxOutputBytes || errors.Is(result.Err, opencode.ErrAnswerLimit) {
		r.finish(g, Failed, "output-too-large", Content{})
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
		r.finish(g, Failed, "execution-failed", Content{})
		return
	}
	content, err := verified.Parse([]byte(result.Text))
	if err != nil {
		code := "invalid-output"
		if errors.Is(err, ErrLimit) {
			code = "output-too-large"
		}
		r.finish(g, Failed, code, Content{})
		return
	}
	if ctx.Err() != nil {
		r.failPreparation(g, ctx, ctx.Err())
		return
	}
	r.finish(g, Validated, "", content)
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
	r.finish(g, Failed, code, Content{})
}

// finish retains only validated private content and serializes cancellation against publication.
func (r *Runtime) finish(g *generation, state State, code string, content Content) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if state != Unknown && (g.status.CancelRequested || r.closed) {
		state, code, content = Cancelled, "", Content{}
	}
	g.status.State, g.status.FailureCode = state, code
	g.status.ExecutionUnknown = state == Unknown
	g.content, g.finished = content, true
	g.advanceLocked()
}

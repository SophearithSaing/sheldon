package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
	"github.com/SophearithSaing/sheldon/internal/question"
	"github.com/SophearithSaing/sheldon/internal/quiz"
)

// waitQuiz observes exact public statuses without exposing native structured output.
func waitQuiz(t *testing.T, runtime *quiz.Runtime, admitted quiz.Status) quiz.Status {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		latest, err := runtime.Read(admitted.PRID, admitted.GenerationID)
		if err != nil {
			t.Fatal(err)
		}
		assertQuizStatus(t, latest)
		switch latest.State {
		case quiz.Completed, quiz.Failed, quiz.Cancelled, quiz.Unknown:
			return latest
		}
		select {
		case <-ctx.Done():
			t.Fatal("generation did not reach a bounded outcome")
		case <-tick.C:
		}
	}
}

// assertQuizStatus checks public feedback carries neither keys/raw text nor native locations.
func assertQuizStatus(t *testing.T, status quiz.Status) {
	t.Helper()
	data, err := json.Marshal(status)
	if err != nil || len(data) > 4096 || !githubpr.ValidID(status.GenerationID) || !githubpr.ValidID(status.QuizID) || status.GenerationID == status.QuizID {
		t.Fatal("invalid bounded generation snapshot")
	}
	if status.State == quiz.Completed && status.SavedVersion == "" || status.State != quiz.Completed && status.SavedVersion != "" {
		t.Fatal("execution status disagrees with saved acknowledgement")
	}
	for _, private := range []string{"correctChoiceID", "explanation", "evidence", "HIDDEN", "RAW", "ses_fixture", "sheldon-opencode-", "Synthetic quiz"} {
		if bytes.Contains(data, []byte(private)) {
			t.Fatalf("generation status exposed private/success data: %s", private)
		}
	}
}

// quizPromptCount counts only actual native prompt calls in the synthetic process audit.
func quizPromptCount(t *testing.T, f questionFixture) int {
	t.Helper()
	count := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if entry.Method == "POST" && strings.HasSuffix(entry.Path, "/prompt") {
			count++
		}
	}
	return count
}

// awaitQuizPrompts waits for an audited dispatch rather than treating Generating as delivery proof.
func awaitQuizPrompts(t *testing.T, f questionFixture, count int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if quizPromptCount(t, f) >= count {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("synthetic quiz dispatch did not arrive")
		case <-tick.C:
		}
	}
}

func TestQuizGenerationCompletesOnlyAfterPrivateSaving(t *testing.T) {
	f := newQuestionFixture(t, "quiz-valid")
	runtime, err := f.owner.openQuizzes(f.paths)
	if err != nil || len(nativeQuestionAudit(t, f.audit)) != 0 {
		t.Fatal("opening runtime started native work")
	}
	ctx, cancel := context.WithCancel(context.Background())
	admitted, err := f.service.GenerateQuiz(ctx, f.pr.ID, f.input.Revision, 0)
	cancel()
	if err != nil || admitted.State != quiz.Pending || admitted.Sequence != "1" {
		t.Fatalf("quiz admission: %v", err)
	}
	latest := waitQuiz(t, runtime, admitted)
	if latest.State != quiz.Completed || latest.SavedVersion != "1" || latest.ExecutionUnknown || latest.CancelRequested || quizPromptCount(t, f) != 1 {
		t.Fatalf("expected completed saved result: %+v", latest)
	}
	content, err := f.owner.ReadQuiz(f.pr.ID)
	if err != nil || len(content.Questions) != 5 || content.Questions[0].Evidence[0].Selected[0].Text != "old literal" {
		t.Fatalf("validated private result: %v", err)
	}
	content.Questions[0].Evidence[0].Selected[0].Text = "local edit"
	content.Questions[0].Choices[0].Text = "local edit"
	again, err := f.owner.ReadQuiz(f.pr.ID)
	if err != nil || again.Questions[0].Evidence[0].Selected[0].Text == "local edit" || again.Questions[0].Choices[0].Text == "local edit" {
		t.Fatal("private candidate aliases caller data")
	}
	nativeBefore := len(nativeQuestionAudit(t, f.audit))
	if read, err := runtime.Read(f.pr.ID, admitted.GenerationID); err != nil || read != latest {
		t.Fatal("exact generation could not be read")
	}
	if stopped, err := runtime.Cancel(f.pr.ID, admitted.GenerationID, admitted.RevisionID); err != nil || stopped != latest {
		t.Fatal("late terminal Cancel changed outcome")
	}
	if len(nativeQuestionAudit(t, f.audit)) != nativeBefore {
		t.Fatal("read/late Cancel started native work")
	}
	second, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 1)
	if err != nil || second.GenerationID == admitted.GenerationID || second.QuizID == admitted.QuizID {
		t.Fatalf("explicit generation did not create fresh identities: %v", err)
	}
	if _, err := runtime.Cancel(f.pr.ID, admitted.GenerationID, admitted.RevisionID); !errors.Is(err, quiz.ErrNotFound) {
		t.Fatal("retired attempt could cancel newer generation")
	}
	if waitQuiz(t, runtime, second).State != quiz.Completed || quizPromptCount(t, f) != 2 {
		t.Fatal("fresh explicit generation did not validate once")
	}
	creates, defaults := 0, 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if entry.Method == "POST" && entry.Path == "/api/session" {
			creates++
		}
		if entry.Path == "/api/model/default" {
			defaults++
		}
	}
	if creates != 2 || defaults != 2 || f.owner.questions != nil {
		t.Fatal("quiz reused a question/session binding or consulted defaults during reads")
	}
	if saved, err := f.owner.ReadQuiz(f.pr.ID); err != nil || saved.Version != 2 || saved.QuizID != second.QuizID {
		t.Fatal("replacement was not saved")
	}
	if _, err := os.Lstat(filepath.Join(f.paths.Data, "reviews", f.pr.ID, "threads.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("quiz changed question storage")
	}
}

func TestQuizBusyAcrossRevisionsConcurrentPRsAndTargetedCancelLeaveQuestionsIntact(t *testing.T) {
	f := newQuestionFixture(t, "quiz-wait")
	turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	answer := finishNativeTurn(t, f, turn)
	threadPath := filepath.Join(f.paths.Data, "reviews", f.pr.ID, "threads.json")
	before, err := os.ReadFile(threadPath)
	if err != nil {
		t.Fatal(err)
	}
	admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	runtime := f.owner.quizzes
	awaitQuizPrompts(t, f, 2)
	var work sync.WaitGroup
	errorsSeen := make(chan error, 12)
	for range 12 {
		work.Go(func() {
			_, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
			errorsSeen <- err
		})
	}
	work.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if !errors.Is(err, quiz.ErrBusy) {
			t.Fatalf("overlapping generation was not rejected: %v", err)
		}
	}
	different := f.input.Revision
	different.BaseSHA = strings.Repeat("d", 40)
	if _, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, different, 0); !errors.Is(err, quiz.ErrBusy) {
		t.Fatal("different revision escaped the per-PR replacement gate")
	}
	if _, err := runtime.Cancel(f.pr.ID, admitted.GenerationID, strings.Repeat("e", 64)); !errors.Is(err, quiz.ErrNotFound) {
		t.Fatal("wrong-revision cancellation reached native work")
	}
	other, err := f.owner.RegisterPR("https://github.com/owner/repo/pull/32")
	if err != nil {
		t.Fatal(err)
	}
	otherInput := prepareQuestionRevision(t, f, other, f.input.Revision.BaseSHA, f.head)
	otherGeneration, err := f.service.GenerateQuiz(context.Background(), other.ID, otherInput.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	awaitQuizPrompts(t, f, 3)
	if read, err := runtime.Read(f.pr.ID, admitted.GenerationID); err != nil || read.State != quiz.Generating {
		t.Fatal("read-only observation changed accepted execution")
	}
	stop, err := runtime.Cancel(f.pr.ID, admitted.GenerationID, admitted.RevisionID)
	if err != nil || !stop.CancelRequested || stop.State != quiz.Generating {
		t.Fatalf("Cancel acknowledgement fabricated completion: %+v %v", stop, err)
	}
	if final := waitQuiz(t, runtime, admitted); final.State != quiz.Cancelled || final.ExecutionUnknown {
		t.Fatalf("targeted cancellation: %+v", final)
	}
	if otherStatus, err := runtime.Read(other.ID, otherGeneration.GenerationID); err != nil || otherStatus.State != quiz.Generating || otherStatus.CancelRequested {
		t.Fatal("cancel reached unrelated PR generation")
	}
	after, _ := os.ReadFile(threadPath)
	if !bytes.Equal(before, after) {
		t.Fatal("quiz activity changed question history/binding")
	}
	followup, err := f.service.BeginFollowup(context.Background(), f.pr.ID, answer.ThreadID, question.FollowupRequest{RevisionID: answer.RevisionID, Question: "A separate explicit follow-up."})
	if err != nil {
		t.Fatal("quiz occupied the question session busy gate")
	}
	finishNativeTurn(t, f, followup)
	if _, err := runtime.Cancel(other.ID, otherGeneration.GenerationID, otherGeneration.RevisionID); err != nil {
		t.Fatal(err)
	}
	if waitQuiz(t, runtime, otherGeneration).State != quiz.Cancelled || quizPromptCount(t, f) != 4 {
		t.Fatal("cross-PR/question work was queued or replayed")
	}
	data, _ := os.ReadFile(f.audit)
	services := map[int]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		var entry struct {
			Args []string
			PID  int
		}
		if json.Unmarshal(line, &entry) == nil && len(entry.Args) > 0 && entry.Args[0] == "serve" {
			services[entry.PID] = true
		}
	}
	if len(services) != 2 {
		t.Fatal("quiz did not own a service separate from questions")
	}
}

func TestQuizGenerationFailureRecoveryNeverReplaysOrPublishesInvalidContent(t *testing.T) {
	for _, mode := range []string{"quiz-invalid", "quiz-failed", "quiz-overflow", "quiz-feed-loss", "version-wrong", "model-missing", "answer-ambiguous-empty"} {
		t.Run(mode, func(t *testing.T) {
			f := newQuestionFixture(t, mode)
			admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
			if err != nil {
				t.Fatal(err)
			}
			runtime := f.owner.quizzes
			latest := waitQuiz(t, runtime, admitted)
			want, code, prompts := quiz.Failed, "invalid-output", 1
			switch mode {
			case "quiz-failed":
				code = "execution-failed"
			case "quiz-overflow":
				code = "output-too-large"
			case "quiz-feed-loss":
				want, code = quiz.Completed, ""
			case "version-wrong", "model-missing":
				code, prompts = "native-unavailable", 0
			case "answer-ambiguous-empty":
				want, code = quiz.Unknown, "execution-uncertain"
			}
			if latest.State != want || latest.FailureCode != code || quizPromptCount(t, f) != prompts {
				t.Fatalf("wrong synthetic outcome: %+v", latest)
			}
			if mode == "quiz-overflow" {
				interrupted := false
				for _, entry := range nativeQuestionAudit(t, f.audit) {
					interrupted = interrupted || strings.HasSuffix(entry.Path, "/interrupt")
				}
				if !interrupted || latest.CancelRequested {
					t.Fatal("output limit did not request a targeted internal interruption")
				}
			}
			if want != quiz.Completed {
				if content, err := f.owner.ReadQuiz(f.pr.ID); err != nil || content.Version != 0 || len(content.Questions) != 0 {
					t.Fatal("failure saved a private candidate")
				}
			}
			if want == quiz.Unknown {
				if !latest.ExecutionUnknown {
					t.Fatal("unknown admission lost uncertainty")
				}
				runtime.Cancel(f.pr.ID, admitted.GenerationID, admitted.RevisionID)
				if _, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0); !errors.Is(err, quiz.ErrUnknown) || quizPromptCount(t, f) != 1 {
					t.Fatal("unknown outcome allowed replacement/replay")
				}
			}
		})
	}
}

func TestQuizConcurrentAdmissionHasOneWinnerAndOneNativePrompt(t *testing.T) {
	f := newQuestionFixture(t, "quiz-wait")
	var work sync.WaitGroup
	type attempt struct {
		status quiz.Status
		err    error
	}
	results := make(chan attempt, 16)
	for range 16 {
		work.Go(func() {
			status, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
			results <- attempt{status, err}
		})
	}
	work.Wait()
	close(results)
	winners := 0
	var admitted quiz.Status
	for result := range results {
		if result.err == nil {
			winners++
			admitted = result.status
		} else if !errors.Is(result.err, quiz.ErrBusy) {
			t.Fatalf("unexpected admission rejection: %v", result.err)
		}
	}
	if winners != 1 {
		t.Fatal("simultaneous admission produced competing generations")
	}
	awaitQuizPrompts(t, f, 1)
	if _, err := f.owner.quizzes.Cancel(f.pr.ID, admitted.GenerationID, admitted.RevisionID); err != nil {
		t.Fatal(err)
	}
	if waitQuiz(t, f.owner.quizzes, admitted).State != quiz.Cancelled || quizPromptCount(t, f) != 1 {
		t.Fatal("competing requests dispatched/queued native work")
	}
}

func TestQuizUnexpectedApprovalCannotInvalidateQuestionService(t *testing.T) {
	f := newQuestionFixture(t, "quiz-valid")
	turn, err := f.service.BeginQuestion(context.Background(), f.pr.ID, f.input)
	if err != nil {
		t.Fatal(err)
	}
	answer := finishNativeTurn(t, f, turn)
	path := filepath.Join(f.paths.Data, "reviews", f.pr.ID, "threads.json")
	before, _ := os.ReadFile(path)
	t.Setenv("SHELDON_NATIVE_MODE", "approval")
	admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	final := waitQuiz(t, f.owner.quizzes, admitted)
	if final.State != quiz.Unknown && final.State != quiz.Failed {
		t.Fatal("unexpected approval hung or became generated success")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || quizPromptCount(t, f) != 1 {
		t.Fatal("quiz approval failure changed/replayed question work")
	}
	followup, err := f.service.BeginFollowup(context.Background(), f.pr.ID, answer.ThreadID, question.FollowupRequest{RevisionID: answer.RevisionID, Question: "Continue on the unaffected original service."})
	if err != nil {
		t.Fatal(err)
	}
	finishNativeTurn(t, f, followup)
}

func TestQuizActiveWriterCloseDrainsWithoutSavingAndRetainsHonestStatus(t *testing.T) {
	f := newQuestionFixture(t, "quiz-wait")
	admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
	if err != nil {
		t.Fatal(err)
	}
	awaitQuizPrompts(t, f, 1)
	runtime := f.owner.quizzes
	if err := f.owner.close(); err != nil {
		t.Fatal(err)
	}
	final := waitQuiz(t, runtime, admitted)
	if final.State == quiz.Completed || final.State != quiz.Unknown && final.State != quiz.Cancelled {
		t.Fatalf("shutdown fabricated successful generation: %+v", final)
	}
	if final.State == quiz.Unknown && !final.ExecutionUnknown {
		t.Fatal("shutdown uncertainty was discarded")
	}
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if len(entry.Args) > 0 {
			if _, err := os.Stat(entry.Directory); !os.IsNotExist(err) {
				t.Fatal("quiz left owned native startup state")
			}
		}
	}
	if quizPromptCount(t, f) != 1 {
		t.Fatal("shutdown retried generation")
	}
	if _, err := os.Lstat(filepath.Join(f.paths.Data, "reviews", f.pr.ID, "quiz.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("shutdown saved/retried an unfinished quiz")
	}
}

func TestQuizRevalidatesSourceAfterNativeCreationAndDrainsOnWriterClose(t *testing.T) {
	for _, action := range []string{"change source", "close writer", "cancel creation"} {
		t.Run(action, func(t *testing.T) {
			f := newQuestionFixture(t, "create-gated")
			admitted, err := f.service.GenerateQuiz(context.Background(), f.pr.ID, f.input.Revision, 0)
			if err != nil {
				t.Fatal(err)
			}
			runtime := f.owner.quizzes
			deadline := time.NewTimer(8 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(10 * time.Millisecond)
			defer tick.Stop()
		waiting:
			for {
				if _, err := os.Stat(f.control + ".created"); err == nil {
					break waiting
				}
				select {
				case <-deadline.C:
					t.Fatal("synthetic Create did not arrive")
				case <-tick.C:
				}
			}
			if action == "close writer" {
				if err := f.owner.close(); err != nil {
					t.Fatal(err)
				}
				if final := waitQuiz(t, runtime, admitted); final.State != quiz.Unknown || !final.ExecutionUnknown {
					t.Fatal("uncertain native creation was called safely cancelled")
				}
			} else {
				if action == "change source" {
					path := filepath.Join(f.paths.Cache, "worktrees", f.pr.ID, f.head, "source.txt")
					if err := os.WriteFile(path, []byte("keep source edits\n"), 0600); err != nil {
						t.Fatal(err)
					}
				} else if _, err := runtime.Cancel(f.pr.ID, admitted.GenerationID, admitted.RevisionID); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(f.control, []byte("release-create"), 0600); err != nil {
					t.Fatal(err)
				}
				final := waitQuiz(t, runtime, admitted)
				if action == "change source" && (final.State != quiz.Failed || final.FailureCode != "source-unavailable") || action == "cancel creation" && final.State != quiz.Cancelled {
					t.Fatalf("wrong pre-dispatch result: %+v", final)
				}
			}
			if quizPromptCount(t, f) != 0 {
				t.Fatal("pre-dispatch cancellation/source failure sent a native prompt")
			}
		})
	}
}

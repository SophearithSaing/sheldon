package app

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/httpapi"
	"github.com/SophearithSaing/sheldon/internal/question"
)

// historyGET reads actual protected history with fixture credentials only in headers.
func historyGET(t *testing.T, s *httpapi.Server, path string) *http.Response {
	t.Helper()
	r, err := http.NewRequest("GET", s.URL()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+s.APIToken())
	response, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	if response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("history allowed caching")
	}
	return response
}

func TestSavedHistoryHTTPClassifiesWithoutSourceOrNativeWork(t *testing.T) {
	paths := testPaths(t)
	owner := ownedPRInstance(t, paths.Data)
	saved := questionTranscriptFixture(t, owner, paths.Cache)
	if err := owner.SaveQuestions(saved, 0); err != nil {
		t.Fatal(err)
	}
	owner.close()
	owner = ownedPRInstance(t, paths.Data)
	t.Setenv("PATH", "")
	service := &sourceRegistry{instance: owner, paths: paths}
	s := questionHTTP(t, questionFixture{owner: owner, service: service})
	base := "/api/reviews/" + saved.PRID + "/threads"
	r, err := http.NewRequest("HEAD", s.URL()+base, nil)
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Authorization", "Bearer "+s.APIToken())
	head, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	head.Body.Close()
	if head.StatusCode != 405 || owner.questionsLoaded[saved.PRID] {
		t.Fatal("HEAD loaded or reclassified history")
	}
	var list question.ThreadList
	response := historyGET(t, s, base)
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&list) != nil || list.Version != "2" || len(list.Threads) != 1 || list.Threads[0].State != question.Interrupted {
		t.Fatal("list did not classify old unfinished history")
	}
	var detail question.ThreadDetail
	response = historyGET(t, s, base+"/"+saved.Threads[0].ID)
	data, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || json.Unmarshal(data, &detail) != nil || detail.Answers[0].Text != "Partial fixture answer" || !detail.Answers[0].ExecutionUnknown || detail.Session.State != "unverified" || detail.Persistence != "saved" {
		t.Fatal("cold detail lost saved data or invented native evidence")
	}
	for _, secret := range []string{saved.Bindings[0].Native.SessionID, paths.Cache, paths.Data, `"native"`, `"directory"`} {
		if bytes.Contains(data, []byte(secret)) {
			t.Fatal("private storage/native information leaked")
		}
	}
	if owner.questions != nil || owner.cache != nil {
		t.Fatal("saved reads initialized runtime/cache")
	}
	if _, err := os.Stat(paths.Cache); !os.IsNotExist(err) {
		t.Fatal("history recreated source cache")
	}
	if response := historyGET(t, s, base+"/"+strings.Repeat("9", 64)); response.StatusCode != 404 {
		t.Fatal("missing thread was not 404")
	}
	other, err := owner.RegisterPR("https://github.com/owner/repo/pull/2")
	if err != nil {
		t.Fatal(err)
	}
	if response := historyGET(t, s, "/api/reviews/"+other.ID+"/threads/"+detail.ID); response.StatusCode != 404 {
		t.Fatal("cross-PR thread was readable")
	}
	response = historyGET(t, s, "/api/reviews/"+other.ID+"/threads")
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&list) != nil || list.Version != "0" || len(list.Threads) != 0 {
		t.Fatal("missing saved file was not empty")
	}
	path := filepath.Join(paths.Data, "reviews", saved.PRID, "threads.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	historyGET(t, s, base)
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("refresh rewrote classified history")
	}
	broken := []byte("{corrupt history PRIVATE DIAGNOSTIC")
	if err := os.WriteFile(path, broken, 0600); err != nil {
		t.Fatal(err)
	}
	response = historyGET(t, s, base)
	data, _ = io.ReadAll(response.Body)
	if response.StatusCode != 500 || bytes.Contains(data, []byte("PRIVATE")) {
		t.Fatal("corrupt storage returned healthy history or raw diagnostics")
	}
	after, _ = os.ReadFile(path)
	if !bytes.Equal(broken, after) {
		t.Fatal("corrupt storage was overwritten")
	}
}

func TestSavedHistoryReadsRetainDiskBytesAndReportUnsavedRuntime(t *testing.T) {
	f := newQuestionFixture(t, "answer-wait")
	s := questionHTTP(t, f)
	a := askHTTP(t, s, f.pr.ID, f.input)
	observeHTTP(t, s, a, true)
	var detail question.ThreadDetail
	response := historyGET(t, s, "/api/reviews/"+a.PRID+"/threads/"+a.ThreadID)
	if response.StatusCode != 200 || json.NewDecoder(response.Body).Decode(&detail) != nil || detail.Answers[0].State != question.Pending || detail.Answers[0].Text != "" || detail.Persistence != "saving" || detail.Session.State != "busy" {
		t.Fatal("read overlaid unsaved streaming output or claimed latest answer saved")
	}
	prompts := 0
	for _, entry := range nativeQuestionAudit(t, f.audit) {
		if filepath.Base(entry.Path) == "prompt" {
			prompts++
		}
	}
	if prompts != 1 {
		t.Fatal("history read dispatched inference")
	}
}

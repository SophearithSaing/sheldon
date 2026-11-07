package app

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/SophearithSaing/sheldon/internal/semantic"
)

func TestSemanticActualHTTPGenerationAndSourceFreeRead(t *testing.T) {
	f := newQuestionFixture(t, "semantic-valid")
	server := questionHTTP(t, f)
	base := "/api/reviews/" + f.pr.ID + "/semantic-review"
	res := historyGET(t, server, base)
	var empty semantic.ReadResult
	if res.StatusCode != 200 || json.NewDecoder(res.Body).Decode(&empty) != nil || empty.Version != "0" || f.owner.semantics != nil {
		t.Fatal("read started work")
	}
	res.Body.Close()
	res = questionPost(t, context.Background(), server, base+"/generations", map[string]any{"revision": f.input.Revision, "expectedVersion": "0"})
	var admitted semantic.Status
	if res.StatusCode != 202 || json.NewDecoder(res.Body).Decode(&admitted) != nil {
		t.Fatal("admission failed")
	}
	res.Body.Close()
	if end := waitSemantic(t, f.owner.semantics, admitted); end.State != "completed" {
		t.Fatalf("generation: %+v", end)
	}
	before := len(nativeQuestionAudit(t, f.audit))
	res = historyGET(t, server, base)
	data, _ := io.ReadAll(res.Body)
	res.Body.Close()
	var read semantic.ReadResult
	if json.Unmarshal(data, &read) != nil || read.Validate(f.pr.ID) != nil || read.Review == nil || read.Version != "1" {
		t.Fatal("invalid saved projection")
	}
	if len(nativeQuestionAudit(t, f.audit)) != before {
		t.Fatal("read dispatched native work")
	}
	for _, private := range []string{"schemaVersion", "ses_fixture", f.paths.Data, f.paths.Cache, "HIDDEN", "RAW PROVIDER"} {
		if strings.Contains(string(data), private) {
			t.Fatalf("public result exposed %s", private)
		}
	}
	if err := f.owner.close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	owner := ownedPRInstance(t, f.paths.Data)
	saved, err := owner.ReadSemantic(f.pr.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := saved.Encode()
	if len(b) == 0 {
		t.Fatal("missing saved bytes")
	}
	if _, err := os.Stat(f.paths.Cache); err != nil {
		t.Fatal(err)
	}
}

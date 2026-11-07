package semantic

import (
	"encoding/json"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// Saved retains one complete private review independently of generation progress or question storage.
type Saved struct {
	SchemaVersion int    `json:"schemaVersion"`
	PRID          string `json:"prID"`
	Version       uint64 `json:"version"`
	ReviewID      string `json:"reviewID"`
	Content
}

// EmptySaved describes a genuinely absent file, not a valid version-zero record.
func EmptySaved(prID string) Saved { return Saved{SchemaVersion: 1, PRID: prID} }

// Encode validates the entire retained graph and final encoded budget before replacement.
func (s Saved) Encode() ([]byte, error) {
	if s.SchemaVersion != 1 || s.Version == 0 || !githubpr.ValidID(s.PRID) || !githubpr.ValidID(s.ReviewID) || s.Diff.Revision.Validate(s.PRID) != nil || s.Content.Validate() != nil {
		return nil, ErrStore
	}
	b, err := json.Marshal(s)
	if err != nil || len(b)+1 > MaxContentBytes {
		return nil, ErrStore
	}
	return append(b, '\n'), nil
}

// DecodeSaved rejects corrupt or unsupported records without consulting source/native state.
func DecodeSaved(data []byte, prID string) (Saved, error) {
	var s Saved
	if len(data) > MaxContentBytes || !decode(data, &s) || s.PRID != prID {
		return Saved{}, ErrStore
	}
	if _, err := s.Encode(); err != nil {
		return Saved{}, err
	}
	return s, nil
}

// String keeps private source and generated explanations out of diagnostic formatting.
func (s Saved) String() string { return "saved semantic review (private details withheld)" }

// GoString withholds source in debug formatting.
func (s Saved) GoString() string { return s.String() }

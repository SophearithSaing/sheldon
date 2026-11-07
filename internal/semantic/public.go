package semantic

import (
	"reflect"
	"strconv"

	"github.com/SophearithSaing/sheldon/internal/githubpr"
)

// Review exposes validated original inventory and grouping, never native or host-location data.
type Review struct {
	ReviewID string `json:"reviewID"`
	Content
}

// ReadResult separates saved content/version from transient generation feedback.
type ReadResult struct {
	PRID       string  `json:"prID"`
	Version    string  `json:"version"`
	Review     *Review `json:"review"`
	Generation *Status `json:"generation"`
}

// ParseVersion accepts canonical lossless uint64 decimal versions, including absent-state zero.
func ParseVersion(value string) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != value {
		return 0, ErrOutput
	}
	return n, nil
}

// Public projects only validated saved fields and detached source content.
func (s Saved) Public() (ReadResult, error) {
	r := ReadResult{PRID: s.PRID, Version: strconv.FormatUint(s.Version, 10)}
	if s.Version == 0 {
		if !githubpr.ValidID(s.PRID) || !reflect.DeepEqual(s, EmptySaved(s.PRID)) {
			return ReadResult{}, ErrStore
		}
		return r, nil
	}
	b, err := s.Encode()
	if err != nil {
		return ReadResult{}, err
	}
	copy, err := DecodeSaved(b, s.PRID)
	if err != nil {
		return ReadResult{}, err
	}
	r.Review = &Review{ReviewID: s.ReviewID, Content: copy.Content}
	return r, nil
}

// Validate verifies the public saved graph and independently scoped runtime feedback.
func (r ReadResult) Validate(prID string) error {
	n, err := ParseVersion(r.Version)
	if err != nil || !githubpr.ValidID(prID) || r.PRID != prID || (r.Review == nil) != (n == 0) {
		return ErrOutput
	}
	if r.Generation != nil && r.Generation.Validate(prID) != nil {
		return ErrOutput
	}
	if r.Review != nil {
		if !githubpr.ValidID(r.Review.ReviewID) || r.Review.Diff.Revision.Validate(prID) != nil || r.Review.Content.Validate() != nil {
			return ErrOutput
		}
	}
	return nil
}

// Validate permits only bounded scoped states and fixed failure/save acknowledgements.
func (s Status) Validate(prID string) error {
	n, err := ParseVersion(s.Sequence)
	if err != nil || n == 0 || !githubpr.ValidID(prID) || s.PRID != prID || !githubpr.ValidID(s.GenerationID) || !githubpr.ValidID(s.ReviewID) || s.GenerationID == s.ReviewID || !githubpr.ValidID(s.RevisionID) || s.ExecutionUnknown != (s.State == "unknown") {
		return ErrOutput
	}
	if s.State == "completed" {
		n, err := ParseVersion(s.SavedVersion)
		if err != nil || n == 0 || s.CancelRequested || s.FailureCode != "" {
			return ErrOutput
		}
	} else if s.SavedVersion != "" {
		return ErrOutput
	}
	switch s.State {
	case "pending", "generating", "validating", "saving", "completed", "cancelled":
		if s.FailureCode != "" {
			return ErrOutput
		}
	case "unknown":
		if s.FailureCode != "session-uncertain" && s.FailureCode != "execution-uncertain" {
			return ErrOutput
		}
	case "failed":
		switch s.FailureCode {
		case "source-unavailable", "source-too-large", "generation-timeout", "native-unavailable", "output-too-large", "execution-failed", "invalid-output", "save-failed", "review-changed", "save-uncertain":
		default:
			return ErrOutput
		}
	default:
		return ErrOutput
	}
	return nil
}

// DecodeRequest strictly decodes one bounded request object without raw diagnostics.
func DecodeRequest(data []byte, out any) error {
	if !decode(data, out) {
		return ErrOutput
	}
	return nil
}

package question

import "encoding/json"

// admissionHeadroom reserves worst-case JSON answer escaping and status/version growth.
func (s Saved) admissionHeadroom() error {
	encoded, err := json.Marshal(s)
	if err != nil {
		return ErrCapacity
	}
	total := len(encoded) + 1 + 32
	for _, thread := range s.Threads {
		data, err := json.Marshal(thread)
		if err != nil {
			return ErrCapacity
		}
		size := len(data)
		for _, answer := range thread.Answers {
			if answer.State == Pending || answer.State == Streaming || answer.ExecutionUnknown {
				reserve := 6*maxAnswerBytes + 1024
				size += reserve
				total += reserve
			}
		}
		if size > MaxSavedThreadBytes {
			return ErrCapacity
		}
	}
	if total > MaxSavedBytes {
		return ErrCapacity
	}
	return nil
}

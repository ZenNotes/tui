package remote

import (
	"context"
	"math/rand"
	"strconv"
	"sync"
)

// AbsenceAwareReader tells "this file is absent" apart from "this request
// failed" against a server whose answer for the two might be the same.
//
// Servers from 2.20 on answer 404 for a missing file and reserve 500 for real
// failures. Older ones answered 500 for both, and a client refusing to read
// 500 as absence cannot open a single database against one. Rather than
// guess from a version, the reader asks the server what it does: it requests
// a path that cannot exist and looks at the status, once per connection.
type AbsenceAwareReader struct {
	read      func(ctx context.Context, rel string) (string, error)
	once      sync.Once
	conflates bool
}

// NewAbsenceAwareReader wraps a raw read.
func NewAbsenceAwareReader(read func(ctx context.Context, rel string) (string, error)) *AbsenceAwareReader {
	return &AbsenceAwareReader{read: read}
}

func (r *AbsenceAwareReader) serverConflatesAbsence(ctx context.Context) bool {
	r.once.Do(func() {
		probe := ".zennotes-absence-probe-" + strconv.FormatInt(rand.Int63(), 36)
		_, err := r.read(ctx, probe)
		if err == nil {
			r.conflates = false
			return
		}
		status := StatusOf(err)
		r.conflates = status >= 500
	})
	return r.conflates
}

// ReadFileTextOrNull returns nil for an absent file and an error for
// everything else.
func (r *AbsenceAwareReader) ReadFileTextOrNull(ctx context.Context, rel string) (*string, error) {
	text, err := r.read(ctx, rel)
	if err == nil {
		return &text, nil
	}
	status := StatusOf(err)
	switch {
	case status == 404:
		return nil, nil
	case status == 0:
		return nil, err
	case status < 500:
		return nil, err
	}
	if r.serverConflatesAbsence(ctx) {
		return nil, nil
	}
	return nil, err
}

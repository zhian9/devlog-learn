package extract

import "bytes"

// cappedBuffer stores at most limit bytes while accepting all writes. A limit
// of zero means unlimited.
type cappedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 && b.buf.Len()+len(p) > b.limit {
		remaining := b.limit - b.buf.Len()
		if remaining > 0 {
			_, _ = b.buf.Write(p[:remaining])
		}
		b.truncated = true
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *cappedBuffer) String() string  { return b.buf.String() }
func (b *cappedBuffer) Len() int        { return b.buf.Len() }
func (b *cappedBuffer) Truncated() bool { return b.truncated }

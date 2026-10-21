package audio

import "errors"

type Buffer struct {
	data []byte
	max  int
}

func NewBuffer(max int) *Buffer { return &Buffer{max: max} }
func (b *Buffer) Append(data []byte) error {
	if len(b.data)+len(data) > b.max {
		return errors.New("audio buffer limit reached")
	}
	b.data = append(b.data, data...)
	return nil
}
func (b *Buffer) Len() int { return len(b.data) }
func (b *Buffer) Clear()   { b.data = nil }
func (b *Buffer) Pop(size int, pad bool) []byte {
	if size <= 0 || len(b.data) == 0 || (len(b.data) < size && !pad) {
		return nil
	}
	out := make([]byte, size)
	n := copy(out, b.data)
	b.data = b.data[n:]
	if len(b.data) == 0 {
		b.data = nil
	}
	return out
}

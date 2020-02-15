package main

import "strconv"

type buffer struct {
	pos  int
	data []byte
}

func newBuffer(n int) *buffer {
	return &buffer{data: make([]byte, n)}
}

func (b *buffer) reset() {
	b.pos = 0
}

func (b *buffer) Remaining() int {
	return len(b.data) - b.pos
}

func (b *buffer) Bytes() []byte {
	return b.data[:b.pos]
}

func (b *buffer) String() string {
	return string(b.data[:b.pos])
}

func (b *buffer) writeString(s string) {
	n := copy(b.data[b.pos:], []byte(s))
	b.pos += n
}

func (b *buffer) write(data []byte) {
	n := copy(b.data[b.pos:], data)
	b.pos += n
}

func (b *buffer) writeByte(c byte) {
	b.data[b.pos] = c
	b.pos++
}

func (b *buffer) appendInt(n int64) {
	// TODO(vincent): maybe extract the relevant code from strconv to further optimize ?
	// We know that n will always be a timestamp in this case.

	// This is not pretty but right now I don't see any way of formatting a int64 with zero copy.
	//
	// To do zero copy we need to make sure AppendInt takes a slice backed by our backing array, but
	// also that starts at the _end_ of what we already wrote.
	// Since our writeXYZ methods use copy our data slice has len==cap and we can't pass that directly.

	// Take a subslice that ends where we last wrote.
	// This slice will have len < cap but still be backend by our backing array.
	tmp := b.data[:b.pos]

	// AppendInt will extend the returned tmp with the formatted integer.
	tmp = strconv.AppendInt(tmp[:b.pos], n, 10)

	// We now must write after that formatted integer so update the pos.
	b.pos = len(tmp)

	// Finally change our slice in the buffer and also reslice it up to the capacity,
	// so that len == cap now.
	b.data = tmp[:cap(tmp)]
}

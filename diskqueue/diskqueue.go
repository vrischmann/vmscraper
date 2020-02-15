// Package diskqueue implements a lightweight on-disk queue.
//
// The queue is backed by a single file and can have an arbitrary length.
//
// It only works on Linux and on filesystems that support the "punch hole" fallocate mode (see "man 2 fallocate").
// This requirement is to enable the use of efficient filesystem operations.
//
// Push writes a batch to the backing file with a single syscall.
// Although Push takes a byte slice, a batch can be written by concatenating entries with a \n:
//
//     err := queue.Push([]byte("abc\ndef\nghi\n"))
//
// Pop reads a batch from the head of the backing file.
//
// The maximum head size that can be read in a single call is 16Kib, however the number of entries
// will differ based on the size of the entries.
//
// You can also limit the number of entries you want:
//
//    batch := make(diskqueue.Batch, 128)
//    batch, err = queue.Pop(batch)
//
package diskqueue

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"sync"
	"syscall"
)

// Entry is a single entry in the queue.
type Entry []byte

// Batch is a slice of N sequential entries in the queue.
type Batch []Entry

// Q is the type of the disk queue.
type Q struct {
	mu sync.Mutex

	f     *os.File
	fstat syscall.Stat_t

	read int64

	hdr header

	scratch     []byte
	scratchSize int64
}

// New creates a new queue.
// If no file exists at the provided path it will be created.
//
// The scratch buffer is used to read a batch of data when calling Pop.
// The size of this buffer determines the maximum size of the batch.
//
// See the Pop method for more information.
func New(path string, scratch []byte) (*Q, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}

	dq := &Q{
		f:           f,
		scratch:     scratch,
		scratchSize: int64(len(scratch)),
	}

	// If the file already existed, read the existing header.

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if fi.Size() > 0 {
		if err := dq.readHeaderLocked(); err != nil {
			return nil, err
		}
	} else {
		if err := dq.writeHeaderLocked(); err != nil {
			return nil, err
		}
	}

	// Now seek to the end so that append works.

	_, err = f.Seek(0, os.SEEK_END)
	if err != nil {
		return nil, err
	}

	return dq, nil
}

func (q *Q) Close() error {
	if err := q.f.Close(); err != nil {
		return err
	}
	return nil
}

// header is the on-disk header of the diskqueue file.
// It's always present and it is located at the very beginning.
type header struct {
	// Zeroed is the number of zeroed bytes in the head of the file, after the header.
	Zeroed int64
}

// headerSize is the on-disk size of the header.
const headerSize = 32

// encode encodes a header into a buffer.
// The buffer must have a length of at least `headerSize` otherwise
// this function will panic.
func (m header) encode(buf []byte) []byte {
	buf = buf[:headerSize]

	binary.BigEndian.PutUint64(buf[:], uint64(m.Zeroed))

	return buf[:]
}

// deocde decodes a header from a buffer.
// The buffer must have a length of at least `headerSize`.
func (m *header) decode(buf []byte) error {
	if len(buf) != headerSize {
		return fmt.Errorf("invalid header size %d, expected %d", len(buf), headerSize)
	}

	m.Zeroed = int64(binary.BigEndian.Uint64(buf[:8]))

	return nil
}

// readHeaderLocked reads the header from disk and decodes it into
// the header structure inside q.
func (q *Q) readHeaderLocked() error {
	var hdrBuf [headerSize]byte
	if _, err := q.f.ReadAt(hdrBuf[:], 0); err != nil {
		return fmt.Errorf("unable to read header. err: %v", err)
	}
	q.hdr.decode(hdrBuf[:])

	return nil
}

// writeHeaderLocked encodes the header structure from q and writes it to disk.
func (q *Q) writeHeaderLocked() error {
	var hdrBuf [headerSize]byte
	q.hdr.encode(hdrBuf[:])

	if _, err := q.f.WriteAt(hdrBuf[:], 0); err != nil {
		return fmt.Errorf("unable to write header. err: %v", err)
	}
	return nil
}

func (q *Q) usableFileSize() (int64, error) {
	err := syscall.Fstat(int(q.f.Fd()), &q.fstat)
	if err != nil {
		return 0, err
	}

	// Compute the actual, usable file size.
	fileSize := q.fstat.Size

	// Remove the fixed header
	fileSize -= headerSize

	// Remove the holes
	fileSize -= q.hdr.Zeroed

	return fileSize, nil
}

func (q *Q) readHeadLocked() ([]byte, error) {
	if err := q.readHeaderLocked(); err != nil {
		return nil, err
	}

	usableSize, err := q.usableFileSize()
	if err != nil {
		return nil, err
	}

	// compute the size of the head we can read.
	var size int64
	switch {
	case q.read+q.scratchSize > usableSize:
		// we can't read the full head size, read what we can
		size = usableSize - q.read
	default:
		size = q.scratchSize
	}

	// We can't read straight from the start of the file because:
	// * there's a header
	// * there might be a hole
	offset := headerSize + q.hdr.Zeroed

	head := q.scratch[:size]

	// Offset by what we already read here too.
	n, err := q.f.ReadAt(head, offset+q.read)
	if err != nil {
		return nil, fmt.Errorf("unable to read head from (%d+%d), size %d", offset, q.read, size)
	}

	return head[:n], nil
}

// Push writes data to disk.
//
// To save time in syscalls it's recommended to batch multiple entry writes into one Push call.
// This works because an entry always ends with a \n, so it's easy to pack multiple entries into one
// single data buffer.
func (q *Q) Push(data []byte) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if data[len(data)-1] != '\n' {
		panic(fmt.Errorf("data doesn't have a final linefeed"))
	}

	_, err := q.f.Write(data)
	return err
}

// Pop reads the head of the file and decodes it into a batch of entries.
//
// The size of the batch returned ultimately depends on two things:
// * the size of the scratch buffer provided to New
// * the size of the batch provided to Pop
//
// Pop uses the provided batch as storage and does no allocation, so that is hard upper bound.
//
// But entries can have arbitrary length and Pop reads as much data as can be put in the scratch buffer,
// which means the bigger the entry, the less can be fit in a batch for the same scratch buffer.
//
// This method doesn't touch the on-disk data, to confirm that data should be removed
// after a call to Pop you need to call Commit.
//
// TODO(vincent): rethink the API ?
func (q *Q) Pop(batch Batch) (Batch, error) {
	// sanity checks
	if len(batch) <= 0 {
		panic(fmt.Errorf("invalid batch size"))
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	head, err := q.readHeadLocked()
	if err != nil {
		return nil, fmt.Errorf("unable to read head from file. err: %v", err)
	}

	// current position to write to in the batch
	var i int

	// as long as the view has data in it and
	// as long as the batch is incomplete, continue to find entries
	for len(head) > 0 && i < len(batch) {
		// find the end of the entry

		pos := bytes.IndexByte(head, '\n')
		if pos == -1 {
			// this indicates that we couldn't find a full entry
			// and failed to complete a batch
			break
		}

		// we know the start of the view is always the start of an entry
		// and we just got the end position of the entry.
		entry := head[:pos]

		// populate the batch
		batch[i] = entry
		i++

		// shift the view so the next iteration starts at the beginning of the next entry.
		head = head[pos+1:]

		q.read += int64(len(entry)) + 1 // +1 for the \n
	}

	return batch[:i], nil
}

// Commit removes the data already read with Pop.
// Returns an error if any. Panics if there's no data to commit.
//
// This operation uses the "punch hole" filesystem operation for efficiency.
// "Hole punching" deallocates the space used in the provided range by removing whole filesystem blocks if possible
// and by zeroing the rest.
//
// TODO(vincent): rename ?
func (q *Q) Commit() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.read <= 0 {
		return nil
	}

	err := q.punchHole(q.hdr.Zeroed, q.read)
	if err != nil {
		return err
	}

	// rewrite header with zeroed bytes
	q.hdr.Zeroed += q.read
	if err := q.writeHeaderLocked(); err != nil {
		return err
	}

	q.read = 0

	return nil
}

// Truncate removes all data from the file except the header.
// All data in the file must have bean read and committed, otherwise this will panic.
//
// Although Commit does remove data and the disk space is available again, the file size actually stays the same.
// This is a property of the "hole punching".
//
// Once all data has been read and committed, we know everything was deallocated and therefore we can truncate the file
// which will update its size with the filesystem.
//
// Eeturns an error if any.
func (q *Q) Truncate() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	usableSize, err := q.usableFileSize()
	if err != nil {
		return err
	}
	if usableSize > 0 {
		log.Printf("cannot truncate if there's still data")
		return nil
	}

	if err := q.f.Truncate(headerSize); err != nil {
		return err
	}

	// rewrite header with no zeroed bytes
	q.hdr.Zeroed = 0
	if err := q.writeHeaderLocked(); err != nil {
		return err
	}
	q.read = 0

	// seek to the end again since the offsets changed

	_, err = q.f.Seek(0, os.SEEK_END)
	if err != nil {
		return err
	}

	return nil
}

func (q *Q) punchHole(offset int64, size int64) error {
	// This is FALLOC_FL_PUNCH_HOLE
	// cf https://elixir.bootlin.com/linux/v5.4.13/source/include/uapi/linux/falloc.h#L6
	const (
		keepSize  = 0x01
		punchHole = 0x02
	)

	offset += headerSize

	err := syscall.Fallocate(int(q.f.Fd()), keepSize|punchHole, offset, size)
	if err != nil {
		return fmt.Errorf("unable to punch hole. err: %v", err)
	}
	return nil
}

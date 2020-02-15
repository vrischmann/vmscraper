package diskqueue

import (
	"bytes"
	"fmt"
	"os"
	"testing"

	"go.rischmann.fr/vmscraper/internal/testutils"
)

const (
	testBatchSize = 32

	kb = 1024
)

func TestDiskQueue(t *testing.T) {
	filename := testutils.GetTempFilename(t)
	t.Logf("writing to %s", filename)

	scratch := make([]byte, 64*kb)
	dq, err := New(filename, scratch)
	if err != nil {
		t.Fatal(err)
	}

	// 1000 == 32*31 + 8
	const lines = 1000

	t.Run("push-then-pop", func(t *testing.T) {
		for i := 0; i < lines; i++ {
			s := fmt.Sprintf("foobar%d\n", i)
			dq.Push([]byte(s))
		}

		// we expect 31 batch of 32 entries == 992 entries
		batch := make(Batch, testBatchSize)
		for i := 0; i < lines/testBatchSize; i++ {
			batch, err = dq.Pop(batch)
			if err != nil {
				t.Fatal(err)
			}

			if exp, got := testBatchSize, len(batch); exp != got {
				t.Fatalf("expected batch of size %d, got %d", exp, got)
			}

			for j, e := range batch {
				pos := i*testBatchSize + j
				exp := fmt.Sprintf("foobar%d", pos)

				if exp, got := exp, string(e); exp != got {
					t.Fatalf("expected entry %q, got %q", exp, got)
				}
			}

			if i%3 == 0 {
				if err := dq.Commit(); err != nil {
					t.Fatal(err)
				}
			}
		}

		// we expect 8 entries
		remaining := lines % testBatchSize

		batch, err = dq.Pop(batch)
		if err != nil {
			t.Fatal(err)
		}

		if exp, got := remaining, len(batch); exp != got {
			t.Fatalf("expected batch of size %d, got %d", exp, got)
		}

		for j, e := range batch {
			pos := 992 + j
			exp := fmt.Sprintf("foobar%d", pos)

			if exp, got := exp, string(e); exp != got {
				t.Fatalf("expected entry %q, got %q", exp, got)
			}
		}

		if err := dq.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := dq.Truncate(); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("push-pop", func(t *testing.T) {
		for i := 0; i < lines/testBatchSize; i++ {
			for i := 0; i < testBatchSize; i++ {
				s := fmt.Sprintf("foobar%d\n", i)
				dq.Push([]byte(s))
			}

			batch := make(Batch, testBatchSize)

			batch, err = dq.Pop(batch)
			if err != nil {
				t.Fatal(err)
			}

			if exp, got := testBatchSize, len(batch); exp != got {
				t.Fatalf("expected batch of size %d, got %d", exp, got)
			}

			for i, e := range batch {
				exp := fmt.Sprintf("foobar%d", i)

				if exp, got := exp, string(e); exp != got {
					t.Fatalf("expected entry %q, got %q", exp, got)
				}
			}

			if err := dq.Commit(); err != nil {
				t.Fatal(err)
			}
		}

		if err := dq.Truncate(); err != nil {
			t.Fatal(err)
		}
	})

	if err := dq.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestDiskQueuePopToEmpty(t *testing.T) {
	filename := testutils.GetTempFilename(t)
	t.Logf("writing to %s", filename)

	scratch := make([]byte, 64*kb)
	dq, err := New(filename, scratch)
	if err != nil {
		t.Fatal(err)
	}

	const lines = 1000

	for _, batchSize := range []int{1, 8, 16, 32, 50, 64, 128} {
		t.Run(fmt.Sprintf("size-%d", batchSize), func(t *testing.T) {
			//

			for i := 0; i < lines; i++ {
				s := fmt.Sprintf("foobar%d\n", i)
				dq.Push([]byte(s))
			}

			if batchSize >= 8 {
				return
			}

			var count int

			batch := make(Batch, batchSize)
			for len(batch) > 0 {
				batch, err = dq.Pop(batch)
				if err != nil {
					t.Fatal(err)
				}

				for _, e := range batch {
					exp := fmt.Sprintf("foobar%d", count)
					if exp, got := exp, string(e); exp != got {
						t.Fatalf("expected entry %q, got %q", exp, got)
					}
					count++
				}
			}

			if exp, got := lines, count; exp != got {
				t.Fatalf("expected %d but got %d entries", exp, got)
			}

			if err := dq.Commit(); err != nil {
				t.Fatal(err)
			}
			if err := dq.Truncate(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDiskQueueReadExisting(t *testing.T) {
	filename := testutils.GetTempFilename(t)
	t.Logf("writing to %s", filename)

	scratch := make([]byte, 64*kb)

	// Initial setup
	const lines = 100000
	{
		dq, err := New(filename, scratch)
		if err != nil {
			t.Fatal(err)
		}

		for i := 0; i < lines; i++ {
			s := fmt.Sprintf("foobar%d\n", i)
			err := dq.Push([]byte(s))
			if err != nil {
				t.Fatal(err)
			}
		}

		batch := make(Batch, 1024)
		for {
			batch, err = dq.Pop(batch)
			if err != nil {
				t.Fatal(err)
			}
			if len(batch) <= 0 {
				break
			}

			if err := dq.Commit(); err != nil {
				t.Fatal(err)
			}
		}

		if err := dq.Close(); err != nil {
			t.Fatal(err)
		}
	}

	// Create a new queue from the same file

	dq, err := New(filename, scratch)
	if err != nil {
		t.Fatal(err)
	}

	for i := lines; i < lines+20; i++ {
		s := fmt.Sprintf("foobar%d\n", i)
		err := dq.Push([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
	}

	batch := make(Batch, 64)
	for {
		batch, err = dq.Pop(batch)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch) <= 0 {
			break
		}

		if exp, got := 20, len(batch); exp != got {
			t.Fatalf("expected batch of size %d, got %d", exp, got)
		}

		for i, entry := range batch {
			s := fmt.Sprintf("foobar%d", lines+i)
			if exp, got := s, string(entry); exp != got {
				t.Fatalf("expected entry %q, got %q", exp, got)
			}
		}

		if err := dq.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	if err := dq.Truncate(); err != nil {
		t.Fatal(err)
	}
	if err := dq.Close(); err != nil {
		t.Fatal(err)
	}

	// Validate the file

	fi, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}

	if exp, got := int64(32), fi.Size(); exp != got {
		t.Fatalf("expected file size to be %d, got %d", exp, got)
	}
}

func BenchmarkDiskQueue(b *testing.B) {
	filename := testutils.GetTempFilename(b)

	b.Logf("bench writing to %s", filename)

	scratch := make([]byte, 64*kb)

	dq, err := New(filename, scratch)
	if err != nil {
		b.Fatal(err)
	}

	const size = 200
	var payloadBuf bytes.Buffer
	for j := 0; j < size; j++ {
		payloadBuf.Write([]byte("foobarbaz\n"))
	}

	//

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if err := dq.Push(payloadBuf.Bytes()); err != nil {
			b.Fatal(err)
		}

		batch := make(Batch, testBatchSize)
		for len(batch) > 0 {
			batch, err = dq.Pop(batch)
			if err != nil {
				b.Fatal(err)
			}
		}

		if err := dq.Commit(); err != nil {
			b.Fatal(err)
		}
	}

	if err := dq.Close(); err != nil {
		b.Fatal(err)
	}
}

func ExampleNew() {
	scratch := make([]byte, 64*kb)

	queue, _ := New("/tmp/mydiskqueue", scratch)
	queue.Push([]byte("data1\ndata2\n"))

	batch := make(Batch, 10)
	batch, _ = queue.Pop(batch)

	fmt.Println(len(batch))
	fmt.Println(string(batch[0]))
	fmt.Println(string(batch[1]))

	queue.Commit()
	queue.Truncate()

	// Output:
	// 2
	// data1
	// data2
}

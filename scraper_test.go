package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.rischmann.fr/vmscraper/diskqueue"
	"go.rischmann.fr/vmscraper/internal/testutils"
)

func TestScraper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, "testdata/scraper_testdata.txt")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	queue, err := diskqueue.New(testutils.GetTempFilename(t), make([]byte, 1*mb))
	if err != nil {
		t.Fatal(err)
	}

	w := newScraper(srv.URL, 100*time.Millisecond, make([]byte, 1*mb), newBuffer(1*mb), queue)
	if err := w.run(ctx); err != context.DeadlineExceeded {
		t.Fatal(err)
	}

	//

	var total int

	batch := make(diskqueue.Batch, 512)

	for len(batch) > 0 {
		batch, err = queue.Pop(batch)
		if err != nil {
			t.Fatal(err)
		}
		total += len(batch)
	}

	// there's 327 metrics in the testdata file.
	const exp = 327 * 3
	if exp != total {
		t.Fatalf("expected %d metrics in queue, got %d", exp, total)
	}
}

package main

import (
	"context"
	"encoding/json"
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

	w := newScraper(srv.URL, "myname", 100*time.Millisecond, make([]byte, 1*mb), newBuffer(1*mb), queue)
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

		// check that the target label is always there
		for _, entry := range batch {
			var obj struct {
				Metric map[string]string
			}

			err := json.Unmarshal(entry, &obj)
			if err != nil {
				t.Fatal(err)
			}

			v, ok := obj.Metric["target"]
			if !ok {
				t.Fatalf("label 'target' is not present in metric %+v", obj)
			}
			if exp, got := "myname", v; exp != got {
				t.Fatalf("expected label 'target' to be %q, got %q", exp, got)
			}
		}
	}

	// there's 327 metrics in the testdata file.
	const exp = 327 * 3
	if exp != total {
		t.Fatalf("expected %d metrics in queue, got %d", exp, total)
	}
}

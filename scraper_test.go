package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

	target := scrapeTarget{
		Endpoint:       srv.URL,
		JobName:        "myname",
		Labels:         map[string]string{"foo": "bar", "bar": "baz"},
		ScrapeInterval: 100 * time.Millisecond,
	}
	targetURL, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	w := newScraper(target, newBuffer(64*kb), queue)
	if err := w.run(ctx); err != context.DeadlineExceeded {
		t.Fatal(err)
	}

	expLabels := []struct {
		Key   string
		Value string
	}{
		{"job", "myname"},
		{"instance", targetURL.Hostname()},
		{"foo", "bar"},
		{"bar", "baz"},
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

		// check that the extra labels are always there
		for _, entry := range batch {
			var obj struct {
				Metric map[string]string
			}

			err := json.Unmarshal(entry, &obj)
			if err != nil {
				t.Fatal(err)
			}

			for _, expl := range expLabels {
				v, ok := obj.Metric[expl.Key]
				if !ok {
					t.Fatalf("label %s is not present in metric %+v", expl.Key, obj)
				}
				if exp, got := expl.Value, v; exp != got {
					t.Fatalf("expected label %s to be %q, got %q", expl.Key, exp, got)
				}
			}

		}
	}

	// there's 327 metrics in the testdata file.
	const exp = 327 * 3
	if exp != total {
		t.Fatalf("expected %d metrics in queue, got %d", exp, total)
	}
}

func BenchmarkScrape(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.ServeFile(w, req, "testdata/scraper_testdata.txt")
	}))
	defer srv.Close()

	target := scrapeTarget{
		Endpoint: srv.URL,
		JobName:  "myname",
	}

	s := newScraper(target, newBuffer(64*kb), nil)

	for i := 0; i < b.N; i++ {
		tmp, _, _ := s.scrape(s.scrapeBuffer)
		s.scrapeBuffer = tmp
	}
}

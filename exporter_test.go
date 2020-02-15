package main

import (
	"bufio"
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/valyala/fasthttp"

	"go.rischmann.fr/vmscraper/diskqueue"
)

func BenchmarkExportBatch(b *testing.B) {
	batch := make(diskqueue.Batch, 8)
	for i := 0; i < len(batch); i++ {
		batch[i] = bytes.Repeat([]byte("foobar"), 40)
	}

	var e exporter

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = e.exportBatch(batch)
	}
}

func TestExportBatch(t *testing.T) {
	const (
		batchSize = 8
	)

	var (
		entry = bytes.Repeat([]byte("foobar"), 40)
	)

	//

	batch := make(diskqueue.Batch, batchSize)
	for i := 0; i < len(batch); i++ {
		batch[i] = entry
	}

	httpClient = &fasthttp.Client{
		ReadTimeout:  1 * time.Second,
		WriteTimeout: 1 * time.Second,
	}

	//

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		payload, err := ioutil.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		defer req.Body.Close()

		// 40*6 is the size of one entry
		// 1 is for the linefeed
		// 8 is the size of the batch
		expSize := batchSize * (len(entry) + 1)
		if exp, got := expSize, len(payload); exp != got {
			t.Fatalf("expected payload of size %d, got %d", exp, got)
		}

		scanner := bufio.NewScanner(bytes.NewReader(payload))
		var count int
		for scanner.Scan() {
			data := scanner.Bytes()

			if exp, got := entry, data; !bytes.Equal(exp, got) {
				t.Fatalf("expected entry %q, got %q", string(exp), string(got))
			}

			count++
		}

		if exp, got := 8, count; exp != got {
			t.Fatalf("expected %d entries, got %d", exp, got)
		}

		//

		w.WriteHeader(204)
		w.Write([]byte("No Content"))
	}))
	defer server.Close()

	//

	e := exporter{endpoint: server.URL}

	err := e.exportBatch(batch)
	if err != nil {
		t.Fatal(err)
	}
}

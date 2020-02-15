package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/valyala/fasthttp"

	"go.rischmann.fr/vmscraper/diskqueue"
)

type exporter struct {
	endpoint  string
	interval  time.Duration
	batchSize int

	queue *diskqueue.Q
}

func newExporter(endpoint string, interval time.Duration, batchSize int, queue *diskqueue.Q) *exporter {
	return &exporter{
		endpoint:  endpoint,
		interval:  interval,
		batchSize: batchSize,
		queue:     queue,
	}
}

func (e *exporter) run(ctx context.Context) error {
	timer := time.NewTicker(e.interval)

	batch := make(diskqueue.Batch, e.batchSize)

loop:
	for {
		select {
		case <-ctx.Done():
			break loop

		case <-timer.C:
			batch = batch[:e.batchSize]

			batch, err := e.queue.Pop(batch)
			if err != nil {
				return err
			}

			err = e.exportBatch(batch)
			if err != nil {
				return err
			}

			err = e.queue.Commit()
			if err != nil {
				return err
			}
		}
	}

	return nil
}

var lf = []byte("\n")

// exportAll exports all data from the queue, then truncates the queue.
//
// This is only run at start before the exporter is started.
func (e *exporter) exportAll(ctx context.Context) error {
	batch := make(diskqueue.Batch, e.batchSize)

	start := time.Now()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop

		default:
			batch = batch[:e.batchSize]

			batch, err := e.queue.Pop(batch)
			if err != nil {
				return err
			}
			if len(batch) <= 0 {
				break loop
			}

			err = e.exportBatch(batch)
			if err != nil {
				return err
			}

			err = e.queue.Commit()
			if err != nil {
				return err
			}
		}
	}

	if err := e.queue.Truncate(); err != nil {
		return err
	}

	log.Printf("exported all data from the queue in %s", time.Since(start))

	return nil
}

func (e *exporter) exportBatch(batch diskqueue.Batch) error {
	if len(batch) <= 0 {
		return nil
	}

	//

	start := time.Now()

	req := fasthttp.AcquireRequest()
	resp := fasthttp.AcquireResponse()
	defer func() {
		fasthttp.ReleaseResponse(resp)
		fasthttp.ReleaseRequest(req)
	}()

	//

	req.SetRequestURI(e.endpoint)

	for _, entry := range batch {
		req.AppendBody(entry)
		req.AppendBody(lf)
	}

	err := httpClient.Do(req, resp)
	if err != nil {
		return err
	}

	if resp.StatusCode() != fasthttp.StatusNoContent {
		s := string(resp.Body())
		return fmt.Errorf("unable to export data. err: %v", s)
	}

	log.Printf("exported batch of %d entries in %s", len(batch), time.Since(start))

	return nil
}

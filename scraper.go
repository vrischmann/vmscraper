package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"go.rischmann.fr/vmscraper/diskqueue"
)

type scraper struct {
	target scrapeTarget

	scrapeBuffer []byte
	outputBuffer *buffer

	queue *diskqueue.Q
}

func newScraper(target scrapeTarget, outputBuffer *buffer, queue *diskqueue.Q) *scraper {
	return &scraper{
		target:       target,
		outputBuffer: outputBuffer,
		queue:        queue,
	}
}

func (s *scraper) run(ctx context.Context) error {
	ticker := time.NewTicker(s.target.ScrapeInterval)

	var (
		parser  promParser
		metrics = make(promMetrics, 512)
	)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			// reset the buffers and slices

			buffer := s.scrapeBuffer[:0]
			s.outputBuffer.reset()

			metrics = metrics[:0]

			// scrape
			var (
				scrapeTs int64
				err      error
			)

			scrapeStart := time.Now()

			buffer, scrapeTs, err = s.scrape(buffer)
			if err != nil {
				return err
			}

			scrapeTime := time.Since(scrapeStart)
			parseStart := time.Now()

			// parse the metrics
			metrics, err = parser.parse(metrics, buffer)
			if err != nil {
				return err
			}

			log.Printf("scraped %d bytes in %s, parsed %d metrics in %s", len(buffer), scrapeTime, len(metrics), time.Since(parseStart))

			// process the metrics
			//
			// This does two things:
			// * convert to the VictoriaMetrics format
			// * dump the buffer to disk when necessary

			for i := range metrics {
				m := metrics[i]

				// add the extra labels
				m.labels = append(m.labels, promLabel{
					key:   []byte("job"),
					value: []byte(s.target.JobName),
				})
				for key, value := range s.target.Labels {
					m.labels = append(m.labels, promLabel{
						key:   []byte(key),
						value: []byte(value),
					})
				}

				// convert to VictoriaMetrics format
				convertPromMetricToVM(s.outputBuffer, &m, scrapeTs)

				// if there's no more room in the output buffer, write to the queue.
				// the limit is arbitrary, it's possible that the metric would fit in 1kb, but it doesn't really matter.
				if s.outputBuffer.Remaining() < 1*kb {
					// no more space, dump to the queue and reset the buffer

					if err := s.queue.Push(s.outputBuffer.Bytes()); err != nil {
						return err
					}
					s.outputBuffer.reset()
				}
			}

			// write the remaining metrics in the buffer to the queue.
			if err := s.queue.Push(s.outputBuffer.Bytes()); err != nil {
				return err
			}
		}
	}
}

func (s *scraper) scrape(dst []byte) ([]byte, int64, error) {
	ts := time.Now().UnixNano() / 1e6

	code, dst, err := httpClient.Get(dst, s.target.Endpoint)
	if err != nil {
		return dst, ts, fmt.Errorf("unable to get data from %s. err: %v", s.target.Endpoint, err)
	}
	if code != http.StatusOK {
		return dst, ts, fmt.Errorf("bad status code %s when scraping %s. err=: %v", http.StatusText(code), s.target.Endpoint, err)
	}

	return dst, ts, nil
}

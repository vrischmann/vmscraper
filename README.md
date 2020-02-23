# vmscraper

`vmscraper` is a replacement for the scraper included with [Prometheus](https://prometheus.io/) with a few caveats:
  * the metrics scraped can only be exported to [Victoria Metrics](https://github.com/VictoriaMetrics/VictoriaMetrics)
  * most features related to scraping from Prometheus are not included

`vmscraper` is designed to be extremely simple and with minimal impact on the host running it.

# Design

`vmscraper` takes a list of targets to scrape and regularly fetches metrics from these targets.

Metrics are written to a disk queue, one per target. This queue is regularly polled by the exporter.

The exporter writes metrics to a Victoria Metrics server using the [/api/v1/import](https://github.com/VictoriaMetrics/VictoriaMetrics#how-to-import-time-series-data) endpoint.

# Prerequisites

`vmscraper` is only tested on Linux but there's no reason it shouldn't work on other operating systems.

`vmscraper` must have write access to a directory to store metrics.

# Installation

Until a release is cut you need to build it yourself.

Building it is as simple as this:

    git clone https://git.sr.ht/~vrischmann/vmscraper
    cd vmscraper
    go build

The binary `vmscraper` is usable.

# Configuration

The configuration is a YAML file.

Here is a working example:

```yaml
default_scrape_interval: 10s

data_dir: /tmp/vmscraper_data

export_endpoint: "http://localhost:8428/api/v1/import"
export_interval: 1s
export_batch_size: 2048

scratch_buffer_size: 1048576

targets:
  - endpoint: "http://localhost:9100/metrics"
    scrape_buffer_size: 1048576
    output_buffer_size: 262144
```

Most parameters are straightforward.

One thing to be aware of is that `export_interval`, `export_batch_size` and `scratch_buffer_size` must be configured correctly so that
the exporter has a chance to catch up with the scraper.

It is entirely possible to misconfigure the exporter, in that case metrics will accumulate in the queue until `vmscraper` is restarted.

# Running

Once you have a configuration file you can run `vmscraper` like this:

    $ vmscraper scrape -config myconfig.yml

The `scrape` command never returns, it's up to you to daemonize it if you want.

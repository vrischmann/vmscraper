# Configuration

This document describes the configuration file. A configuration file is written in YAML format.

In the schema below brackets indicate that a parameter is optional, a default value may be defined too.

Values can have the following placeholders:

* `<duration>`: a duration matching the regular expression [0-9]+(ms|[smhdwy])
* `<int>`: a positive integer
* `<string>`: a string of characters
* `<url>`: a valid URL

# Root parameters

```
# The scrape interval used by default if one is not defined in the scrape target.
[ default_scrape_interval: <duration> | default = 15s ]

# The directory where the disk queues will be stored. Must be write accessible.
data_dir: <string>

# The URL of the Victoria Metrics import endpoint.
export_endpoint: <url>

# The interval at which metrics are exported to Victoria Metrics.
[ export_interval: <duration> | default = 5s ]

# The maximum size of a batch of metrics used for export
[ export_batch_size: <int> | default = 512 ]

# The size in bytes for the scratch buffer.
# This should on average be large enough to fit `export_batch_size` entries,
# where each entry is the average of your metric size.
[ scratch_buffer_size: <int> | default ) 65536 ]

# A list of scrape targets.
targets:
  [ - <scrape_target> ... ]
```

# Scrape target

```
# The URL of the scrape target.
# This should respond with Prometheus formatted metrics.
endpoint: <url>

# The name of the scrape job, used as a label in the final metrics.
# Usually this is the name of the app or service which is scraped.
job_name: <string>

# Extra labels to add to every metrics.
[ labels: <string>: <string> ... ]

# The scrape interval. If not set the default scrape interval will be used.
[ scrape_interval: <duration> | default = `default_scrape_interval` ]

# The size of the buffer used to store metrics before writing them to the disk queue.
# Increasing this will reduce the number of writes to disk but increase the memory footprint.
[ output_buffer_size: <int> | default = 64 Kib ]
```

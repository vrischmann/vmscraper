package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/peterbourgon/ff/ffcli"
	"github.com/valyala/fasthttp"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"

	"go.rischmann.fr/vmscraper/diskqueue"
)

const (
	kb = 1024
	mb = kb * 1024
)

var (
	gVersion string = "unknown"
	gCommit  string = "unknown"

	httpClient *fasthttp.Client

	globalFlags = flag.NewFlagSet("root", flag.ExitOnError)

	cpuProfile = globalFlags.String("cpuprofile", "", "Create a CPU profile")
	memProfile = globalFlags.String("memprofile", "", "Create a memory profile")

	scrapeFlags = flag.NewFlagSet("scrape", flag.ExitOnError)

	scrapeConfig  = scrapeFlags.String("config", "/etc/vmscraper/config.yml", "the configuration file")
	scrapeDataDir = scrapeFlags.String("data-dir", "", "the data directory")
)

func cancelOnSigterm() (context.Context, func()) {
	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, syscall.SIGTERM, syscall.SIGINT, syscall.SIGQUIT)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-signalCh
		cancel()
	}()

	return ctx, cancel
}

// startCPUProfile starts a CPU profile if enabled.
func startCPUProfile() func() {
	if *cpuProfile == "" {
		return func() {}
	}

	f, err := os.Create(*cpuProfile)
	if err != nil {
		log.Fatalf("could not create CPU profile. err: %v", err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		log.Fatalf("could not start CPU profile. err: %v", err)
	}

	return func() {
		pprof.StopCPUProfile()
		f.Close()
	}
}

// createMemProfile creates a memory profile if enabled.
func createMemProfile() {
	if *memProfile == "" {
		return
	}

	f, err := os.Create(*memProfile)
	if err != nil {
		log.Fatalf("could not create memory profile. err: %v", err)
	}
	defer f.Close()

	runtime.GC() // get up-to-date statistics
	if err := pprof.WriteHeapProfile(f); err != nil {
		log.Fatalf("could not write memory profile. err: %v", err)
	}
}

type scrapeTarget struct {
	Endpoint string            `yaml:"endpoint"`
	Name     string            `yaml:"name"`
	Labels   map[string]string `yaml:"labels"`

	ScrapeBufferSize int           `yaml:"scrape_buffer_size"`
	ScrapeInterval   time.Duration `yaml:"scrape_interval"`

	OutputBufferSize int `yaml:"output_buffer_size"`
}

type config struct {
	DefaultScrapeInterval time.Duration  `yaml:"default_scrape_interval"`
	Targets               []scrapeTarget `yaml:"targets"`

	DataDir string `yaml:"data_dir"`

	ExportEndpoint string        `yaml:"export_endpoint"`
	ExportInterval time.Duration `yaml:"export_interval"`

	ScratchBufferSize int `yaml:"scratch_buffer_size"`
	ExportBatchSize   int `yaml:"export_batch_size"`
}

func runScrape(args []string) error {
	ctx, cancel := cancelOnSigterm()
	defer cancel()

	//

	httpClient = &fasthttp.Client{
		ReadTimeout:  4 * time.Second,
		WriteTimeout: 4 * time.Second,
	}

	//

	configData, err := ioutil.ReadFile(*scrapeConfig)
	if err != nil {
		return err
	}

	var conf config
	if err := yaml.Unmarshal(configData, &conf); err != nil {
		return err
	}

	if *scrapeDataDir != "" {
		conf.DataDir = *scrapeDataDir
	}

	if err := os.MkdirAll(conf.DataDir, 0700); err != nil {
		return err
	}

	if conf.ExportEndpoint == "" {
		return errors.New("export endpoint can't be empty")
	}
	if conf.ExportInterval <= 0 {
		return errors.New("export interval can't be 0")
	}

	if conf.ScratchBufferSize <= 64*kb {
		log.Printf("invalid scratch buffer size %d, must be at least 64Kib", conf.ScratchBufferSize)
		conf.ScratchBufferSize = 64 * kb
	}
	if conf.ExportBatchSize <= 64 {
		log.Printf("invalid batch size %d, must be at least 64", conf.ExportBatchSize)
		conf.ExportBatchSize = 64
	}

	//

	eg, ctx := errgroup.WithContext(ctx)

	var (
		queueIndex int
		queues     = make([]*diskqueue.Q, len(conf.Targets))
	)

	for _, target := range conf.Targets {
		// Setup the configuration for the target.

		target := target

		if target.Endpoint == "" {
			log.Fatalf("target endpoint cannot be empty")
		}
		if target.Name == "" {
			log.Fatalf("target name cannot be empty")
		}
		if !strings.HasPrefix(target.Endpoint, "http://") {
			target.Endpoint = "http://" + target.Endpoint
		}

		if target.ScrapeInterval == 0 {
			target.ScrapeInterval = conf.DefaultScrapeInterval
		}

		scrapeBufferSize := target.ScrapeBufferSize
		if scrapeBufferSize <= 8*kb {
			log.Printf("target: %s, invalid scrape buffer size %d, defaulting to 8Kib", target.Endpoint, scrapeBufferSize)
			scrapeBufferSize = 8 * kb
		}
		outputBufferSize := target.OutputBufferSize
		if outputBufferSize <= 8*kb {
			log.Printf("target: %s, invalid output buffer size %d, defaulting to 8Kib", target.Endpoint, outputBufferSize)
			outputBufferSize = 8 * kb
		}

		// There's one queue per target.
		queuePath := filepath.Join(conf.DataDir, fmt.Sprintf("queue.%04d", queueIndex))

		queue, err := diskqueue.New(queuePath, make([]byte, conf.ScratchBufferSize))
		if err != nil {
			return err
		}

		queues[queueIndex] = queue
		queueIndex++

		// Start two goroutines per scraping target:
		// - the scraper
		// - the exporter

		eg.Go(func() error {
			scrapeBuffer := make([]byte, scrapeBufferSize)
			outputBuffer := newBuffer(outputBufferSize)

			sc := newScraper(target, scrapeBuffer, outputBuffer, queue)

			return sc.run(ctx)
		})

		eg.Go(func() error {
			ex := newExporter(conf.ExportEndpoint, conf.ExportInterval, conf.ExportBatchSize, queue)

			if err := ex.exportAll(ctx); err != nil {
				return err
			}

			return ex.run(ctx)
		})
	}

	if err := eg.Wait(); err != nil {
		return err
	}

	return nil
}

func runDumpQueue(args []string) error {
	defer startCPUProfile()()

	//

	queue, err := diskqueue.New(args[0], make([]byte, 1*mb))
	if err != nil {
		return err
	}

	batch := make(diskqueue.Batch, 64)
	for {
		batch, err = queue.Pop(batch)
		if err != nil {
			return err
		}
		if len(batch) <= 0 {
			break
		}

		var buf []byte

		for _, entry := range batch {
			buf = append(buf, entry...)
			buf = append(buf, '\n')
		}

		os.Stdout.Write(buf)
	}

	createMemProfile()

	return nil
}

func main() {
	scrapeCmd := &ffcli.Command{
		Name:      "scrape",
		FlagSet:   scrapeFlags,
		Usage:     "scrape [flag]",
		ShortHelp: "scrape prometheus endpoints",
		Exec: func(args []string) error {
			return runScrape(args)
		},
	}

	dumpQueueCmd := &ffcli.Command{
		Name:      "dump-queue",
		Usage:     "dump-queue <queue file>",
		ShortHelp: "dump the contents of a queue file",
		Exec: func(args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("usage: vmscraper dump-queue <file>")
			}
			return runDumpQueue(args)
		},
	}

	versionCmd := &ffcli.Command{
		Name:      "version",
		Usage:     "version",
		ShortHelp: "print the version information (necessary to report bugs)",
		Exec: func([]string) error {
			log.Printf("vmscraper version %s, commit %s", gVersion, gCommit)
			return nil
		},
	}

	rootCmd := &ffcli.Command{
		Usage:     "vmscraper <subcommand> [flag] [args...]",
		FlagSet:   globalFlags,
		ShortHelp: "Scrape prometheus targets and import timeseries to VictoriaMetrics",
		Subcommands: []*ffcli.Command{
			scrapeCmd, dumpQueueCmd,
			versionCmd,
		},
		Exec: func([]string) error {
			return flag.ErrHelp
		},
	}

	if err := rootCmd.Run(os.Args[1:]); err != nil && err != flag.ErrHelp {
		log.Fatal(err)
	}
}

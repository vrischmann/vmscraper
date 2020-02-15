package main

import (
	"bytes"
	"io/ioutil"
	"path/filepath"
	"testing"
)

func b(s string) []byte { return []byte(s) }

func labelsEq(a, b promLabels) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		l1 := a[i]
		l2 := b[i]

		if !bytes.Equal(l1.key, l2.key) {
			return false
		}
		if !bytes.Equal(l1.value, l2.value) {
			return false
		}
	}

	return true
}

func TestPromParserParse(t *testing.T) {
	testCases, err := filepath.Glob("testdata/parser_parse_*.txt")
	if err != nil {
		t.Fatalf("unable to get files")
	}

	var parser promParser

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			data, err := ioutil.ReadFile(tc)
			if err != nil {
				t.Fatalf("unable to read file %s. err: %v", tc, err)
			}

			dst, err := parser.parse(nil, data)
			if err != nil {
				t.Fatalf("unable to parse file %s. err: %v", tc, err)
			}

			if len(dst) <= 0 {
				t.Fatalf("no metric where parsed")
			}
		})
	}
}

func TestPromParserParseLine(t *testing.T) {
	testCases := []struct {
		input     string
		expName   []byte
		expLabels promLabels
		expValue  []byte
	}{
		{
			`node_filesystem_free_bytes{device="tmpfs",fstype="tmpfs",mountpoint="/run/user/0"} 1.9562496e+08`,
			b("node_filesystem_free_bytes"),
			promLabels{
				{b("device"), b("tmpfs")},
				{b("fstype"), b("tmpfs")},
				{b("mountpoint"), b("/run/user/0")},
			},
			b(`1.9562496e+08`),
		},
		{
			`node_filefd_maximum 187806`,
			b("node_filefd_maximum"),
			nil,
			b("187806"),
		},
		{
			`node_filefd_maximum{name="Vincent Rischmann"} 23000`,
			b("node_filefd_maximum"),
			promLabels{
				{b("name"), b("Vincent Rischmann")},
			},
			b("23000"),
		},
	}

	var parser promParser

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			var m promMetric

			err := parser.parseLine(&m, []byte(tc.input))
			if err != nil {
				t.Fatalf("unable to parse line. err: %v", err)
			}

			if !bytes.Equal(tc.expName, m.name) {
				t.Fatalf("expected name %s, got %s", string(tc.expName), string(m.name))
			}
			if !labelsEq(tc.expLabels, m.labels) {
				t.Fatalf("expected labels %v, got %v", promLabels(tc.expLabels), promLabels(m.labels))
			}
			if !bytes.Equal(tc.expValue, m.value) {
				t.Fatalf("expected value %s, got %s", string(tc.expValue), string(m.value))
			}
		})
	}
}

func BenchmarkParseLine(b *testing.B) {
	const input = `node_filesystem_free_bytes{device="tmpfs",fstype="tmpfs",mountpoint="/run/user/0"} 1.9562496e+08`

	var (
		parser promParser
		m      promMetric

		line = []byte(input)
	)

	for i := 0; i < b.N; i++ {
		m.reset()

		err := parser.parseLine(&m, line)
		if err != nil {
			b.Fatalf("unable to parse line. err: %v", err)
		}
	}
}

func BenchmarkParallelParseLine(b *testing.B) {
	const input = `node_filesystem_free_bytes{device="tmpfs",fstype="tmpfs",mountpoint="/run/user/0"} 1.9562496e+08`

	var (
		parser promParser

		line = []byte(input)
	)

	b.RunParallel(func(pb *testing.PB) {
		var m promMetric

		for pb.Next() {
			m.reset()

			err := parser.parseLine(&m, line)
			if err != nil {
				b.Fatalf("unable to parse line. err: %v", err)
			}
		}
	})
}

package main

import (
	"bytes"
	"strings"
)

type promLabel struct {
	key   []byte
	value []byte
}

// String formats a key/value pair in the expfmt format.
func (l *promLabel) String() string {
	return string(l.key) + `="` + string(l.value) + `"`
}

type promLabels []promLabel

func (l promLabels) String() string {
	s := make([]string, len(l))
	for i := range l {
		s[i] = l[i].String()
	}
	return strings.Join(s, ",")
}

type promMetric struct {
	name   []byte
	labels promLabels
	value  []byte
}

// reset makes this metric reusable.
func (m *promMetric) reset() {
	m.name = nil
	m.labels = m.labels[:0]
	m.value = nil
}

// String formats the metric in the expfmt format.
func (m promMetric) String() string {
	return string(m.name) + "{" + promLabels(m.labels).String() + "} " + string(m.value)
}

type promMetrics []promMetric

// reset reslices the slice with a length of 0.
// This makes it reusable while preserving the backing array if there's one.
func (m *promMetrics) reset() {
	(*m) = (*m)[:0]
}

// promParser is a purpose built parser for expfmt Prometheus metric.
// Examples of this format can be found here: https://github.com/prometheus/common/blob/master/expfmt/testdata/text
//
// It does no allocation and simply populates a promMetric struct
// with slices from the original line data.
//
// It has no states and therefore is completely thread safe.
type promParser struct {
}

func (p *promParser) parse(dst promMetrics, data []byte) (promMetrics, error) {
loop:
	for {
		pos := bytes.IndexByte(data, '\n')
		if pos == -1 {
			break loop
		}

		line := data[:pos]
		data = data[pos+1:] // +1 for the \n we don't want

		if line[0] == '#' {
			continue loop
		}

		var metric promMetric
		err := p.parseLine(&metric, line)
		if err != nil {
			return dst, err
		}

		dst = append(dst, metric)
	}

	return dst, nil
}

// parseLine parses a expfmt Prometheus metric.
//
// This function populates the metric value with references to data from the input line slice, so you must take care
// of using the metric before modifying the line slice in any way.
// Doing it this way allows the parsing to never allocate any data on the heap.
//
// If there's any problem with the parsing an error will be returned.
func (p *promParser) parseLine(dst *promMetric, line []byte) (err error) {
	labelOpenPos := bytes.IndexByte(line, '{')

	switch {
	case labelOpenPos == -1:
		// no label, this is the easy case

		spacePos := bytes.IndexByte(line, ' ')
		if spacePos == -1 {
			return mkParserError(badSyntax, "unable to find space separator, no value")
		}

		dst.name = line[:spacePos]

		line = line[spacePos+1:]

	default:
		// the name ends where the labels start.
		dst.name = line[:labelOpenPos]

		// we got the opening bracket, check the closing bracket
		labelClosePos := bytes.IndexByte(line, '}')
		if labelClosePos == -1 {
			return mkParserError(badSyntax, "no closing label bracket")
		}

		// we got the start and end positions for the labels
		// create a view of this data to make it easier to check
		// if we've consumed everything.
		view := line[labelOpenPos+1 : labelClosePos]

	loop:
		for {
			// find the key

			pos := bytes.IndexByte(view, '=')
			if pos == -1 {
				return mkParserError(badSyntax, "invalid key/value formatting")
			}

			var label promLabel
			label.key = view[:pos]

			// shift the view:
			// +1 for the = character
			// +1 for the double quote at the start of the value
			view = view[pos+2:]

			// find the value

			pos = bytes.IndexByte(view, '"')
			if pos == -1 {
				return mkParserError(badSyntax, "invalid labels formatting")
			}

			label.value = view[:pos]

			// +1 for the double quote at the end of the value
			view = view[pos+1:]

			dst.labels = append(dst.labels, label)

			// detect the end of the labels
			if len(view) == 0 {
				break loop
			}

			// not the end which means there's a , next
			view = view[1:]
		}

		line = line[labelClosePos+1:] // +1 for the closing label bracket

		spacePos := bytes.IndexByte(line, ' ')
		if spacePos == -1 {
			return mkParserError(badSyntax, "unable to find space separator, no value")
		}

		line = line[1:] // to remove the space before the value
	}

	dst.value = line

	return
}

type parserErrorReason uint

const (
	unknownReason parserErrorReason = iota // unknown
	badSyntax                              // bad syntax
)

func (r parserErrorReason) String() string {
	switch r {
	case badSyntax:
		return "bad syntax"
	default:
		return "unknown"
	}
}

// parserError is the type of the error returned by ParseLine if the parsing fails.
type parserError struct {
	reason parserErrorReason
	msg    string
}

func (e *parserError) Error() string {
	return e.reason.String() + ": " + e.msg
}

func mkParserError(reason parserErrorReason, msg string) *parserError {
	return &parserError{reason, msg}
}

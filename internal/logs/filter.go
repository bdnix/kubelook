package logs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/bdnix/kubelook/internal/apperr"
)

// maxFilterLen bounds a filter pattern.
const maxFilterLen = 256

// filterSpec is the "filter" of a logs.open op: a text, or a regular expression (RE2).
type filterSpec struct {
	Text  string `json:"text"`
	Regex string `json:"regex"`
}

// lineFilter keeps the lines that match. Go's regexp runs in linear time, so a user's pattern cannot stall the server.
type lineFilter struct {
	text string
	re   *regexp.Regexp
}

func newFilter(f *filterSpec) (*lineFilter, error) {
	if f == nil {
		return nil, nil //nolint:nilnil // no filter is not an error
	}
	switch {
	case f.Text != "" && f.Regex != "":
		return nil, fmt.Errorf("a filter is either text or a regular expression: %w", apperr.ErrInvalid)
	case f.Text == "" && f.Regex == "":
		return nil, fmt.Errorf("a filter needs text or a regular expression: %w", apperr.ErrInvalid)
	case len(f.Text) > maxFilterLen || len(f.Regex) > maxFilterLen:
		return nil, fmt.Errorf("a filter is at most %d characters long: %w", maxFilterLen, apperr.ErrInvalid)
	case f.Text != "":
		return &lineFilter{text: strings.ToLower(f.Text)}, nil
	}
	re, err := regexp.Compile(f.Regex)
	if err != nil {
		return nil, fmt.Errorf("the regular expression is not valid: %w", apperr.ErrInvalid)
	}
	return &lineFilter{re: re}, nil
}

// match reports whether the line passes. A nil filter passes everything.
func (f *lineFilter) match(text string) bool {
	switch {
	case f == nil:
		return true
	case f.re != nil:
		return f.re.MatchString(text)
	}
	return strings.Contains(strings.ToLower(text), f.text)
}

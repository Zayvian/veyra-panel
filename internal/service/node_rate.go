package service

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Names used in JSON/YAML and URI fragments can safely contain Chinese.
// Keep control characters, invisible format characters and delimiters out.
var displayNameRE = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N}\p{M} ._()（）-]*$`)
var nodeRateRE = regexp.MustCompile(`^[0-9]+(?:\.[0-9]{1,3})?$`)

func checkDisplayName(kind, name string) error {
	limit := 64
	if kind == "inbound tag" {
		// Auto-generated tags also contain the protocol and a collision suffix.
		limit = 128
	}
	if !displayNameRE.MatchString(name) || utf8.RuneCountInString(name) > limit {
		return invalid("%s must be 1-%d characters: Chinese/letters, numbers, spaces, dot, dash, underscore or parentheses", kind, limit)
	}
	return nil
}
func checkRate(rate int64) error {
	if rate < 0 || rate > 100000 {
		return invalid("流量倍率必须在 0 到 100 之间，最多三位小数")
	}
	return nil
}

// ParseNodeRate uses integer arithmetic so repeated reports never lose fractions.
func ParseNodeRate(value string) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 1000, nil
	}
	if !nodeRateRE.MatchString(value) {
		return 0, invalid("流量倍率必须在 0 到 100 之间，最多三位小数")
	}
	whole, fraction, _ := strings.Cut(value, ".")
	n, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || n > 100 {
		return 0, invalid("流量倍率不能超过 100")
	}
	f, _ := strconv.ParseInt(fraction+strings.Repeat("0", 3-len(fraction)), 10, 64)
	rate := n*1000 + f
	return rate, checkRate(rate)
}

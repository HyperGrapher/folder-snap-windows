package ignore

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

type MatchResult struct {
	Excluded     bool
	MatchingRule string
}

type rule struct {
	original  string
	negated   bool
	dirOnly   bool
	anchored  bool
	hasSlash  bool
	regex     *regexp.Regexp
	baseRegex *regexp.Regexp
}

type Matcher struct {
	rules       []rule
	hasNegation bool
	raw         []string
}

func Compile(lines []string) (*Matcher, error) {
	m := &Matcher{raw: append([]string(nil), lines...)}
	for lineNo, input := range lines {
		line := strings.TrimSpace(strings.ReplaceAll(input, `\`, "/"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := rule{original: input}
		if strings.HasPrefix(line, "!") {
			r.negated = true
			m.hasNegation = true
			line = strings.TrimPrefix(line, "!")
		}
		if line == "" {
			return nil, fmt.Errorf("ignore rule %d: empty negation", lineNo+1)
		}
		r.anchored = strings.HasPrefix(line, "/")
		line = strings.TrimPrefix(line, "/")
		r.dirOnly = strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		r.hasSlash = strings.Contains(line, "/")
		if line == "" {
			return nil, fmt.Errorf("ignore rule %d: empty pattern", lineNo+1)
		}

		var expression string
		if r.anchored || r.hasSlash {
			expression = "^" + globRegex(line)
			if r.dirOnly {
				expression += "(?:/.*)?$"
			} else {
				expression += "$"
			}
		} else {
			expression = "(?:^|/)" + globRegex(line)
			if r.dirOnly {
				expression += "(?:/.*)?$"
			} else {
				expression += "$"
			}
		}
		compiled, err := regexp.Compile("(?i)" + expression)
		if err != nil {
			return nil, fmt.Errorf("ignore rule %d: %w", lineNo+1, err)
		}
		r.regex = compiled
		m.rules = append(m.rules, r)
	}
	return m, nil
}

func globRegex(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	return b.String()
}

func (m *Matcher) Match(path string, isDirectory bool) MatchResult {
	path = strings.Trim(strings.ReplaceAll(path, `\`, "/"), "/")
	result := MatchResult{}
	for _, r := range m.rules {
		if r.dirOnly && !isDirectory && !r.regex.MatchString(path) {
			continue
		}
		if r.regex.MatchString(path) {
			result.Excluded = !r.negated
			result.MatchingRule = r.original
		}
	}
	return result
}

// CanPrune returns false whenever a later negation could require traversal.
// This deliberately favors correctness over early pruning for rule sets with
// re-inclusions.
func (m *Matcher) CanPrune(path string) bool {
	return !m.hasNegation && m.Match(path, true).Excluded
}

func (m *Matcher) Hash() string {
	sum := sha256.Sum256([]byte(strings.Join(m.raw, "\n")))
	return hex.EncodeToString(sum[:])
}

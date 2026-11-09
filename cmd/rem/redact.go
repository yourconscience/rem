package main

import (
	"regexp"
	"strings"
)

// secretPatterns mirrors lib/safety.py; change the two lists together.
// The first capture group is the part that stays after redaction.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(Bearer\s+)[A-Za-z0-9._~+/=-]{8,}`),
	regexp.MustCompile(`(?i)\b((?:api[_-]?key|secret|token|password|authorization|auth)\s*[:=]\s*)(?:"[^"]+"|'[^']+'|[^\s` + "`" + `'"\\]+)`),
	regexp.MustCompile(`(?i)((?:[?&])(?:api[_-]?key|secret|token|password|auth)=)[^&\s` + "`" + `'"\\]+`),
	regexp.MustCompile(`(?i)\b(sk-(?:ant-|proj-|svcacct-)?)[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`(?i)\b(github_pat_)[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`(?i)\b(gh[pousr]_)[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`(?i)\b(glpat-)[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`(?i)\b(xox[baprs]-)[A-Za-z0-9-]{16,}`),
}

// genericSecretPatterns are the key=value forms: prose such as "auth: oauth" also
// matches them, so the diff scan needs the value itself to look generated.
var genericSecretPatterns = map[int]bool{1: true, 2: true}

var generatedValueRe = regexp.MustCompile(`[0-9]`)

func redactText(s string) string {
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, "${1}[REDACTED]")
	}
	return s
}

// hasSecret reports whether a line still holds an unredacted credential.
func hasSecret(line string) bool {
	for i, re := range secretPatterns {
		for _, m := range re.FindAllStringSubmatch(line, -1) {
			value := strings.Trim(strings.TrimPrefix(m[0], m[1]), `"'`)
			if strings.HasPrefix(value, "[REDACTED]") {
				continue
			}
			if genericSecretPatterns[i] && (len(value) < 16 || !generatedValueRe.MatchString(value)) {
				continue
			}
			return true
		}
	}
	return false
}

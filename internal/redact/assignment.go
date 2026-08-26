package redact

import "strings"

// The vendor prefix table covers keys Kolkrabbi has heard of. Most secrets in a
// real project belong to nobody it knows: a database password, an internal
// service token, a partner's API key. Those are recognised by the shape of the
// line instead — a name that says "secret" next to a value that looks like one.
//
// The bar for redacting is deliberately high. Over-redaction corrupts the
// output the model needs to work, and a scrubber that mangles ordinary code
// teaches people to turn it off, which is worse than one that misses a case.

// secretNames are the name fragments that make a value worth hiding. "key" on
// its own is missing on purpose: primary_key, sort_key, foreign_key and
// cache_key are ordinary code, and redacting them would be constant.
var secretNames = []string{
	"SECRET", "TOKEN", "PASSWORD", "PASSWD", "CREDENTIAL",
	"APIKEY", "API_KEY", "ACCESS_KEY", "PRIVATE_KEY", "AUTH_KEY", "AUTHORIZATION",
}

// placeholders are values that exist to be replaced. Redacting them hides
// nothing and makes documentation unreadable.
//
// The list is matched as a substring, which is the policy the project already
// settled on in testdata/falsepositives.txt: a .env template full of
// sk-proj-EXAMPLE… and YOUR_KEY_HERE is the common case, and redacting it is
// noise that teaches people to distrust the scrubber. A real secret containing
// the word EXAMPLE is the rarer accident, and the cost of missing it is bounded
// by every other rule still applying.
var placeholders = []string{
	"CHANGEME", "CHANGE_ME", "PLACEHOLDER", "EXAMPLE", "TODO", "REDACTED", "YOUR",
}

const minimumSecretValue = 12

// scrubAssignments redacts the value of any line that names a secret.
func scrubAssignments(text string) string {
	if !strings.ContainsAny(text, "=:") {
		return text
	}
	lines := strings.Split(text, "\n")
	changed := false
	for i, line := range lines {
		redacted, ok := scrubAssignmentLine(line)
		if ok {
			lines[i] = redacted
			changed = true
		}
	}
	if !changed {
		return text
	}
	return strings.Join(lines, "\n")
}

func scrubAssignmentLine(line string) (string, bool) {
	separator := -1
	for i, r := range line {
		if r == '=' || r == ':' {
			separator = i
			break
		}
	}
	if separator < 0 || separator == len(line)-1 {
		return line, false
	}
	name := strings.ToUpper(cleanName(line[:separator]))
	if !namesASecret(name) {
		return line, false
	}
	value := line[separator+1:]
	trimmed, prefix, suffix := splitSurrounding(value)
	if !looksLikeASecretValue(trimmed) {
		return line, false
	}
	return line[:separator+1] + prefix + sentinel(trimmed, "secret") + suffix, true
}

// cleanName strips the decoration around a name: shell exports, quotes, commas
// and indentation all appear around the same assignment in the wild.
func cleanName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimPrefix(name, "export ")
	name = strings.TrimPrefix(name, "set ")
	return strings.Trim(name, "\"' \t-,")
}

func namesASecret(name string) bool {
	for _, fragment := range secretNames {
		if strings.Contains(name, fragment) {
			return true
		}
	}
	return false
}

// splitSurrounding separates a value from the whitespace and quoting around it
// so the line keeps its shape once the value is replaced.
func splitSurrounding(value string) (trimmed, prefix, suffix string) {
	trimmed = value
	for len(trimmed) > 0 && (trimmed[0] == ' ' || trimmed[0] == '\t' || trimmed[0] == '"' || trimmed[0] == '\'') {
		prefix += string(trimmed[0])
		trimmed = trimmed[1:]
	}
	for len(trimmed) > 0 {
		last := trimmed[len(trimmed)-1]
		if last != ' ' && last != '\t' && last != '"' && last != '\'' && last != ',' && last != ';' && last != '\r' {
			break
		}
		suffix = string(last) + suffix
		trimmed = trimmed[:len(trimmed)-1]
	}
	return trimmed, prefix, suffix
}

// looksLikeASecretValue keeps the false-positive rate low: a secret is long,
// unbroken, and is not standing in for one that will be supplied later.
func looksLikeASecretValue(value string) bool {
	if len(value) < minimumSecretValue || strings.ContainsAny(value, " \t") {
		return false
	}
	// A reference to another variable is not the secret itself.
	if strings.HasPrefix(value, "${") || strings.HasPrefix(value, "$(") ||
		strings.HasPrefix(value, "<") || strings.HasPrefix(value, "$") {
		return false
	}
	upper := strings.ToUpper(value)
	for _, placeholder := range placeholders {
		if strings.Contains(upper, placeholder) {
			return false
		}
	}
	// A long run of one character is a blank to fill in, not a secret, whether
	// it is the whole value or follows a real prefix: sk-or-v1-XXXXXXXX…
	if hasLongRun(value, 8) {
		return false
	}
	// A plain lowercase word is a name, not a secret. Source code is full of
	// `const tokenName = "access_token"`, and redacting identifiers would make
	// reading code impossible. A password of only lowercase letters is missed
	// by this, which is the price of not corrupting every file that mentions a
	// token.
	return !isPlainWord(value)
}

// scrubURLCredentials hides the password in a URL while leaving the rest — the
// scheme, user and host are what make the line worth reading at all.
func scrubURLCredentials(text string) string {
	const marker = "://"
	out := text
	searchFrom := 0
	for {
		schemeAt := strings.Index(out[searchFrom:], marker)
		if schemeAt < 0 {
			return out
		}
		start := searchFrom + schemeAt + len(marker)
		end := start
		for end < len(out) && !isURLTerminator(out[end]) {
			end++
		}
		authority := out[start:end]
		at := strings.LastIndex(authority, "@")
		colon := strings.Index(authority, ":")
		if at > 0 && colon > 0 && colon < at {
			password := authority[colon+1 : at]
			if len(password) > 0 {
				replacement := authority[:colon+1] + sentinel(password, "secret") + authority[at:]
				out = out[:start] + replacement + out[end:]
				end = start + len(replacement)
			}
		}
		searchFrom = end
		if searchFrom >= len(out) {
			return out
		}
	}
}

func isURLTerminator(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '"', '\'', '<', '>', '`', ')', ']', '}', ',', ';':
		return true
	default:
		return false
	}
}

// hasLongRun reports a run of one repeated character, which is how a template
// says "put yours here".
func hasLongRun(value string, minimum int) bool {
	run := 1
	for i := 1; i < len(value); i++ {
		if value[i] == value[i-1] {
			run++
			if run >= minimum {
				return true
			}
			continue
		}
		run = 1
	}
	return false
}

// isPlainWord reports a value made only of lowercase letters, underscores and
// hyphens: an identifier or an English word rather than a credential.
func isPlainWord(value string) bool {
	for i := 0; i < len(value); i++ {
		c := value[i]
		if (c >= 'a' && c <= 'z') || c == '_' || c == '-' || c == '.' {
			continue
		}
		return false
	}
	return true
}

package redact

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

const minimumLiteralLength = 12

type scrubPattern struct {
	prefix      string
	label       string
	minSuffix   int
	exactLength int
	alphabet    string
}

type knownLiteral struct {
	value    string
	sentinel string
}

var (
	patternGate = buildPatternGate()
	keywordGate = buildKeywordGate()
	salt        = processSalt()
	known       = struct {
		sync.RWMutex
		byFirst [256][]knownLiteral
	}{}
)

// Register adds an exact process-known credential literal to the scrubber.
// Short values are ignored because treating a common word as a secret would
// redact unrelated output throughout the process.
func Register(value string) {
	value = strings.TrimSpace(value)
	if len(value) < minimumLiteralLength {
		return
	}
	literal := knownLiteral{
		value:    value,
		sentinel: sentinel(value, labelForLiteral(value)),
	}

	known.Lock()
	defer known.Unlock()
	bucket := &known.byFirst[value[0]]
	for _, existing := range *bucket {
		if existing.value == value {
			return
		}
	}
	*bucket = append(*bucket, literal)
	sort.Slice(*bucket, func(i, j int) bool {
		return len((*bucket)[i].value) > len((*bucket)[j].value)
	})
}

// Scrub replaces process-known literals and durable credential patterns with
// stable, process-salted sentinels. It works on arbitrary text and leaves text
// without a match byte-for-byte unchanged.
func Scrub(text string) string {
	known.RLock()
	defer known.RUnlock()

	last := 0
	var output strings.Builder
	for at := 0; at < len(text); {
		start, end, replacement, ok := scrubMatch(text, at)
		if !ok {
			at++
			continue
		}
		if output.Cap() == 0 {
			output.Grow(len(text))
		}
		output.WriteString(text[last:start])
		output.WriteString(replacement)
		last = end
		at = end
	}
	if output.Cap() == 0 {
		// The prefix scan found nothing. Vendor-less secrets are recognised by
		// the shape of the line instead, and are checked on the original text
		// so the two passes cannot interfere with each other.
		return scrubURLCredentials(scrubAssignments(text))
	}
	output.WriteString(text[last:])
	return scrubURLCredentials(scrubAssignments(output.String()))
}

func scrubMatch(text string, at int) (start, end int, replacement string, ok bool) {
	for _, literal := range known.byFirst[text[at]] {
		if strings.HasPrefix(text[at:], literal.value) {
			return at, at + len(literal.value), literal.sentinel, true
		}
	}
	if text[at] == '-' {
		if end, label, ok := privateKeyAt(text, at); ok {
			return at, end, sentinel(text[at:end], label), true
		}
	}
	if (text[at] == 'b' || text[at] == 'B') && (at == 0 || !isWordByte(text[at-1])) {
		if start, end, label, ok := bearerAt(text, at); ok {
			return start, end, sentinel(text[start:end], label), true
		}
	}
	if text[at] == 'e' {
		if end, label, ok := jwtAt(text, at); ok {
			return at, end, sentinel(text[at:end], label), true
		}
	}
	if len(patternGate[text[at]]) > 0 {
		if end, label, ok := shapeAt(text, at); ok {
			return at, end, sentinel(text[at:end], label), true
		}
	}
	if len(keywordGate[text[at]]) > 0 && (at == 0 || !isWordByte(text[at-1])) {
		if start, end, label, ok := keywordValueAt(text, at); ok {
			return start, end, sentinel(text[start:end], label), true
		}
	}
	return 0, 0, "", false
}

func buildPatternGate() [256][]scrubPattern {
	var gate [256][]scrubPattern
	add := func(pattern scrubPattern) {
		if pattern.prefix == "" {
			return
		}
		if pattern.minSuffix == 0 && pattern.exactLength == 0 {
			pattern.minSuffix = 16
		}
		if pattern.alphabet == "" {
			pattern.alphabet = "key"
		}
		gate[pattern.prefix[0]] = append(gate[pattern.prefix[0]], pattern)
	}
	for _, rule := range keyShapes.Infer {
		minSuffix := rule.MinSuffix
		if rule.ScrubMinSuffix > 0 {
			minSuffix = rule.ScrubMinSuffix
		}
		add(scrubPattern{
			prefix:      rule.Prefix,
			label:       safeLabel(rule.MaskPrefix),
			minSuffix:   minSuffix,
			exactLength: rule.ExactLength,
			alphabet:    rule.Alphabet,
		})
	}
	for _, rule := range keyShapes.Deny {
		add(scrubPattern{
			prefix:    rule.Prefix,
			label:     safeLabel(string(rule.Kind)),
			minSuffix: rule.MinSuffix,
			alphabet:  rule.Alphabet,
		})
	}
	for i := range gate {
		sort.Slice(gate[i], func(left, right int) bool {
			return len(gate[i][left].prefix) > len(gate[i][right].prefix)
		})
	}
	return gate
}

func buildKeywordGate() [256][]string {
	var gate [256][]string
	for _, keyword := range []string{"credential", "password", "api_key", "api-key", "apikey", "secret", "token"} {
		gate[keyword[0]] = append(gate[keyword[0]], keyword)
		gate[keyword[0]-('a'-'A')] = append(gate[keyword[0]-('a'-'A')], keyword)
	}
	return gate
}

func shapeAt(text string, at int) (int, string, bool) {
	for _, pattern := range patternGate[text[at]] {
		if !strings.HasPrefix(text[at:], pattern.prefix) {
			continue
		}
		suffixStart := at + len(pattern.prefix)
		end := suffixStart
		if pattern.exactLength > 0 {
			end = at + pattern.exactLength
			if end > len(text) || (end < len(text) && allowedPatternByte(text[end], pattern.alphabet)) {
				continue
			}
			for i := suffixStart; i < end; i++ {
				if !allowedPatternByte(text[i], pattern.alphabet) {
					end = suffixStart
					break
				}
			}
			if end == suffixStart {
				continue
			}
		} else {
			for end < len(text) && allowedPatternByte(text[end], pattern.alphabet) {
				end++
			}
			if end-suffixStart < pattern.minSuffix {
				continue
			}
		}
		if placeholder(text[suffixStart:end]) {
			continue
		}
		return end, pattern.label, true
	}
	return 0, "", false
}

func bearerAt(text string, at int) (int, int, string, bool) {
	const word = "bearer"
	if at+len(word) > len(text) || !strings.EqualFold(text[at:at+len(word)], word) {
		return 0, 0, "", false
	}
	if at > 0 && isWordByte(text[at-1]) {
		return 0, 0, "", false
	}
	start := at + len(word)
	if start >= len(text) || text[start] != ' ' && text[start] != '\t' {
		return 0, 0, "", false
	}
	for start < len(text) && (text[start] == ' ' || text[start] == '\t') {
		start++
	}
	end := start
	for end < len(text) && bearerByte(text[end]) {
		end++
	}
	if end-start < 16 || placeholder(text[start:end]) {
		return 0, 0, "", false
	}
	return start, end, "bearer", true
}

func jwtAt(text string, at int) (int, string, bool) {
	if !strings.HasPrefix(text[at:], "eyJ") {
		return 0, "", false
	}
	end := at
	dots := 0
	for end < len(text) {
		if text[end] == '.' {
			dots++
			if dots > 2 {
				break
			}
			end++
			continue
		}
		if !base64URLByte(text[end]) {
			break
		}
		end++
	}
	if dots != 2 {
		return 0, "", false
	}
	firstDot := strings.IndexByte(text[at:end], '.')
	if firstDot <= 0 {
		return 0, "", false
	}
	header, err := base64.RawURLEncoding.DecodeString(text[at : at+firstDot])
	if err != nil {
		return 0, "", false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(header, &object) != nil || len(object["alg"]) == 0 {
		return 0, "", false
	}
	return end, "jwt", true
}

func privateKeyAt(text string, at int) (int, string, bool) {
	const begin = "-----BEGIN "
	if !strings.HasPrefix(text[at:], begin) {
		return 0, "", false
	}
	headerEnd := strings.Index(text[at+len(begin):], "-----")
	if headerEnd < 0 {
		return 0, "", false
	}
	kind := text[at+len(begin) : at+len(begin)+headerEnd]
	if !strings.HasSuffix(kind, "PRIVATE KEY") || !upperWords(kind) {
		return 0, "", false
	}
	marker := "-----END " + kind + "-----"
	relativeEnd := strings.Index(text[at+len(begin)+headerEnd+5:], marker)
	if relativeEnd < 0 {
		return len(text), "private-key", true
	}
	endStart := at + len(begin) + headerEnd + 5 + relativeEnd
	return endStart + len(marker), "private-key", true
}

func keywordValueAt(text string, at int) (int, int, string, bool) {
	if at > 0 && isWordByte(text[at-1]) {
		return 0, 0, "", false
	}
	for _, keyword := range keywordGate[text[at]] {
		if at+len(keyword) > len(text) || !strings.EqualFold(text[at:at+len(keyword)], keyword) {
			continue
		}
		cursor := at + len(keyword)
		if cursor < len(text) && isWordByte(text[cursor]) {
			continue
		}
		if at > 0 && (text[at-1] == '"' || text[at-1] == '\'') &&
			cursor < len(text) && text[cursor] == text[at-1] {
			cursor++
		}
		for cursor < len(text) && (text[cursor] == ' ' || text[cursor] == '\t') {
			cursor++
		}
		if cursor >= len(text) || text[cursor] != ':' && text[cursor] != '=' {
			continue
		}
		cursor++
		for cursor < len(text) && (text[cursor] == ' ' || text[cursor] == '\t') {
			cursor++
		}
		quote := byte(0)
		if cursor < len(text) && (text[cursor] == '\'' || text[cursor] == '"') {
			quote = text[cursor]
			cursor++
		}
		start := cursor
		if quote != 0 {
			for cursor < len(text) && text[cursor] != quote {
				if text[cursor] == '\\' && cursor+1 < len(text) {
					cursor += 2
					continue
				}
				cursor++
			}
			if cursor >= len(text) {
				continue
			}
		} else {
			for cursor < len(text) && !keywordTerminator(text[cursor]) {
				cursor++
			}
		}
		if cursor-start < minimumLiteralLength || placeholder(text[start:cursor]) {
			continue
		}
		return start, cursor, safeLabel(keyword), true
	}
	return 0, 0, "", false
}

func placeholder(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == `""` || strings.HasPrefix(trimmed, "$") ||
		strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") {
		return true
	}
	upper := strings.ToUpper(trimmed)
	for _, marker := range []string{"EXAMPLE", "XXXX", "YOUR_", "CHANGEME"} {
		if strings.Contains(upper, marker) {
			return true
		}
	}
	first := byte(0)
	count := 0
	allSame := true
	for i := 0; i < len(trimmed); i++ {
		if !isAlphaNumeric(trimmed[i]) {
			continue
		}
		if count == 0 {
			first = trimmed[i]
		} else if trimmed[i] != first {
			allSame = false
		}
		count++
	}
	return count > 0 && allSame
}

func labelForLiteral(value string) string {
	if len(value) > 0 {
		for _, pattern := range patternGate[value[0]] {
			if strings.HasPrefix(value, pattern.prefix) {
				return pattern.label
			}
		}
	}
	return "credential"
}

func sentinel(value, label string) string {
	mac := hmac.New(sha256.New, salt[:])
	_, _ = mac.Write([]byte(value))
	digest := mac.Sum(nil)
	return "[redacted " + safeLabel(label) + " #" + hex.EncodeToString(digest[:4]) + "]"
}

func processSalt() [32]byte {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic("redact: cannot initialize the process-local fingerprint salt")
	}
	return value
}

func safeLabel(value string) string {
	value = strings.Trim(value, "-_")
	var label strings.Builder
	for i := 0; i < len(value) && label.Len() < 24; i++ {
		char := value[i]
		switch {
		case char >= 'A' && char <= 'Z':
			label.WriteByte(char + ('a' - 'A'))
		case char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '-':
			label.WriteByte(char)
		case char == '_':
			label.WriteByte('-')
		}
	}
	if label.Len() == 0 {
		return "credential"
	}
	return label.String()
}

func allowedPatternByte(value byte, alphabet string) bool {
	if isAlphaNumeric(value) {
		return true
	}
	return alphabet == "key" && (value == '-' || value == '_')
}

func bearerByte(value byte) bool {
	return isAlphaNumeric(value) || value == '-' || value == '_' || value == '.' || value == '='
}

func base64URLByte(value byte) bool {
	return isAlphaNumeric(value) || value == '-' || value == '_'
}

func isWordByte(value byte) bool {
	return isAlphaNumeric(value) || value == '_'
}

func keywordTerminator(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n' ||
		value == ',' || value == ';' || value == '}' || value == ']'
}

func upperWords(value string) bool {
	if value == "" {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] != ' ' && (value[i] < 'A' || value[i] > 'Z') {
			return false
		}
	}
	return true
}

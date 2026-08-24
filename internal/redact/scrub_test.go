package redact

import (
	"bufio"
	"encoding/base64"
	"os"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"
)

func TestScrubRemovesEveryEmbeddedShape(t *testing.T) {
	for _, rule := range keyShapes.Infer {
		rule := rule
		t.Run("infer_"+rule.Provider+"_"+rule.Prefix, func(t *testing.T) {
			candidate := scrubCandidate(rule.Prefix, rule.MinSuffix, rule.ExactLength)
			assertScrubbed(t, candidate)
		})
	}
	for _, rule := range keyShapes.Deny {
		if rule.Prefix == "" {
			continue
		}
		rule := rule
		t.Run("deny_"+string(rule.Kind)+"_"+rule.Prefix, func(t *testing.T) {
			candidate := scrubCandidate(rule.Prefix, rule.MinSuffix, 0)
			assertScrubbed(t, candidate)
		})
	}
}

func TestScrubHonorsEveryEmbeddedShapeBoundary(t *testing.T) {
	for _, rule := range keyShapes.Infer {
		rule := rule
		t.Run("infer_"+rule.Provider+"_"+rule.Prefix, func(t *testing.T) {
			if rule.ExactLength > 0 {
				for _, candidate := range []string{
					rule.Prefix + alternating(rule.ExactLength-len(rule.Prefix)-1),
					rule.Prefix + alternating(rule.ExactLength-len(rule.Prefix)+1),
				} {
					assertUnchanged(t, candidate)
				}
				return
			}
			minimum := rule.MinSuffix
			if rule.ScrubMinSuffix > 0 {
				minimum = rule.ScrubMinSuffix
			}
			assertUnchanged(t, rule.Prefix+alternating(minimum-1))
			assertUnchanged(t, rule.Prefix+alternating(minimum-1)+"."+alternating(8))
		})
	}
	for _, rule := range keyShapes.Deny {
		if rule.Prefix == "" {
			continue
		}
		rule := rule
		t.Run("deny_"+string(rule.Kind)+"_"+rule.Prefix, func(t *testing.T) {
			assertUnchanged(t, rule.Prefix+alternating(rule.MinSuffix-1))
			assertUnchanged(t, rule.Prefix+alternating(rule.MinSuffix-1)+"."+alternating(8))
		})
	}
}

func TestScrubRemovesDurableOnlyPatterns(t *testing.T) {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"1234567890"}`))
	jwt := header + "." + payload + ".YWJjZGVmZ2hpamtsbW5vcA"
	privateKey := "-----BEGIN PRIVATE KEY-----\n" + alternating(96) + "\n-----END PRIVATE KEY-----"

	tests := map[string]string{
		"bearer":      "Authorization: Bearer " + alternating(40),
		"jwt":         "token=" + jwt,
		"private key": "before\n" + privateKey + "\nafter",
		"keyword =":   "api_key = " + alternating(32),
		"keyword :":   `{"password":"` + alternating(32) + `"}`,
	}
	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			assertScrubbed(t, input)
		})
	}
}

func TestBearerAndKeywordMinimumLengthsAreExact(t *testing.T) {
	assertUnchanged(t, "Bearer "+alternating(15))
	assertScrubbed(t, "Bearer "+alternating(16))
	assertUnchanged(t, "secret="+alternating(minimumLiteralLength-1))
	assertScrubbed(t, "secret="+alternating(minimumLiteralLength))
}

func TestRegisteredShapeLessLiteralsAreStableAndCorrelatable(t *testing.T) {
	one := "mistral" + alternating(37)
	two := "cohere" + alternating(38)
	Register(one)
	Register(two)

	first := Scrub("one=" + one + " again=" + one)
	second := Scrub("one=" + one + " again=" + one)
	if first != second {
		t.Fatalf("registered sentinel changed within one process:\nfirst:  %s\nsecond: %s", first, second)
	}
	if strings.Contains(first, one) || strings.Contains(first, one[:8]) || strings.Contains(first, one[len(one)-8:]) {
		t.Fatalf("registered sentinel retained a usable literal fragment: %s", first)
	}
	parts := strings.Split(first, "[redacted credential #")
	if len(parts) != 3 || !strings.Contains(parts[1], "]") || !strings.Contains(parts[2], "]") {
		t.Fatalf("repeated literal did not produce two credential sentinels: %s", first)
	}
	if strings.SplitN(parts[1], "]", 2)[0] != strings.SplitN(parts[2], "]", 2)[0] {
		t.Fatalf("the same literal did not correlate to the same fingerprint: %s", first)
	}

	other := Scrub("two=" + two)
	if other == first || strings.Contains(other, strings.SplitN(parts[1], "]", 2)[0]) {
		t.Fatalf("different literals shared a correlation fingerprint:\none: %s\ntwo: %s", first, other)
	}
}

func TestRegisteredLiteralWinsBeforePlaceholderSuppressionAndLongestWins(t *testing.T) {
	placeholderValue := "EXAMPLE-X7p9"
	shorter := "shape-less-C4nary"
	longer := shorter + "-extended"
	Register(placeholderValue)
	Register(shorter)
	Register(longer)

	placeholderOutput := Scrub("value=" + placeholderValue)
	if strings.Contains(placeholderOutput, placeholderValue) ||
		!strings.Contains(placeholderOutput, "[redacted credential #") {
		t.Fatalf("registered placeholder-shaped literal was suppressed: %s", placeholderOutput)
	}

	overlapOutput := Scrub("value=" + longer)
	if strings.Contains(overlapOutput, shorter) || strings.Contains(overlapOutput, "extended") {
		t.Fatalf("overlapping registered literal leaked a fragment: %s", overlapOutput)
	}
	if strings.Count(overlapOutput, "[redacted credential #") != 1 {
		t.Fatalf("longest registered literal was not replaced exactly once: %s", overlapOutput)
	}
}

func TestRegisterAndScrubAreConcurrentSafe(t *testing.T) {
	const workers = 24
	start := make(chan struct{})
	errors := make(chan string, workers)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		worker := worker
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			literal := "concurrent-C4nary-" + string(rune('A'+worker)) + alternating(24)
			Register(literal)
			output := Scrub("value=" + literal)
			if strings.Contains(output, literal) || !strings.Contains(output, "[redacted credential #") {
				errors <- output
			}
		}()
	}
	close(start)
	wait.Wait()
	close(errors)
	for output := range errors {
		t.Errorf("concurrent registration did not scrub its literal: %s", output)
	}
}

func TestPatternSentinelRetainsNoReusableSuffix(t *testing.T) {
	value := scrubCandidate("sk-or-v1-", 32, 0)
	output := Scrub(value)
	if output == value {
		t.Fatal("shape was not scrubbed")
	}
	for _, fragment := range []string{value[len(value)-8:], value[10:18]} {
		if strings.Contains(output, fragment) {
			t.Fatalf("sentinel retained credential fragment %q: %s", fragment, output)
		}
	}
}

func TestScrubIsIdempotent(t *testing.T) {
	input := "key=" + scrubCandidate("sk-or-v1-", 24, 0) + " bearer Bearer " + alternating(40)
	once := Scrub(input)
	twice := Scrub(once)
	if once != twice {
		t.Fatalf("Scrub is not idempotent:\nonce:  %s\ntwice: %s", once, twice)
	}
}

func TestScrubSuppressesPlaceholdersAndFalsePositives(t *testing.T) {
	file, err := os.Open("testdata/falsepositives.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	line := 0
	for scanner.Scan() {
		line++
		input := scanner.Text()
		if input == "" || strings.HasPrefix(input, "#") {
			continue
		}
		if got := Scrub(input); got != input {
			t.Errorf("false-positive corpus line %d changed:\ninput: %s\ngot:   %s", line, input, got)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestScrubRecognizesOnlyJWTHeadersWithAnAlgorithm(t *testing.T) {
	notJWT := "eyJub3QiOiJhbGdvcml0aG0ifQ.YWJj.ZGVm"
	if got := Scrub(notJWT); got != notJWT {
		t.Fatalf("ordinary three-part base64 changed: %s", got)
	}
}

func TestPrivateKeyScrubPreservesSurroundingText(t *testing.T) {
	key := "-----BEGIN EC PRIVATE KEY-----\n" + alternating(96) + "\n-----END EC PRIVATE KEY-----"
	output := Scrub("before\n" + key + "\nafter")
	if !strings.HasPrefix(output, "before\n[redacted private-key #") || !strings.HasSuffix(output, "]\nafter") {
		t.Fatalf("private-key scrub damaged surrounding text: %s", output)
	}
	if strings.Contains(output, alternating(16)) {
		t.Fatalf("private-key body survived scrubbing: %s", output)
	}
}

func TestScrubHandlesMalformedUTF8Deterministically(t *testing.T) {
	input := string([]byte{'a', 0xff, ' ', 's', 'k', '-', 'o', 'r', '-', 'v', '1', '-',
		'a', 'B', '3', 'c', 'D', '4', 'e', 'F', '5', 'g', 'H', '6', 'j', 'K', '7', 'm', 'N', '8', 'p', 'Q'})
	first := Scrub(input)
	second := Scrub(input)
	if first != second || !strings.HasPrefix(first, string([]byte{'a', 0xff, ' '})) {
		t.Fatalf("malformed UTF-8 handling was unstable or changed the prefix: %q / %q", first, second)
	}
	if strings.Contains(first, "sk-or-v1-") {
		t.Fatalf("malformed UTF-8 prevented credential scrubbing: %q", first)
	}
}

func FuzzScrubPreservesValidUTF8AndIdempotence(f *testing.F) {
	for _, seed := range []string{
		"hello, octopus 🐙",
		"sk-or-v1-" + alternating(32),
		"token = " + alternating(32),
		"embedded NUL \x00 and replacement �",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		output := Scrub(input)
		if utf8.ValidString(input) && !utf8.ValidString(output) {
			t.Fatalf("Scrub turned valid UTF-8 into invalid UTF-8: %q", output)
		}
		if twice := Scrub(output); twice != output {
			t.Fatalf("Scrub is not idempotent for %q: %q then %q", input, output, twice)
		}
	})
}

func BenchmarkScrub12KiB(b *testing.B) {
	text := strings.Repeat("ordinary build output: package compiled successfully\n", 256)
	text = text[:12<<10]
	b.SetBytes(int64(len(text)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Scrub(text)
	}
}

func assertScrubbed(t *testing.T, input string) {
	t.Helper()
	output := Scrub("before " + input + " after")
	if output == "before "+input+" after" {
		t.Fatalf("Scrub changed nothing: %s", output)
	}
	if strings.Contains(output, input) {
		t.Fatalf("Scrub retained the matched value: %s", output)
	}
	if !strings.Contains(output, "[redacted ") {
		t.Fatalf("Scrub did not emit a sentinel: %s", output)
	}
}

func assertUnchanged(t *testing.T, input string) {
	t.Helper()
	if output := Scrub(input); output != input {
		t.Fatalf("Scrub changed a below-boundary lookalike:\ninput:  %s\noutput: %s", input, output)
	}
}

func scrubCandidate(prefix string, minSuffix, exactLength int) string {
	suffixLength := max(minSuffix, 24)
	if exactLength > 0 {
		suffixLength = exactLength - len(prefix)
	}
	return prefix + alternating(suffixLength)
}

func alternating(length int) string {
	const alphabet = "aB3cD4eF5gH6jK7mN8pQ9rS2tV"
	var out strings.Builder
	out.Grow(length)
	for out.Len() < length {
		remaining := length - out.Len()
		if remaining < len(alphabet) {
			out.WriteString(alphabet[:remaining])
			break
		}
		out.WriteString(alphabet)
	}
	return out.String()
}

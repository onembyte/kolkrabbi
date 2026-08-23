package redact

import "strings"

// Mask keeps a shape prefix and the last four bytes only when at least eight
// bytes remain hidden. The two visible slices can therefore never overlap.
func Mask(value string) string {
	value = strings.TrimSpace(value)
	const (
		tail      = 4
		minHidden = 8
	)
	if len(value) < tail+minHidden {
		return "…"
	}

	prefix := shapePrefix(value)
	if prefix == "" && len(value) >= 4+tail+minHidden {
		prefix = value[:4]
	}
	if len(prefix)+tail+minHidden > len(value) {
		prefix = ""
	}
	return prefix + "…" + value[len(value)-tail:]
}

func shapePrefix(value string) string {
	longest := 0
	mask := ""
	for _, rule := range keyShapes.Infer {
		if strings.HasPrefix(value, rule.Prefix) && len(rule.Prefix) > longest {
			longest = len(rule.Prefix)
			mask = rule.MaskPrefix
		}
	}
	return mask
}

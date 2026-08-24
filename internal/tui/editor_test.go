package tui

import (
	"reflect"
	"testing"
)

func TestEditorEditsAndSubmitsAnExactMultilineDraft(t *testing.T) {
	editor := NewEditor(64 * 1024)
	for _, key := range []Key{
		{Kind: KeyText, Text: "first"},
		{Kind: KeyNewline},
		{Kind: KeyText, Text: "secod"},
		{Kind: KeyLeft},
		{Kind: KeyText, Text: "n"},
	} {
		result := editor.Update(key)
		if !result.Changed {
			t.Fatalf("%#v did not report a changed draft", key)
		}
	}
	if got := editor.Draft(); got != "first\nsecond" {
		t.Fatalf("edited draft = %q", got)
	}

	result := editor.Update(Key{Kind: KeyEnter})
	if !result.Submit || result.Submitted != "first\nsecond" {
		t.Fatalf("submit result = %#v", result)
	}
	if got := editor.Draft(); got != "" {
		t.Fatalf("submitted draft was not cleared: %q", got)
	}

	result = editor.Update(Key{Kind: KeyUp})
	if !result.Changed || editor.Draft() != "first\nsecond" {
		t.Fatalf("history did not restore exact multiline input: %#v, %q", result, editor.Draft())
	}
}

func TestEditorKeepsDraftAcrossInterruptAndExitsOnEOF(t *testing.T) {
	editor := NewEditor(100)
	editor.Update(Key{Kind: KeyText, Text: "keep this draft"})

	result := editor.Update(Key{Kind: KeyInterrupt})
	if !result.Interrupt || result.Changed || editor.Draft() != "keep this draft" {
		t.Fatalf("interrupt corrupted draft: %#v, %q", result, editor.Draft())
	}
	result = editor.Update(Key{Kind: KeyEOF})
	if !result.Exit || editor.Draft() != "keep this draft" {
		t.Fatalf("EOF did not exit cleanly with draft retained: %#v, %q", result, editor.Draft())
	}
}

func TestDecoderHandlesFragmentedControlSequencesAndMultilinePaste(t *testing.T) {
	decoder := NewDecoder()
	var got []Key
	for _, chunk := range [][]byte{
		[]byte("/mo"),
		[]byte("del"),
		[]byte("\x1b["),
		[]byte("D"),
		[]byte("\x1b[13;2"),
		[]byte("u"),
		[]byte("\x1b[200~first\nsec"),
		[]byte("ond\x1b[201~"),
	} {
		got = append(got, decoder.Feed(chunk)...)
	}
	want := []Key{
		{Kind: KeyText, Text: "/mo"},
		{Kind: KeyText, Text: "del"},
		{Kind: KeyLeft},
		{Kind: KeyNewline},
		{Kind: KeyPaste, Text: "first\nsecond"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("decoded keys:\n got %#v\nwant %#v", got, want)
	}
}

func TestDecoderRecognizesTabAsACompletionKey(t *testing.T) {
	got := NewDecoder().Feed([]byte("/mo\t"))
	want := []Key{{Kind: KeyText, Text: "/mo"}, {Kind: KeyTab}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tab decode = %#v, want %#v", got, want)
	}
}

func TestDecoderTreatsBareCarriageReturnAndLineFeedAsEnter(t *testing.T) {
	for name, input := range map[string][]byte{
		"carriage return": {'\r'},
		"line feed":       {'\n'},
	} {
		t.Run(name, func(t *testing.T) {
			got := NewDecoder().Feed(input)
			want := []Key{{Kind: KeyEnter}}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("decoded Enter = %#v, want %#v", got, want)
			}
		})
	}
}

func TestEditorCapsOnePasteWithoutSplittingUTF8(t *testing.T) {
	editor := NewEditor(3)
	result := editor.Update(Key{Kind: KeyPaste, Text: "🐙abc"})
	if !result.Changed || editor.Draft() != "🐙ab" {
		t.Fatalf("capped paste = %#v, %q", result, editor.Draft())
	}
}

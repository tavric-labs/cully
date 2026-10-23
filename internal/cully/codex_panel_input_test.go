package cully

import (
	"bytes"
	"testing"
)

func TestPanelInputSplitControlsAndEscape(t *testing.T) {
	var input panelInput
	if got := input.feed([]byte("\x1b["), false); len(got) != 0 {
		t.Fatal(got)
	}
	got := input.feed([]byte("5~"), false)
	if len(got) != 1 || got[0].Action != "pageup" {
		t.Fatal(got)
	}
	input.feed([]byte{0x1b}, false)
	got = input.feed(nil, true)
	if len(got) != 1 || got[0].Action != "escape" {
		t.Fatal(got)
	}
}

func TestPanelInputPreservesPasteAndOtherBytes(t *testing.T) {
	var input panelInput
	text := []byte("hello\x1b[200~pasted\x1ba\x1b[A\x1d\x1b[17~\x1b[201~\x1b[Zé")
	var forwarded []byte
	for _, b := range text {
		for _, key := range input.feed([]byte{b}, false) {
			if key.Action != "" {
				t.Fatalf("paste/unknown sequence consumed as %q", key.Action)
			}
			forwarded = append(forwarded, key.Data...)
		}
	}
	for _, key := range input.feed(nil, true) {
		forwarded = append(forwarded, key.Data...)
	}
	if !bytes.Equal(forwarded, text) {
		t.Fatalf("got %q want %q", forwarded, text)
	}
}

func TestPanelFocusWithoutMetaSetting(t *testing.T) {
	for _, sequence := range []string{"\x1d", "\x1b[17~"} {
		var input panelInput
		var actions []panelKey
		for _, b := range []byte("typing" + sequence + "after") {
			actions = append(actions, input.feed([]byte{b}, false)...)
		}
		var forwarded []byte
		focus := 0
		for _, key := range actions {
			if key.Action == "focus" {
				focus++
			} else {
				forwarded = append(forwarded, key.Data...)
			}
		}
		if focus != 1 || string(forwarded) != "typingafter" {
			t.Fatalf("sequence %q: focus=%d forwarded=%q", sequence, focus, forwarded)
		}
	}
}

func TestPanelInputMouseEnterAndPaste(t *testing.T) {
	var input panelInput
	var got []panelKey
	for _, b := range []byte("\x1b[<0;22;18M\r\t") {
		got = append(got, input.feed([]byte{b}, false)...)
	}
	if len(got) != 3 || got[0].Mouse == nil || got[0].Mouse.X != 22 || got[0].Mouse.Y != 18 || got[1].Action != "enter" || got[2].Action != "details" {
		t.Fatal(got)
	}
	text := []byte("\x1b[200~hello\r\t\x1b[<0;22;18M\x1b[201~")
	var forwarded []byte
	for _, key := range input.feed(text, false) {
		if key.Action != "" {
			t.Fatal("paste consumed", key)
		}
		forwarded = append(forwarded, key.Data...)
	}
	if !bytes.Equal(text, forwarded) {
		t.Fatal(string(forwarded))
	}
}

func TestNativeModifiedKeysStayAtomicAcrossReads(t *testing.T) {
	for _, sequence := range []string{
		"\x1b[1;2C", "\x1b[1;2D", "\x1b[1;2A", "\x1b[1;2B",
		"\x1b[1;5C", "\x1b[1;6D", "\x1b[1;3C", "\x1b[Z",
		"\x1b[13;2u", "\x1b[27;2;13~", "\x1b\r", "\x1ba", "\x1bOQ",
		"\x1b[99;5:3u", "\x1b]11;rgb:ffff/ffff/ffff\x1b\\",
	} {
		for split := 0; split <= len(sequence); split++ {
			var input panelInput
			keys := input.feed([]byte(sequence[:split]), false)
			keys = append(keys, input.feed([]byte(sequence[split:]), false)...)
			if len(keys) != 1 || keys[0].Action != "" || string(keys[0].Data) != sequence {
				t.Fatalf("sequence %q split %d was consumed or fragmented: %+v", sequence, split, keys)
			}
		}
	}
}

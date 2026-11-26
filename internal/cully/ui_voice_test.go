package cully

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func uiPostAudio(s *uiServer, ct string, body []byte) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/voice/transcribe", bytes.NewReader(body))
	req.Header.Set("Content-Type", ct)
	s.voiceTranscribe(rec, req)
	return rec
}

func TestVoiceIsNotReadyUntilSetUpAndRefusesBadInput(t *testing.T) {
	m, _, _ := uiIndexManager(t)
	t.Setenv("CULLY_STT_CMD", "")
	s := &uiServer{m: m}
	if st := m.voice.status(); st.Ready || st.State != "none" {
		t.Fatalf("status = %+v", st)
	}
	audio := bytes.Repeat([]byte("a"), 500)
	if rec := uiPostAudio(s, "audio/webm;codecs=opus", audio); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "not set up") {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if rec := uiPostAudio(s, "text/plain", audio); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("a non-audio type was accepted: %d", rec.Code)
	}
	if err := m.voice.setup("not-a-model"); err == nil {
		t.Fatal("an unknown model was accepted")
	}
}

func TestVoiceTranscribesThroughACustomEngineAndDeletesTheAudio(t *testing.T) {
	m, _, _ := uiIndexManager(t)
	t.Setenv("CULLY_STT_CMD", `["echo","start the nightly build"]`)
	s := &uiServer{m: m}
	if st := m.voice.status(); !st.Ready || st.Engine != "custom" {
		t.Fatalf("status = %+v", st)
	}
	rec := uiPostAudio(s, "audio/webm", bytes.Repeat([]byte("a"), 500))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"text":"start the nightly build"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if left, _ := filepath.Glob(filepath.Join(uiVoiceDir(), "tmp", "clip-*")); len(left) != 0 {
		t.Fatalf("audio was kept: %v", left)
	}
	if rec := uiPostAudio(s, "audio/webm", []byte("tiny")); !strings.Contains(rec.Body.String(), `"text":""`) {
		t.Fatalf("an empty clip: %s", rec.Body)
	}
}

func TestVoiceKeepsOneLoadedWorkerAndReadsItsAnswers(t *testing.T) {
	m, _, _ := uiIndexManager(t)
	t.Setenv("CULLY_STT_CMD", "")
	py := uiVoicePython()
	if err := os.MkdirAll(filepath.Dir(py), 0o700); err != nil {
		t.Fatal(err)
	}
	// A stand-in for the model: one answer per file path on stdin.
	fake := "#!/bin/sh\nn=0\nwhile read f; do n=$((n+1)); if [ -f \"$f\" ]; then echo \"{\\\"text\\\":\\\"clip $n\\\"}\"; else echo '{\"error\":\"x\"}'; fi; done\n"
	if err := os.WriteFile(py, []byte(fake), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(uiVoiceDir(), "ready"), []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.voice.stop)
	for _, want := range []string{"clip 1", "clip 2"} { // the second clip uses the same running worker
		got, err := m.voice.transcribe(bytes.Repeat([]byte("a"), 500), ".webm")
		if err != nil || got != want {
			t.Fatalf("got %q, %v, want %q", got, err, want)
		}
	}
	if st := m.voice.status(); !st.Ready || st.Engine != "faster-whisper" || st.Model != uiVoiceDefaultModel {
		t.Fatalf("status = %+v", st)
	}
}

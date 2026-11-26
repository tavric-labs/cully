package cully

import (
	"bufio"
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Voice. The page records what you say, sends the audio here, and gets text
// back to use as the task. The speech model runs on this machine: Cully sets up
// faster-whisper in its own folder (nothing else is installed), keeps one worker
// loaded while you use it and stops it after ten idle minutes. The audio goes to
// a private temporary file that is deleted at once, and is never kept.
// CULLY_STT_CMD can name another engine: a JSON list of words with {file} where
// the audio file goes, that prints the text.

//go:embed ui_voice_stt.py
var uiVoiceScript string

const (
	uiVoiceMaxAudio   = 12 << 20
	uiVoiceIdle       = 10 * time.Minute
	uiVoiceTimeout    = 3 * time.Minute
	uiVoiceSetupLimit = 45 * time.Minute
)

// uiVoiceModels are the models offered. The default is distil-large-v3: close to
// the best accuracy for English and several times faster than large-v3.
var uiVoiceModels = map[string]bool{"tiny.en": true, "base.en": true, "small.en": true, "medium.en": true, "distil-large-v3": true, "large-v3": true}

const uiVoiceDefaultModel = "distil-large-v3"

var uiVoiceTypes = map[string]string{"audio/webm": ".webm", "audio/ogg": ".ogg", "audio/mp4": ".m4a", "audio/wav": ".wav", "audio/x-wav": ".wav", "audio/mpeg": ".mp3"}

type uiVoice struct {
	mu      sync.Mutex // one transcription at a time
	setupMu sync.Mutex
	state   string // "", installing or failed
	detail  string
	worker  *exec.Cmd
	in      io.WriteCloser
	out     *bufio.Reader
	timer   *time.Timer
}

type uiVoiceStatus struct {
	Ready  bool   `json:"ready"`
	Engine string `json:"engine,omitempty"` // custom or faster-whisper
	Model  string `json:"model,omitempty"`
	State  string `json:"state"` // none, installing, ready or failed
	Detail string `json:"detail,omitempty"`
}

func uiVoiceDir() string { return filepath.Join(cullyDir(), "voice") }

func uiVoicePython() string { return filepath.Join(uiVoiceDir(), "venv", "bin", "python") }

func uiVoiceModel() string {
	if b, err := os.ReadFile(filepath.Join(uiVoiceDir(), "model")); err == nil && uiVoiceModels[strings.TrimSpace(string(b))] {
		return strings.TrimSpace(string(b))
	}
	return uiVoiceDefaultModel
}

func uiVoiceCustom() []string {
	var cmd []string
	if raw := strings.TrimSpace(os.Getenv("CULLY_STT_CMD")); raw != "" && json.Unmarshal([]byte(raw), &cmd) == nil && len(cmd) > 0 {
		return cmd
	}
	return nil
}

func (v *uiVoice) status() uiVoiceStatus {
	if uiVoiceCustom() != nil {
		return uiVoiceStatus{Ready: true, Engine: "custom", State: "ready"}
	}
	if _, err := os.Stat(filepath.Join(uiVoiceDir(), "ready")); err == nil {
		return uiVoiceStatus{Ready: true, Engine: "faster-whisper", Model: uiVoiceModel(), State: "ready"}
	}
	v.setupMu.Lock()
	defer v.setupMu.Unlock()
	switch v.state {
	case "installing", "failed":
		return uiVoiceStatus{State: v.state, Detail: v.detail}
	}
	return uiVoiceStatus{State: "none"}
}

// setup installs faster-whisper in a private virtual environment and downloads
// the model, in the background. It runs fixed commands; only the model name
// varies, and only from the list above.
func (v *uiVoice) setup(model string) error {
	if model == "" {
		model = uiVoiceDefaultModel
	}
	if !uiVoiceModels[model] {
		return errors.New("unknown model")
	}
	v.setupMu.Lock()
	if v.state == "installing" {
		v.setupMu.Unlock()
		return errors.New("setup is already running")
	}
	v.state, v.detail = "installing", "Starting"
	v.setupMu.Unlock()
	go func() {
		err := v.install(model)
		v.setupMu.Lock()
		defer v.setupMu.Unlock()
		if err != nil {
			v.state, v.detail = "failed", err.Error()
			return
		}
		v.state, v.detail = "", ""
	}()
	return nil
}

func (v *uiVoice) step(s string) {
	v.setupMu.Lock()
	v.detail = s
	v.setupMu.Unlock()
}

func (v *uiVoice) install(model string) error {
	dir := uiVoiceDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), uiVoiceSetupLimit)
	defer cancel()
	run := func(what string, name string, args ...string) error {
		v.step(what)
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "HF_HOME="+filepath.Join(dir, "hf"), "PIP_DISABLE_PIP_VERSION_CHECK=1", "CULLY_STT_MODEL="+model)
		if out, err := cmd.CombinedOutput(); err != nil {
			tail := strings.TrimSpace(string(out))
			if len(tail) > 300 {
				tail = tail[len(tail)-300:]
			}
			return errors.New(what + " failed: " + tail)
		}
		return nil
	}
	if _, err := os.Stat(uiVoicePython()); err != nil {
		if err := run("Creating the Python environment", "python3", "-m", "venv", filepath.Join(dir, "venv")); err != nil {
			return err
		}
	}
	if err := run("Installing faster-whisper", uiVoicePython(), "-m", "pip", "install", "-q", "faster-whisper"); err != nil {
		return err
	}
	script := filepath.Join(dir, "stt.py")
	if err := os.WriteFile(script, []byte(uiVoiceScript), 0o600); err != nil {
		return err
	}
	if err := run("Downloading the "+model+" speech model", uiVoicePython(), script, "--warmup"); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "model"), []byte(model), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "ready"), []byte("1"), 0o600)
}

// transcribe turns audio into text.
func (v *uiVoice) transcribe(audio []byte, ext string) (string, error) {
	st := v.status()
	if !st.Ready {
		return "", errors.New("voice is not set up yet")
	}
	dir := filepath.Join(uiVoiceDir(), "tmp")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "clip-*"+ext)
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return "", err
	}
	if _, err := f.Write(audio); err != nil {
		_ = f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if cmd := uiVoiceCustom(); cmd != nil {
		args := make([]string, len(cmd))
		for i, a := range cmd {
			args[i] = strings.ReplaceAll(a, "{file}", f.Name())
		}
		ctx, cancel := context.WithTimeout(context.Background(), uiVoiceTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, args[0], args[1:]...).Output()
		if err != nil {
			return "", errors.New("the speech engine failed")
		}
		return strings.TrimSpace(string(out)), nil
	}
	return v.viaWorker(f.Name())
}

func (v *uiVoice) viaWorker(file string) (string, error) {
	if v.worker == nil {
		script := filepath.Join(uiVoiceDir(), "stt.py")
		if err := os.WriteFile(script, []byte(uiVoiceScript), 0o600); err != nil {
			return "", err
		}
		cmd := exec.Command(uiVoicePython(), script)
		cmd.Env = append(os.Environ(), "HF_HOME="+filepath.Join(uiVoiceDir(), "hf"), "CULLY_STT_MODEL="+uiVoiceModel())
		in, err := cmd.StdinPipe()
		if err != nil {
			return "", err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return "", err
		}
		if err := cmd.Start(); err != nil {
			return "", errors.New("could not start the speech model")
		}
		v.worker, v.in, v.out = cmd, in, bufio.NewReader(out)
	}
	if v.timer != nil {
		v.timer.Stop()
	}
	defer func() {
		if v.worker != nil {
			v.timer = time.AfterFunc(uiVoiceIdle, v.stop)
		}
	}()
	if _, err := io.WriteString(v.in, file+"\n"); err != nil {
		v.kill()
		return "", errors.New("the speech model stopped; try again")
	}
	type reply struct {
		line []byte
		err  error
	}
	ch := make(chan reply, 1)
	go func() { l, err := v.out.ReadBytes('\n'); ch <- reply{l, err} }()
	select {
	case r := <-ch:
		if r.err != nil {
			v.kill()
			return "", errors.New("the speech model stopped; try again")
		}
		var a struct {
			Text  string `json:"text"`
			Error string `json:"error"`
		}
		if json.Unmarshal(bytes.TrimSpace(r.line), &a) != nil {
			return "", errors.New("the speech model gave an unreadable answer")
		}
		if a.Error != "" {
			return "", errors.New("could not read that recording")
		}
		return strings.TrimSpace(a.Text), nil
	case <-time.After(uiVoiceTimeout):
		v.kill()
		return "", errors.New("transcribing took too long")
	}
}

func (v *uiVoice) kill() {
	if v.worker != nil {
		_ = v.in.Close()
		_ = v.worker.Process.Kill()
		_, _ = v.worker.Process.Wait()
		v.worker, v.in, v.out = nil, nil, nil
	}
}

// stop shuts the worker down, freeing its memory. The next clip starts it again.
func (v *uiVoice) stop() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.kill()
}

func (s *uiServer) voiceGet(w http.ResponseWriter, r *http.Request) {
	uiJSON(w, http.StatusOK, s.m.voice.status())
}

func (s *uiServer) voiceSetup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model string `json:"model"`
	}
	if r.ContentLength > 0 && !uiDecode(w, r, &body) {
		return
	}
	if err := s.m.voice.setup(body.Model); err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusAccepted, s.m.voice.status())
}

func (s *uiServer) voiceTranscribe(w http.ResponseWriter, r *http.Request) {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	ext, ok := uiVoiceTypes[ct]
	if !ok {
		uiError(w, http.StatusUnsupportedMediaType, "send audio as webm, ogg, mp4, wav or mp3")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, uiVoiceMaxAudio)
	audio, err := io.ReadAll(r.Body)
	if err != nil {
		uiError(w, http.StatusRequestEntityTooLarge, "that recording is too long")
		return
	}
	if len(audio) < 200 {
		uiJSON(w, http.StatusOK, map[string]string{"text": ""})
		return
	}
	text, err := s.m.voice.transcribe(audio, ext)
	if err != nil {
		uiError(w, http.StatusBadRequest, err.Error())
		return
	}
	uiJSON(w, http.StatusOK, map[string]string{"text": s.m.cfg.Tokens.redact(uiClip(text, uiMaxPromptRunes))})
}

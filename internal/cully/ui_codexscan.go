package cully

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Counting a Codex rollout the way scanTranscript counts a Claude transcript:
// requests, tool calls by tool, files changed (from apply_patch), test runs and
// working time. A command that failed is recognised by the exit code Codex
// prints in its output.

var uiCodexExit = regexp.MustCompile(`(?i)(?:process exited with code|exit code)[: ]+(\d+)`)

var uiPatchFile = regexp.MustCompile(`(?m)^\*\*\* (?:Update|Add|Delete) File: (.+)$`)

type uiCodexRec struct {
	Type      string `json:"type"`
	Timestamp string `json:"timestamp"`
	Payload   struct {
		Type      string          `json:"type"`
		Role      string          `json:"role"`
		Name      string          `json:"name"`
		Input     string          `json:"input"`
		Arguments string          `json:"arguments"`
		Content   json.RawMessage `json:"content"`
		CallID    string          `json:"call_id"`
		Output    json.RawMessage `json:"output"`
	} `json:"payload"`
}

func uiCodexText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var parts []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return ""
	}
	var b strings.Builder
	for _, p := range parts {
		b.WriteString(p.Text)
	}
	return b.String()
}

func scanCodex(path string, withDigest bool) (uiSessionFacts, uiDigest, bool) {
	f, err := os.Open(path)
	if err != nil {
		return uiSessionFacts{}, uiDigest{}, false
	}
	defer f.Close()
	var facts uiSessionFacts
	var dg uiDigest
	files := map[string]struct{}{}
	var prev time.Time
	last, run, sinceCheck := "", 0, false
	hashes, fails := map[string]string{}, map[string]int{}
	r := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			var rec uiCodexRec
			if json.Unmarshal(line, &rec) == nil {
				if t := uiParseTime(rec.Timestamp); !t.IsZero() {
					if !prev.IsZero() && t.After(prev) && t.Sub(prev) <= uiIdleGap {
						facts.ActiveSec += int(t.Sub(prev).Seconds())
					}
					prev = t
				}
				if rec.Type == "response_item" {
					p := rec.Payload
					switch p.Type {
					case "message":
						text := strings.TrimSpace(uiCodexText(p.Content))
						if p.Role == "user" && text != "" && !strings.HasPrefix(text, "<") {
							facts.Turns++
							if withDigest && dg.First == "" {
								dg.First = text
							}
						}
						if p.Role == "assistant" && withDigest && text != "" {
							dg.Last = text
						}
					case "function_call_output", "custom_tool_call_output":
						if m := uiCodexExit.FindStringSubmatch(uiCodexText(p.Output)); m != nil && m[1] != "0" {
							facts.Errors++
							if h, ok := hashes[p.CallID]; ok {
								fails[h]++
								if fails[h] > facts.MaxFailRepeat {
									facts.MaxFailRepeat = fails[h]
								}
							}
						}
					case "function_call", "custom_tool_call":
						facts.Tools++
						if facts.ToolNames == nil {
							facts.ToolNames = map[string]int{}
						}
						facts.ToolNames[p.Name]++
						body := p.Input + p.Arguments
						hash := p.Name + "|" + body
						if p.CallID != "" {
							hashes[p.CallID] = hash
						}
						if hash == last {
							run++
						} else {
							last, run = hash, 1
						}
						if run > facts.MaxRepeat {
							facts.MaxRepeat = run
						}
						if strings.Contains(body, "apply_patch") || strings.Contains(body, "*** Begin Patch") {
							facts.Edits++
							sinceCheck = true
							for _, m := range uiPatchFile.FindAllStringSubmatch(body, -1) {
								files[strings.TrimSpace(m[1])] = struct{}{}
							}
						}
						if uiCheckCommand.MatchString(uiCodexCommandText(body)) {
							facts.Checks++
							sinceCheck = false
						}
					}
				}
			}
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				return facts, dg, false
			}
			break
		}
	}
	facts.Files = len(files)
	facts.Unverified = facts.Edits > 0 && sinceCheck
	if withDigest {
		for p := range files {
			dg.Files = append(dg.Files, p)
		}
		sort.Strings(dg.Files)
		if len(dg.Files) > 15 {
			dg.Files = dg.Files[:15]
		}
	}
	return facts, dg, true
}

// uiScanAgent counts a session file of any supported harness.
func uiScanAgent(agent, path string, withDigest bool) (uiSessionFacts, uiDigest, bool) {
	switch agent {
	case "codex":
		return scanCodex(path, withDigest)
	case "cursor":
		return scanCursor(path, withDigest)
	}
	return scanTranscript(path, withDigest)
}

// uiSessionIDFromFile reads a session's id from its file name: a Claude
// transcript is named by it, a Codex rollout ends with it.
func uiSessionIDFromFile(path string) string {
	n := strings.TrimSuffix(baseName(path), ".jsonl")
	if len(n) >= 36 && uiSessionID.MatchString(n[len(n)-36:]) {
		return n[len(n)-36:]
	}
	return n
}

func baseName(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// uiCodexCommandText loosens the quoting around a command that Codex wraps in
// script, so a test command is recognised the way it is in a plain shell line.
func uiCodexCommandText(body string) string {
	return strings.NewReplacer(`"`, " ", `'`, " ", "(", " ", ")", " ", "{", " ", "}", " ", "`", " ").Replace(body)
}

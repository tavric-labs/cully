package cully

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Savings answers "does Cully cost more tokens than it saves?" from Claude Code
// transcripts. Cost is measured: bytes Cully injected into the conversation.
// Benefit is observational, not causal: sessions where Cully returned prior work
// are compared with sessions where it did not, by how much the agent read and
// searched before its first edit. Only counts are kept; no transcript text is
// stored or printed.
type Savings struct {
	Sessions, WithHit, WithMiss, WithoutCully int
	Calls, EmptyContext                       int
	// Tokens are estimated at four characters each.
	ResultTokens, ArgTokens, SchemaTokens int64
	WastedTokens                          int64 // context lookups that returned nothing
	ExploreHit, ExploreMiss, ExploreNone  int64 // mean pre-edit exploration tokens, by cohort
	EstimatedSaved, NetTokens             int64
	Confidence                            string
	ByTool                                map[string]*ToolCost
}

type ToolCost struct {
	Calls        int
	ResultTokens int64
}

type sessionCost struct {
	cullyCalls, emptyContext, hits int
	result, args, schema, wasted   int64
	explore                        int64 // tokens read or searched before the first edit
	edited                         bool
	byTool                         map[string]*ToolCost
}

func approxTokens(chars int) int64 { return int64(chars+3) / 4 }

var exploreTools = map[string]bool{"Read": true, "Grep": true, "Glob": true, "Bash": true, "WebFetch": true, "WebSearch": true}
var editTools = map[string]bool{"Edit": true, "Write": true, "NotebookEdit": true, "MultiEdit": true}

// emptyCullyResult reports a lookup that returned no prior work.
func emptyCullyResult(text string) bool {
	compact := strings.ReplaceAll(strings.ReplaceAll(text, " ", ""), "\n", "")
	return strings.Contains(compact, `"notes":[]`) || strings.Contains(compact, `"results":[]`) || strings.Contains(compact, `"records":[]`)
}

// svItem is the part of a content block that savings reads; Input stays raw so
// the size of any tool call's arguments can be counted.
type svItem struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	ToolUseID string          `json:"tool_use_id"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
}

func scanSession(r io.Reader) sessionCost {
	sc := sessionCost{byTool: map[string]*ToolCost{}}
	names := map[string]string{} // tool_use id -> name
	reader := bufio.NewReaderSize(r, 1<<20)
	for {
		line, err := reader.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			var e tEntry
			if json.Unmarshal(line, &e) == nil {
				var items []svItem
				if json.Unmarshal(e.Message.Content, &items) == nil {
					for _, it := range items {
						switch it.Type {
						case "tool_use":
							names[it.ID] = it.Name
							if tool := cullyTool(it.Name); tool != "" {
								sc.args += approxTokens(len(it.Input))
							} else if editTools[it.Name] {
								sc.edited = true
							}
						case "tool_result":
							name := names[it.ToolUseID]
							text := toolResultText(it.Content)
							tool := cullyTool(name)
							switch {
							case tool != "":
								tokens := approxTokens(len(text))
								sc.cullyCalls++
								sc.result += tokens
								cost := sc.byTool[tool]
								if cost == nil {
									cost = &ToolCost{}
									sc.byTool[tool] = cost
								}
								cost.Calls++
								cost.ResultTokens += tokens
								if tool == "cully_context" || tool == "cully_search" || tool == "cully_recall" {
									if emptyCullyResult(text) {
										sc.emptyContext++
										sc.wasted += tokens
									} else if strings.Contains(text, `"summary"`) || strings.Contains(text, `"task"`) {
										sc.hits++
									}
								}
							case name == "ToolSearch" && strings.Contains(text, "mcp__cully__"):
								sc.schema += approxTokens(len(text))
							case exploreTools[name] && !sc.edited:
								sc.explore += approxTokens(len(text))
							}
						}
					}
				}
			}
		}
		if err != nil {
			return sc
		}
	}
}

func meanExplore(total int64, n int) int64 {
	if n == 0 {
		return 0
	}
	return total / int64(n)
}

// ComputeSavings aggregates the transcripts under root (a Claude projects
// directory), limited to sessions modified since the cutoff and, when project
// is set, to directories whose name contains it.
func ComputeSavings(root, project string, since time.Time) (Savings, error) {
	var s Savings
	s.ByTool = map[string]*ToolCost{}
	var hitSum, missSum, noneSum int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if project != "" && !strings.Contains(filepath.Base(filepath.Dir(path)), project) {
			return nil
		}
		if info, err := d.Info(); err != nil || info.ModTime().Before(since) {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()
		sc := scanSession(f)
		if !sc.edited {
			return nil // conversation-only sessions say nothing about exploration cost
		}
		s.Sessions++
		s.Calls += sc.cullyCalls
		s.EmptyContext += sc.emptyContext
		s.ResultTokens += sc.result
		s.ArgTokens += sc.args
		s.SchemaTokens += sc.schema
		s.WastedTokens += sc.wasted
		for name, cost := range sc.byTool {
			total := s.ByTool[name]
			if total == nil {
				total = &ToolCost{}
				s.ByTool[name] = total
			}
			total.Calls += cost.Calls
			total.ResultTokens += cost.ResultTokens
		}
		switch {
		case sc.hits > 0:
			s.WithHit++
			hitSum += sc.explore
		case sc.cullyCalls > 0:
			s.WithMiss++
			missSum += sc.explore
		default:
			s.WithoutCully++
			noneSum += sc.explore
		}
		return nil
	})
	if err != nil {
		return s, err
	}
	s.ExploreHit = meanExplore(hitSum, s.WithHit)
	s.ExploreMiss = meanExplore(missSum, s.WithMiss)
	s.ExploreNone = meanExplore(noneSum, s.WithoutCully)
	// Baseline is what comparable sessions spent without a Cully hit. A negative
	// difference is reported as zero benefit rather than as a loss, since the two
	// cohorts are different tasks.
	baseline := s.ExploreNone
	if s.WithoutCully == 0 {
		baseline = s.ExploreMiss
	}
	if per := baseline - s.ExploreHit; per > 0 && s.WithHit > 0 {
		s.EstimatedSaved = per * int64(s.WithHit)
	}
	s.NetTokens = s.EstimatedSaved - (s.ResultTokens + s.ArgTokens + s.SchemaTokens)
	switch {
	case s.WithHit < 5 || s.WithoutCully < 5 && s.WithMiss < 5:
		s.Confidence = "low: too few sessions in a cohort to compare"
	case s.WithHit < 20:
		s.Confidence = "medium: observational comparison, not a controlled test"
	default:
		s.Confidence = "medium-high: large sample, still observational"
	}
	return s, nil
}

// Write prints the report. The cost lines are measured; the saving is an estimate.
func (s Savings) Write(w io.Writer, days int) {
	fmt.Fprintf(w, "CULLY TOKEN IMPACT  (last %d days, %d sessions that edited code)\n\n", days, s.Sessions)
	cost := s.ResultTokens + s.ArgTokens + s.SchemaTokens
	fmt.Fprintf(w, "  Cost (measured, ~4 chars/token)\n")
	fmt.Fprintf(w, "    Cully results injected   %8d tokens in %d calls\n", s.ResultTokens, s.Calls)
	fmt.Fprintf(w, "    Cully tool schemas       %8d tokens\n", s.SchemaTokens)
	fmt.Fprintf(w, "    Cully call arguments     %8d tokens\n", s.ArgTokens)
	fmt.Fprintf(w, "    Total                    %8d tokens\n", cost)
	fmt.Fprintf(w, "    Wasted on empty lookups  %8d tokens in %d calls\n\n", s.WastedTokens, s.EmptyContext)
	tools := make([]string, 0, len(s.ByTool))
	for name := range s.ByTool {
		tools = append(tools, name)
	}
	sort.Slice(tools, func(i, j int) bool { return s.ByTool[tools[i]].ResultTokens > s.ByTool[tools[j]].ResultTokens })
	for _, name := range tools {
		fmt.Fprintf(w, "      %-22s %4d calls  %7d tokens\n", name, s.ByTool[name].Calls, s.ByTool[name].ResultTokens)
	}
	fmt.Fprintf(w, "\n  Benefit (estimated: tokens read/searched before the first edit)\n")
	fmt.Fprintf(w, "    Sessions with Cully hit    %4d  mean %7d\n", s.WithHit, s.ExploreHit)
	fmt.Fprintf(w, "    Cully called, no hit       %4d  mean %7d\n", s.WithMiss, s.ExploreMiss)
	fmt.Fprintf(w, "    Without Cully              %4d  mean %7d\n", s.WithoutCully, s.ExploreNone)
	fmt.Fprintf(w, "    Estimated saved            %8d tokens\n\n", s.EstimatedSaved)
	verdict := "Cully is saving tokens"
	if s.NetTokens < 0 {
		verdict = "Cully is costing more than it saves"
	}
	fmt.Fprintf(w, "  Net %+d tokens: %s.\n  Confidence: %s.\n", s.NetTokens, verdict, s.Confidence)
	if s.EmptyContext > 0 && s.WastedTokens*4 > cost {
		fmt.Fprintf(w, "  Hint: empty lookups are over a quarter of the cost; skip cully_context on tasks with no prior work.\n")
	}
}

// RunSavings implements `cully savings [--days N] [--project NAME] [--json]`.
func RunSavings(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("savings", flag.ContinueOnError)
	days := fs.Int("days", 14, "look back this many days")
	project := fs.String("project", "", "only transcript folders containing this text")
	asJSON := fs.Bool("json", false, "print JSON")
	root := fs.String("dir", "", "Claude projects directory (default ~/.claude/projects)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*root = filepath.Join(home, ".claude", "projects")
	}
	s, err := ComputeSavings(*root, *project, time.Now().AddDate(0, 0, -*days))
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(s)
	}
	s.Write(w, *days)
	return nil
}

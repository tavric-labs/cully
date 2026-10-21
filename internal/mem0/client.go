// Package mem0 talks to a separately deployed, self-hosted Mem0 REST server.
package mem0

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/mcp-runtime/cully/internal/memory"
)

type Client struct {
	BaseURL string
	APIKey  string
	HTTP    *http.Client
}
type Hit struct {
	ID       string         `json:"id"`
	RunID    string         `json:"run_id"`
	UserID   string         `json:"user_id"`
	Metadata map[string]any `json:"metadata"`
}
type hits struct {
	Results []Hit `json:"results"`
}

func New(base, key string) (*Client, error) {
	u, e := url.Parse(base)
	if e != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || key == "" {
		return nil, fmt.Errorf("Mem0 requires a valid server URL and API key")
	}
	return &Client{BaseURL: strings.TrimRight(base, "/"), APIKey: key, HTTP: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func UserID(owner string) string {
	h := sha256.Sum256([]byte(owner))
	return "cully-" + hex.EncodeToString(h[:])
}
func (c *Client) request(ctx context.Context, method, path string, body any, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.APIKey)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return memory.ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return memory.ErrUnavailable
	}
	if out == nil {
		_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return err
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(out)
}

// Reconcile replaces one source projection. infer=false avoids a model call
// and preserves exact attribution; Mem0 still embeds for semantic recall.
func (c *Client) Reconcile(ctx context.Context, owner, id string, e *memory.Entry) error {
	uid := UserID(owner)
	params := url.Values{"user_id": {uid}, "run_id": {id}, "top_k": {"1000"}}
	var old hits
	if err := c.request(ctx, "GET", "/memories?"+params.Encode(), nil, &old); err != nil {
		return err
	}
	for _, h := range old.Results {
		if h.UserID != uid || h.RunID != id {
			return memory.ErrUnavailable
		}
		if err := c.request(ctx, "DELETE", "/memories/"+url.PathEscape(h.ID), nil, nil); err != nil {
			return err
		}
	}
	if e == nil {
		return nil
	}
	parts := []string{e.Summary}
	for _, v := range []*string{e.Approach, e.Outcome, e.Issue, e.Learning, e.NextSteps} {
		if v != nil && *v != "" {
			parts = append(parts, *v)
		}
	}
	meta := map[string]any{"cully_entry_id": id, "cully_updated_at": e.UpdatedAt.Format(time.RFC3339Nano), "assistant": e.Assistant, "entry_type": e.EntryType}
	if e.SessionRef != nil {
		meta["session_ref"] = *e.SessionRef
	}
	if e.AgentSessionID != nil {
		meta["agent_session_id"] = *e.AgentSessionID
	}
	body := map[string]any{"messages": []map[string]string{{"role": "user", "content": strings.Join(parts, "\n")}}, "user_id": uid, "run_id": id, "infer": false, "metadata": meta}
	return c.request(ctx, "POST", "/memories", body, nil)
}
func (c *Client) Search(ctx context.Context, owner string, input memory.SearchInput) ([]Hit, error) {
	filters := map[string]any{"user_id": UserID(owner)}
	if input.SessionRef != nil {
		filters["session_ref"] = *input.SessionRef
	}
	if input.EntryType != nil {
		filters["entry_type"] = *input.EntryType
	}
	var out hits
	err := c.request(ctx, "POST", "/search", map[string]any{"query": input.Query, "filters": filters, "top_k": input.Limit}, &out)
	return out.Results, err
}

// Package mcp is the list's tools for its session: an MCP server on stdin and
// stdout, run by the session's harness as `foray mcp --list home`.
//
// Every tool is one call to the Foray service on loopback, naming the session
// it works for, so what the session changes is recorded as the session's.
package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is the service, as one session sees one list.
type Client struct {
	Base    string // http://127.0.0.1:7389
	List    string
	Session string
	HTTP    *http.Client
}

func (c Client) do(method, path string, body any) ([]byte, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+"/v1/lists/"+url.PathEscape(c.List)+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Foray-As", c.Session)
	req.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the Foray service: %w", err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		var e struct{ Error string }
		json.Unmarshal(out, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return nil, fmt.Errorf("%s", e.Error)
	}
	return out, nil
}

// Args are every tool's arguments.
type Args struct {
	ID       string   `json:"id"`
	States   string   `json:"states"`
	Title    *string  `json:"title"`
	Notes    *string  `json:"notes"`
	State    *string  `json:"state"`
	Due      *string  `json:"due"`
	Who      *string  `json:"who"`
	Tags     []string `json:"tags"`
	Text     string   `json:"text"`
	Task     string   `json:"task"`
	Worker   string   `json:"worker"`
	Kind     string   `json:"kind"`
	Ask      string   `json:"ask"`
	Answer   string   `json:"answer"`
	Decision string   `json:"decision"`
	Into     string   `json:"into"`
}

type item struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Notes       string   `json:"notes"`
	State       string   `json:"state"`
	Due         string   `json:"due"`
	Who         string   `json:"who"`
	Tags        []string `json:"tags"`
	AddedBy     string   `json:"added_by"`
	SuggestedBy string   `json:"suggested_by"`
	Comments    []struct {
		By, Text string
	} `json:"comments"`
	Help []struct {
		Task, Worker, Kind, State, Answer string
	} `json:"help"`
}

func line(it item) string {
	s := it.ID + " · " + it.State
	if it.Due != "" {
		s += " · due " + it.Due
	}
	s += " · " + it.Title
	if it.Who != "" {
		s += " (" + it.Who + ")"
	}
	if len(it.Tags) > 0 {
		s += " [" + strings.Join(it.Tags, ", ") + "]"
	}
	if it.SuggestedBy != "" {
		s += " — suggested by " + it.SuggestedBy
	}
	return s
}

// Call runs one tool.
func (c Client) Call(tool string, a Args) (string, error) {
	switch tool {
	case "list_items":
		q := ""
		if a.States != "" {
			q = "?states=" + url.QueryEscape(a.States)
		}
		b, err := c.do("GET", q, nil)
		if err != nil {
			return "", err
		}
		var l struct{ Items []item }
		json.Unmarshal(b, &l)
		if len(l.Items) == 0 {
			return "no items", nil
		}
		out := make([]string, len(l.Items))
		for i, it := range l.Items {
			out[i] = line(it)
		}
		return strings.Join(out, "\n"), nil
	case "get_item":
		b, err := c.do("GET", "", nil)
		if err != nil {
			return "", err
		}
		var l struct{ Items []json.RawMessage }
		json.Unmarshal(b, &l)
		for _, raw := range l.Items {
			var it item
			json.Unmarshal(raw, &it)
			if it.ID == a.ID {
				var pretty bytes.Buffer
				json.Indent(&pretty, raw, "", "  ")
				return pretty.String(), nil
			}
		}
		return "", fmt.Errorf("no item %q", a.ID)
	case "add_item":
		n := map[string]any{"tags": a.Tags}
		for k, v := range map[string]*string{"title": a.Title, "notes": a.Notes, "due": a.Due, "who": a.Who, "state": a.State} {
			if v != nil {
				n[k] = *v
			}
		}
		return c.item("POST", "/items", n)
	case "update_item":
		p := map[string]any{}
		for k, v := range map[string]*string{"title": a.Title, "notes": a.Notes, "due": a.Due, "who": a.Who, "state": a.State} {
			if v != nil {
				p[k] = *v
			}
		}
		if a.Tags != nil {
			p["tags"] = a.Tags
		}
		return c.item("PATCH", "/items/"+url.PathEscape(a.ID), p)
	case "comment":
		return c.item("POST", "/items/"+url.PathEscape(a.ID)+"/comments", map[string]string{"text": a.Text})
	case "record_help":
		return c.item("POST", "/items/"+url.PathEscape(a.ID)+"/help", map[string]string{
			"task": a.Task, "worker": a.Worker, "kind": a.Kind, "state": a.stateOr(), "ask": a.Ask, "answer": a.Answer})
	case "decide_suggestion":
		return c.decide(a)
	}
	return "", fmt.Errorf("no tool %q", tool)
}

func (a Args) stateOr() string {
	if a.State != nil {
		return *a.State
	}
	return ""
}

func (c Client) item(method, path string, body any) (string, error) {
	b, err := c.do(method, path, body)
	if err != nil {
		return "", err
	}
	var it item
	json.Unmarshal(b, &it)
	return line(it), nil
}

// decide is the session's answer to a suggestion: into the inbox for a
// person, folded into an item it repeats, or dropped.
func (c Client) decide(a Args) (string, error) {
	state := func(s string) map[string]any { return map[string]any{"state": s} }
	switch a.Decision {
	case "accept":
		return c.item("PATCH", "/items/"+url.PathEscape(a.ID), state("inbox"))
	case "drop":
		if strings.TrimSpace(a.Text) != "" {
			if _, err := c.item("POST", "/items/"+url.PathEscape(a.ID)+"/comments", map[string]string{"text": a.Text}); err != nil {
				return "", err
			}
		}
		return c.item("PATCH", "/items/"+url.PathEscape(a.ID), state("dropped"))
	case "merge":
		if a.Into == "" {
			return "", fmt.Errorf("merge needs into: the item it repeats")
		}
		b, err := c.do("GET", "", nil)
		if err != nil {
			return "", err
		}
		var l struct{ Items []item }
		json.Unmarshal(b, &l)
		var sug *item
		for i := range l.Items {
			if l.Items[i].ID == a.ID {
				sug = &l.Items[i]
			}
		}
		if sug == nil {
			return "", fmt.Errorf("no item %q", a.ID)
		}
		note := fmt.Sprintf("%s suggested: %s", sug.SuggestedBy, sug.Title)
		if sug.Notes != "" {
			note += "\n" + sug.Notes
		}
		if _, err := c.item("POST", "/items/"+url.PathEscape(a.Into)+"/comments", map[string]string{"text": note}); err != nil {
			return "", err
		}
		if _, err := c.item("POST", "/items/"+url.PathEscape(a.ID)+"/comments",
			map[string]string{"text": "folded into " + a.Into}); err != nil {
			return "", err
		}
		return c.item("PATCH", "/items/"+url.PathEscape(a.ID), state("dropped"))
	}
	return "", fmt.Errorf("decision is accept, merge or drop")
}

// Tools is what the session is offered.
var Tools = []map[string]any{
	tool("list_items", "The list's items, one line each: id · state · due · title (who) [tags]. States: suggested (an agent's suggestion you have to decide on), inbox, open, waiting, done, dropped.",
		props{"states": str("comma-separated states to show; all when empty")}),
	tool("get_item", "One item in full: notes, comments, help requests and their answers.", props{"id": str("the item")}, "id"),
	tool("add_item", "Add an item yourself (one the people asked you for in conversation). Agents' suggestions arrive on their own, as state suggested.",
		props{"title": str(""), "notes": str(""), "due": str("2006-01-02, or 2006-01-02T15:04:05+02:00"), "who": str("who it is for"),
			"tags": arr("tags"), "state": str("open by default")}, "title"),
	tool("update_item", "Change an item: title, notes, state, due, who, tags. Only the fields given change.",
		props{"id": str(""), "title": str(""), "notes": str(""), "state": str(""), "due": str(""), "who": str(""), "tags": arr("")}, "id"),
	tool("comment", "Say something on an item — a question for the people, or what you found. They see it in the app and their answers come back to you.",
		props{"id": str(""), "text": str("")}, "id", "text"),
	tool("record_help", "Record help asked of another agent for an item, and its progress: call it when you send the task (state working) and again with the answer.",
		props{"id": str("the item"), "task": str("the task id ask_agent returned"), "worker": str("MACHINE/SESSION"),
			"kind": str("find out, compare, draft, plan"), "state": str("working, completed, failed"), "ask": str("what you asked"), "answer": str("what came back, short")},
		"id", "task"),
	tool("decide_suggestion", "Decide on an agent's suggestion: accept (into the inbox for a person), merge (into the item it repeats, given as into), or drop (with text saying why).",
		props{"id": str("the suggestion"), "decision": str("accept, merge or drop"), "into": str("for merge"), "text": str("for drop: why")}, "id", "decision"),
}

type props map[string]any

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func arr(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}
func tool(name, desc string, p props, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": map[string]any(p)}
	if len(required) > 0 {
		schema["required"] = required
	}
	return map[string]any{"name": name, "description": desc, "inputSchema": schema}
}

// Instructions are what the session reads first about these tools.
const Instructions = "This is a household's todo list, and you look after it. People add items in the Foray app; you are told, and triage: due dates, who, tags, a question back if something is unclear. When an item wants it — or a person asks — get help from other agents (list_agents, ask_agent), record it with record_help, and put the answer on the item. Agents' suggestions arrive as state suggested: decide on each. Never buy, book or send anything yourself: prepare it, and a person acts."

// Serve answers MCP requests, one JSON-RPC message per line, until in ends.
func Serve(in io.Reader, out io.Writer, c Client) error {
	enc := json.NewEncoder(out)
	reply := func(id json.RawMessage, result any, code int, msg string) {
		r := map[string]any{"jsonrpc": "2.0", "id": id}
		if msg != "" {
			r["error"] = map[string]any{"code": code, "message": msg}
		} else {
			r["result"] = result
		}
		enc.Encode(r)
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if json.Unmarshal(sc.Bytes(), &req) != nil {
			reply(nil, nil, -32700, "not JSON")
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			json.Unmarshal(req.Params, &p)
			if p.ProtocolVersion == "" {
				p.ProtocolVersion = "2025-06-18"
			}
			reply(req.ID, map[string]any{"protocolVersion": p.ProtocolVersion,
				"capabilities": map[string]any{"tools": map[string]any{}},
				"serverInfo":   map[string]any{"name": "foray", "version": "1"},
				"instructions": Instructions}, 0, "")
		case "ping":
			reply(req.ID, map[string]any{}, 0, "")
		case "tools/list":
			reply(req.ID, map[string]any{"tools": Tools}, 0, "")
		case "tools/call":
			var p struct {
				Name      string `json:"name"`
				Arguments Args   `json:"arguments"`
			}
			json.Unmarshal(req.Params, &p)
			text, err := c.Call(p.Name, p.Arguments)
			if err != nil {
				text = err.Error()
			}
			reply(req.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}, "isError": err != nil}, 0, "")
		default:
			reply(req.ID, nil, -32601, "no method "+req.Method)
		}
	}
	return sc.Err()
}

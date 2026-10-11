package mcp

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vpavlin/foray/internal/server"
	"github.com/vpavlin/foray/internal/store"
)

// The tools against a real service on loopback, as the session uses them.
func TestTheSessionWorksTheList(t *testing.T) {
	dir := t.TempDir()
	cfg := &server.Config{Data: dir, Lists: []server.ListConfig{{Name: "home", Mesh: "home", Session: "foray-home",
		SuggestFrom: []string{"laptop/shrooms"}}}}
	path := filepath.Join(dir, "config.json")
	cfg.Save(path)
	srv, err := server.New(cfg, path, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	srv.Self = func() string { return "jimmy-crib" }
	srv.Who = func(netip.Addr) string { return "laptop.home" }
	local := httptest.NewUnstartedServer(srv.Handler())
	local.Config.ConnContext = server.ConnContext("", true)
	local.Start()
	defer local.Close()
	mesh := httptest.NewUnstartedServer(srv.Handler())
	mesh.Config.ConnContext = server.ConnContext("home", false)
	mesh.Start()
	defer mesh.Close()

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go Serve(inR, outW, Client{Base: local.URL, List: "home", Session: "foray-home"})
	dec := bufio.NewScanner(outR)
	n := 0
	call := func(tool string, args map[string]any) (string, bool) {
		t.Helper()
		n++
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": n, "method": "tools/call",
			"params": map[string]any{"name": tool, "arguments": args}})
		inW.Write(append(b, '\n'))
		if !dec.Scan() {
			t.Fatal("no reply")
		}
		var r struct {
			Result struct {
				Content []struct{ Text string }
				IsError bool
			}
		}
		json.Unmarshal(dec.Bytes(), &r)
		return r.Result.Content[0].Text, r.Result.IsError
	}

	out, isErr := call("add_item", map[string]any{"title": "tire change", "due": "2026-11-03", "tags": []string{"car"}})
	if isErr || !strings.Contains(out, "· open · due 2026-11-03 · tire change [car]") {
		t.Fatalf("add_item: %s", out)
	}
	id := strings.Fields(out)[0]

	// A suggestion arrives the way an agent sends one, and the session folds
	// it into the item it repeats.
	req, _ := http.NewRequest("POST", mesh.URL+"/v1/lists/home/suggest", strings.NewReader(`{"title":"winter tires on","why":"first frost next week"}`))
	req.Header.Set(server.FromSessionHeader, "shrooms")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("suggest: %v %v", err, resp.Status)
	}
	var sug store.Item
	json.NewDecoder(resp.Body).Decode(&sug)
	resp.Body.Close()

	out, _ = call("list_items", map[string]any{"states": "suggested"})
	if !strings.Contains(out, "suggested by laptop/shrooms") {
		t.Fatalf("the session does not see the suggestion: %s", out)
	}
	if out, isErr = call("decide_suggestion", map[string]any{"id": sug.ID, "decision": "merge", "into": id}); isErr {
		t.Fatalf("merge: %s", out)
	}
	full, _ := call("get_item", map[string]any{"id": id})
	if !strings.Contains(full, "laptop/shrooms suggested: winter tires on") || !strings.Contains(full, "first frost") {
		t.Fatalf("the suggestion was not folded in: %s", full)
	}
	out, _ = call("list_items", map[string]any{"states": "suggested,inbox"})
	if out != "no items" {
		t.Fatalf("the merged suggestion is still pending: %s", out)
	}

	// Help, as the session records it, is the session's.
	if out, isErr = call("record_help", map[string]any{"id": id, "task": "k11/research:7", "worker": "k11/research",
		"kind": "find out", "state": "working", "ask": "garages open Saturday"}); isErr {
		t.Fatalf("record_help: %s", out)
	}
	full, _ = call("get_item", map[string]any{"id": id})
	if !strings.Contains(full, `"worker": "k11/research"`) || !strings.Contains(full, `"state": "working"`) {
		t.Fatalf("help not recorded: %s", full)
	}

	if out, isErr = call("decide_suggestion", map[string]any{"id": id, "decision": "maybe"}); !isErr {
		t.Fatalf("a nonsense decision was taken: %s", out)
	}
	inW.Close()

	// Another session on the machine is offered the tools and cannot use them.
	r2, w2 := io.Pipe()
	o2r, o2w := io.Pipe()
	go Serve(r2, o2w, Client{Base: local.URL, List: "home", Session: "webapp"})
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "list_items", "arguments": map[string]any{}}})
	w2.Write(append(b, '\n'))
	sc := bufio.NewScanner(o2r)
	sc.Scan()
	if !strings.Contains(sc.Text(), "looked after by") || !strings.Contains(sc.Text(), `"isError":true`) {
		t.Fatalf("another session used the list: %s", sc.Text())
	}
	w2.Close()
}

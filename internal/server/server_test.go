package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vpavlin/foray/internal/store"
)

type fixture struct {
	s        *Server
	home     *httptest.Server // the home mesh's address
	office   *httptest.Server // the office mesh's address
	local    *httptest.Server // loopback
	cfgPath  string
	mu       sync.Mutex
	notified []string
	callerOf string // what the mesh calls whoever connects
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	cfg := &Config{Data: dir, Lists: []ListConfig{{Name: "home", Title: "Home", Mesh: "home", Session: "foray-home",
		SuggestFrom: []string{"laptop/shrooms"}}}}
	f := &fixture{cfgPath: filepath.Join(dir, "config.json"), callerOf: "phone.home"}
	if err := cfg.Save(f.cfgPath); err != nil {
		t.Fatal(err)
	}
	s, err := New(cfg, f.cfgPath, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	s.Who = func(netip.Addr) string { f.mu.Lock(); defer f.mu.Unlock(); return f.callerOf }
	s.Self = func() string { return "jimmy-crib" }
	s.Notify = func(l ListConfig, text string) { f.mu.Lock(); f.notified = append(f.notified, text); f.mu.Unlock() }
	f.s = s
	start := func(mesh string, local bool) *httptest.Server {
		ts := httptest.NewUnstartedServer(s.Handler())
		ts.Config.ConnContext = ConnContext(mesh, local)
		ts.Start()
		t.Cleanup(ts.Close)
		return ts
	}
	f.home, f.office, f.local = start("home", false), start("office", false), start("", true)
	return f
}

func (f *fixture) call(t *testing.T, ts *httptest.Server, method, path string, body any, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, ts.URL+path, r)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (f *fixture) told() []string {
	// Notifications go out on their own goroutine.
	for i := 0; i < 50; i++ {
		f.mu.Lock()
		n := len(f.notified)
		f.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.notified
	f.notified = nil
	return out
}

// A list exists on its own mesh and on this machine, and nowhere else.
func TestAListIsOnlyOnItsMesh(t *testing.T) {
	f := newFixture(t)
	if code, out := f.call(t, f.home, "GET", "/v1/lists", nil, nil); code != 200 || len(out["lists"].([]any)) != 1 {
		t.Fatalf("home mesh: %d %v", code, out)
	}
	if _, out := f.call(t, f.office, "GET", "/v1/lists", nil, nil); len(out["lists"].([]any)) != 0 {
		t.Fatalf("the office mesh sees the home list: %v", out)
	}
	if code, _ := f.call(t, f.office, "GET", "/v1/lists/home", nil, nil); code != 404 {
		t.Fatalf("the office mesh opened the home list: %d", code)
	}
	if code, _ := f.call(t, f.office, "POST", "/v1/lists/home/items", store.New{Title: "x"}, nil); code != 404 {
		t.Fatalf("the office mesh wrote to the home list: %d", code)
	}
	if code, _ := f.call(t, f.local, "GET", "/v1/lists/home", nil, nil); code != 200 {
		t.Fatalf("this machine cannot open its own list: %d", code)
	}
}

func TestPeopleAddAndTheSessionIsTold(t *testing.T) {
	f := newFixture(t)
	code, it := f.call(t, f.home, "POST", "/v1/lists/home/items", store.New{Title: "buy winter tires"}, nil)
	if code != 201 || it["state"] != "open" || it["added_by"] != "phone.home" {
		t.Fatalf("add: %d %v", code, it)
	}
	if told := f.told(); len(told) != 1 || !strings.Contains(told[0], "phone.home added \"buy winter tires\"") {
		t.Fatalf("the session was told: %v", told)
	}
	// The session's own changes are not news to it.
	id := it["id"].(string)
	f.call(t, f.local, "POST", "/v1/lists/home/items/"+id+"/comments", map[string]string{"text": "Which car?"},
		map[string]string{AsHeader: "foray-home"})
	time.Sleep(50 * time.Millisecond)
	if told := f.told(); len(told) != 0 {
		t.Fatalf("the session was told about itself: %v", told)
	}
	// A person's answer is.
	f.call(t, f.home, "POST", "/v1/lists/home/items/"+id+"/comments", map[string]string{"text": "the Octavia"}, nil)
	if told := f.told(); len(told) != 1 || !strings.Contains(told[0], "the Octavia") {
		t.Fatalf("an answer did not reach the session: %v", told)
	}
}

// A caller the mesh cannot name changes nothing.
func TestAnUnnamedCallerCannotWrite(t *testing.T) {
	f := newFixture(t)
	f.callerOf = ""
	if code, _ := f.call(t, f.home, "POST", "/v1/lists/home/items", store.New{Title: "x"}, nil); code != 403 {
		t.Fatalf("an unnamed caller wrote: %d", code)
	}
	// And the loopback header means nothing from the mesh.
	if code, _ := f.call(t, f.home, "POST", "/v1/lists/home/items", store.New{Title: "x"}, map[string]string{AsHeader: "foray-home"}); code != 403 {
		t.Fatalf("a mesh caller named itself: %d", code)
	}
}

// Only /suggest says who suggested something, and only for senders allowed.
func TestSuggestionsAreGated(t *testing.T) {
	f := newFixture(t)
	f.callerOf = "k11.home"
	code, _ := f.call(t, f.home, "POST", "/v1/lists/home/suggest", map[string]string{"title": "renew the domain"},
		map[string]string{FromSessionHeader: "research"})
	if code != 403 {
		t.Fatalf("a sender not on the list suggested: %d", code)
	}
	_, l := f.call(t, f.local, "GET", "/v1/lists/home", nil, nil)
	if asks := l["asks"].([]any); len(asks) != 1 || asks[0].(map[string]any)["from"] != "k11/research" {
		t.Fatalf("the refused sender was not kept for the app: %v", l["asks"])
	}

	f.callerOf = "laptop.home"
	code, it := f.call(t, f.home, "POST", "/v1/lists/home/suggest", map[string]string{"title": "renew the domain", "why": "it lapses on the 20th"},
		map[string]string{FromSessionHeader: "shrooms"})
	if code != 201 || it["state"] != "suggested" || it["suggested_by"] != "laptop/shrooms" || !strings.Contains(it["notes"].(string), "lapses") {
		t.Fatalf("an allowed suggestion: %d %v", code, it)
	}
	if told := f.told(); len(told) != 1 || !strings.Contains(told[0], "laptop/shrooms suggests") {
		t.Fatalf("the session was not asked to decide: %v", told)
	}
	// Adding as a person cannot claim a suggester.
	_, it = f.call(t, f.home, "POST", "/v1/lists/home/items", map[string]string{"title": "x", "suggested_by": "laptop/shrooms"}, nil)
	if it["suggested_by"] != nil || it["state"] != "open" {
		t.Fatalf("a person's add claimed a suggester: %v", it)
	}
}

// Allowing a sender from the app is kept in the config.
func TestAllowingIsKept(t *testing.T) {
	f := newFixture(t)
	if code, _ := f.call(t, f.home, "POST", "/v1/lists/home/allow", map[string]string{"from": "k11/*"}, nil); code != 200 {
		t.Fatalf("allow: %d", code)
	}
	cfg, err := LoadConfig(f.cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Lists[0].SuggestFrom; len(got) != 2 || got[1] != "k11/*" {
		t.Fatalf("not kept: %v", got)
	}
	f.callerOf = "k11.home"
	if code, _ := f.call(t, f.home, "POST", "/v1/lists/home/suggest", map[string]string{"title": "y"},
		map[string]string{FromSessionHeader: "research"}); code != 201 {
		t.Fatalf("an allowed machine was refused: %d", code)
	}
}

// Help is recorded by the session's tools, on this machine only.
func TestHelpIsRecordedLocally(t *testing.T) {
	f := newFixture(t)
	_, it := f.call(t, f.home, "POST", "/v1/lists/home/items", store.New{Title: "tire change"}, nil)
	id := it["id"].(string)
	h := store.Help{Task: "k11/research:1", Worker: "k11/research", Kind: "find out", State: "working"}
	if code, _ := f.call(t, f.home, "POST", "/v1/lists/home/items/"+id+"/help", h, nil); code != 403 {
		t.Fatalf("help recorded from the mesh: %d", code)
	}
	if code, out := f.call(t, f.local, "POST", "/v1/lists/home/items/"+id+"/help", h, map[string]string{AsHeader: "foray-home"}); code != 200 || len(out["help"].([]any)) != 1 {
		t.Fatalf("help from the session: %d %v", code, out)
	}
	// A person asking for help goes to the session.
	f.told()
	f.call(t, f.home, "POST", "/v1/lists/home/items/"+id+"/ask", map[string]string{"kind": "find out", "text": "garages open Saturday"}, nil)
	if told := f.told(); len(told) != 1 || !strings.Contains(told[0], "asks for find out: garages open Saturday") {
		t.Fatalf("the ask did not reach the session: %v", told)
	}
}

func TestWhoAmIAndAReset(t *testing.T) {
	f := newFixture(t)
	_, me := f.call(t, f.home, "GET", "/v1/whoami", nil, nil)
	if me["name"] != "phone.home" || me["mesh"] != "home" || me["can_write"] != true {
		t.Fatalf("whoami: %v", me)
	}
	f.callerOf = ""
	if _, me = f.call(t, f.home, "GET", "/v1/whoami", nil, nil); me["can_write"] != false {
		t.Fatalf("an unknown caller can write: %v", me)
	}
	f.callerOf = "phone.home"
	f.call(t, f.home, "POST", "/v1/lists/home/items", store.New{Title: "a"}, nil)
	_, ch := f.call(t, f.home, "GET", "/v1/lists/home/changes?since=0", nil, nil)
	if ch["reset"] != nil || len(ch["events"].([]any)) != 1 {
		t.Fatalf("changes from 0: %v", ch)
	}
	// A since this list never reached: the caller must read it all again.
	if _, ch = f.call(t, f.home, "GET", "/v1/lists/home/changes?since=99", nil, nil); ch["reset"] != true {
		t.Fatalf("a since ahead of the list did not reset: %v", ch)
	}
	// The session is always said, empty or not.
	_, ls := f.call(t, f.home, "GET", "/v1/lists", nil, nil)
	if _, ok := ls["lists"].([]any)[0].(map[string]any)["session"]; !ok {
		t.Fatal("session missing from the list")
	}
}

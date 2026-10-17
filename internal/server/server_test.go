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

func TestPairingLink(t *testing.T) {
	f := newFixture(t)
	if code, _ := f.call(t, f.home, "GET", "/v1/lists/home/pair", nil, nil); code != 409 {
		t.Fatalf("a pairing link without an address: %d", code)
	}
	f.s.Addr = func(string) (netip.Addr, bool) { return netip.MustParseAddr("fd7b::1"), true }
	_, p := f.call(t, f.home, "GET", "/v1/lists/home/pair", nil, nil)
	if p["link"] != "foray://add?address=fd7b::1&list=home" || len(p["qr"].([]any)) < 21 {
		t.Fatalf("pair: %v", p["link"])
	}
}

// People set the household's context and the session is told; the session
// reads it and cannot change it.
func TestTheContextIsThePeoples(t *testing.T) {
	f := newFixture(t)
	code, c := f.call(t, f.home, "PUT", "/v1/lists/home/context", store.Context{Place: "Prague", Currency: "CZK"}, nil)
	if code != 200 || c["place"] != "Prague" || c["by"] != "phone.home" {
		t.Fatalf("set: %d %v", code, c)
	}
	if told := f.told(); len(told) != 1 || !strings.Contains(told[0], "get_context") {
		t.Fatalf("the session was told: %v", told)
	}
	if code, _ := f.call(t, f.local, "PUT", "/v1/lists/home/context", store.Context{Place: "Rome"},
		map[string]string{AsHeader: "foray-home"}); code != 403 {
		t.Fatalf("the session changed the context: %d", code)
	}
	if code, c := f.call(t, f.local, "GET", "/v1/lists/home/context", nil, map[string]string{AsHeader: "foray-home"}); code != 200 || c["place"] != "Prague" {
		t.Fatalf("the session reads: %d %v", code, c)
	}
	if _, l := f.call(t, f.home, "GET", "/v1/lists/home", nil, nil); l["context"].(map[string]any)["currency"] != "CZK" {
		t.Fatalf("the list does not carry its context: %v", l["context"])
	}
	if code, _ := f.call(t, f.office, "PUT", "/v1/lists/home/context", store.Context{Place: "x"}, nil); code != 404 {
		t.Fatalf("another mesh changed the context: %d", code)
	}
}

// "analyze" leaves the kind of help to the session.
func TestAnalyzeLeavesItToTheSession(t *testing.T) {
	f := newFixture(t)
	_, it := f.call(t, f.home, "POST", "/v1/lists/home/items", store.New{Title: "Makerfair Brno"}, nil)
	f.told()
	id := it["id"].(string)
	if code, _ := f.call(t, f.home, "POST", "/v1/lists/home/items/"+id+"/ask", map[string]string{"kind": "analyze"}, nil); code != 200 {
		t.Fatalf("analyze: %d", code)
	}
	if told := f.told(); len(told) != 1 || !strings.Contains(told[0], "Work out what would help") {
		t.Fatalf("the session was told: %v", told)
	}
	f.call(t, f.home, "POST", "/v1/lists/home/items/"+id+"/ask", map[string]string{"kind": "find out"}, nil)
	if told := f.told(); len(told) != 1 || !strings.Contains(told[0], "look it up yourself or ask the helper that fits") {
		t.Fatalf("find out: %v", told)
	}
}

// A person editing the summary is news to the session; the session's own
// summary is not.
func TestASummaryEditReachesTheSession(t *testing.T) {
	f := newFixture(t)
	_, it := f.call(t, f.home, "POST", "/v1/lists/home/items", store.New{Title: "Makerfair"}, nil)
	f.told()
	id := it["id"].(string)
	f.call(t, f.local, "PATCH", "/v1/lists/home/items/"+id, map[string]string{"summary": "**Sat 17 Oct**"},
		map[string]string{AsHeader: "foray-home"})
	time.Sleep(50 * time.Millisecond)
	if told := f.told(); len(told) != 0 {
		t.Fatalf("the session was told about its own summary: %v", told)
	}
	code, got := f.call(t, f.home, "PATCH", "/v1/lists/home/items/"+id, map[string]string{"summary": "**Sun 18 Oct**"}, nil)
	if code != 200 || got["summary_by"] != "phone.home" {
		t.Fatalf("edit: %d %v", code, got)
	}
	if told := f.told(); len(told) != 1 || !strings.Contains(told[0], "edited the summary") {
		t.Fatalf("the session was told: %v", told)
	}
}

// Helpers: adding one lets it take the list's session's tasks, taking it away
// withdraws that unless another list here still has it, and a person can
// ask a helper by name — only one of the list's.
func TestHelpersAreTrustedAndAskedByName(t *testing.T) {
	f := newFixture(t)
	type call struct {
		mesh, agent string
		on          bool
	}
	var calls []call
	f.s.Trust = func(mesh, agent string, on bool) error {
		calls = append(calls, call{mesh, agent, on})
		return nil
	}
	put := func(helpers ...store.Helper) map[string]any {
		code, out := f.call(t, f.home, "PUT", "/v1/lists/home/context", store.Context{Place: "Brno", Helpers: helpers}, nil)
		if code != 200 {
			t.Fatalf("put: %d %v", code, out)
		}
		return out
	}
	out := put(store.Helper{Agent: "jimmy-crib/vpavlin", For: "Logos apps"}, store.Helper{Agent: "duet/duet"})
	if len(calls) != 2 || calls[0] != (call{"home", "jimmy-crib/vpavlin", true}) || out["trust"].(map[string]any)["duet/duet"] != "accepting" {
		t.Fatalf("adding: %v %v", calls, out["trust"])
	}
	calls = nil
	out = put(store.Helper{Agent: "duet/duet"})
	if len(calls) != 1 || calls[0] != (call{"home", "jimmy-crib/vpavlin", false}) {
		t.Fatalf("removing: %v %v", calls, out["trust"])
	}
	calls = nil
	put(store.Helper{Agent: "duet/duet", For: "research"})
	if len(calls) != 0 {
		t.Fatalf("an unchanged helper was set again: %v", calls)
	}
	if code, _ := f.call(t, f.home, "PUT", "/v1/lists/home/context", store.Context{Helpers: []store.Helper{{Agent: "nobody"}}}, nil); code != 400 {
		t.Fatalf("a helper that is not MACHINE/SESSION: %d", code)
	}

	_, it := f.call(t, f.home, "POST", "/v1/lists/home/items", store.New{Title: "demo recordings"}, nil)
	f.told()
	id := it["id"].(string)
	if code, _ := f.call(t, f.home, "POST", "/v1/lists/home/items/"+id+"/ask", map[string]string{"kind": "find out", "to": "pi5/jimmy"}, nil); code != 400 {
		t.Fatalf("asked an agent that is not a helper: %d", code)
	}
	if code, _ := f.call(t, f.home, "POST", "/v1/lists/home/items/"+id+"/ask", map[string]string{"kind": "find out", "to": "duet/duet"}, nil); code != 200 {
		t.Fatalf("asked a helper: %d", code)
	}
	if told := f.told(); len(told) != 1 || !strings.Contains(told[0], "asks duet/duet for find out") || !strings.Contains(told[0], "Ask duet/duet (they chose it)") {
		t.Fatalf("the session was told: %v", told)
	}
}

// An item by its own address: what an agent is handed to work on it.
func TestAnItemHasItsOwnAddress(t *testing.T) {
	f := newFixture(t)
	_, it := f.call(t, f.home, "POST", "/v1/lists/home/items", store.New{Title: "Demo recordings"}, nil)
	id := it["id"].(string)
	code, got := f.call(t, f.home, "GET", "/v1/lists/home/items/"+id, nil, nil)
	if code != 200 || got["title"] != "Demo recordings" || got["api"] == nil || got["list"].(map[string]any)["name"] != "home" {
		t.Fatalf("get: %d %v", code, got)
	}
	if code, _ := f.call(t, f.home, "GET", "/v1/lists/home/items/nope", nil, nil); code != 404 {
		t.Fatalf("a missing item: %d", code)
	}
	if code, _ := f.call(t, f.office, "GET", "/v1/lists/home/items/"+id, nil, nil); code != 404 {
		t.Fatalf("another mesh read the item: %d", code)
	}
}

// A write sent again with the same Idempotency-Key — its answer was lost —
// is applied once and answered as the first was; another writer's same key,
// or no key, is a write of its own (design §7.1).
func TestAWriteSentTwiceIsAppliedOnce(t *testing.T) {
	f := newFixture(t)
	key := map[string]string{"Idempotency-Key": "unsent-abc-1"}
	code, first := f.call(t, f.home, "POST", "/v1/lists/home/items", map[string]any{"title": "Milk"}, key)
	if code != http.StatusCreated && code != http.StatusOK {
		t.Fatalf("first: %d %v", code, first)
	}
	code2, again := f.call(t, f.home, "POST", "/v1/lists/home/items", map[string]any{"title": "Milk"}, key)
	if code2 != code || again["id"] != first["id"] {
		t.Fatalf("again: %d %v, first %d %v", code2, again, code, first)
	}
	count := func() int {
		_, l := f.call(t, f.home, "GET", "/v1/lists/home", nil, nil)
		return len(l["items"].([]any))
	}
	if n := count(); n != 1 {
		t.Fatalf("%d items after a repeat, want 1", n)
	}
	f.mu.Lock()
	f.callerOf = "laptop.home"
	f.mu.Unlock()
	f.call(t, f.home, "POST", "/v1/lists/home/items", map[string]any{"title": "Milk"}, key)
	f.call(t, f.home, "POST", "/v1/lists/home/items", map[string]any{"title": "Milk"}, nil)
	if n := count(); n != 3 {
		t.Fatalf("%d items, want 3: another writer's key and no key are writes of their own", n)
	}
}

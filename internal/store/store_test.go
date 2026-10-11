package store

import (
	"strings"
	"os"
	"path/filepath"
	"testing"
)

func open(t *testing.T) (*List, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "home.jsonl")
	l, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return l, p
}

func TestAListSurvivesARestart(t *testing.T) {
	l, p := open(t)
	a, err := l.Add("phone.home", New{Title: " buy winter tires ", Due: "2026-11-01", Tags: []string{"Car", "car"}})
	if err != nil {
		t.Fatal(err)
	}
	if a.State != Open || a.Title != "buy winter tires" || len(a.Tags) != 1 {
		t.Fatalf("added %+v", a)
	}
	done := Done
	if _, err := l.Update("laptop.home", a.ID, Patch{State: &done}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Comment("foray-home", a.ID, "Booked for Tuesday?"); err != nil {
		t.Fatal(err)
	}

	again, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	it, ok := again.Get(a.ID)
	if !ok || it.State != Done || len(it.Comments) != 1 || it.Comments[0].By != "foray-home" {
		t.Fatalf("after reopening: %+v", it)
	}
	if again.Seq() != 3 {
		t.Fatalf("seq %d, want 3", again.Seq())
	}
}

// An agent's suggestion waits for the session, whatever state it asked for.
func TestASuggestionLandsInTheInbox(t *testing.T) {
	l, _ := open(t)
	it, err := l.Add("foray-home", New{Title: "renew the domain", State: Open, SuggestedBy: "laptop/shrooms"})
	if err != nil {
		t.Fatal(err)
	}
	if it.State != Suggested || it.SuggestedBy != "laptop/shrooms" {
		t.Fatalf("suggestion: %+v", it)
	}
}

func TestBadInputIsRefused(t *testing.T) {
	l, _ := open(t)
	for _, n := range []New{{Title: "  "}, {Title: "x", Due: "next week"}, {Title: "x", State: "someday"}} {
		if _, err := l.Add("me", n); err == nil {
			t.Errorf("%+v was taken", n)
		}
	}
	if _, err := l.Update("me", "nope", Patch{}); err == nil {
		t.Error("an unknown item was updated")
	}
	it, _ := l.Add("me", New{Title: "x", Due: "2026-11-01T09:00:00+01:00"})
	empty := ""
	if _, err := l.Update("me", it.ID, Patch{Title: &empty}); err == nil {
		t.Error("a title was emptied")
	}
}

// A help request is one entry per task, updated as the task moves.
func TestHelpIsOneEntryPerTask(t *testing.T) {
	l, _ := open(t)
	it, _ := l.Add("me", New{Title: "tire change"})
	if _, err := l.SetHelp("foray-home", it.ID, Help{Task: "k11/research:1", Worker: "k11/research", Kind: "find out",
		State: "working", Ask: "garages open Saturday near home"}); err != nil {
		t.Fatal(err)
	}
	got, err := l.SetHelp("foray-home", it.ID, Help{Task: "k11/research:1", State: "completed", Answer: "Pneu Servis, 8–12"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Help) != 1 || got.Help[0].State != "completed" || got.Help[0].Ask == "" || got.Help[0].Worker != "k11/research" {
		t.Fatalf("help: %+v", got.Help)
	}
}

func TestSinceIsWhatChanged(t *testing.T) {
	l, _ := open(t)
	a, _ := l.Add("me", New{Title: "a"})
	_, seq := l.Since(0)
	l.Add("me", New{Title: "b"})
	l.Comment("me", a.ID, "hm")
	ev, now := l.Since(seq)
	if len(ev) != 2 || now != 3 || ev[0].Item.Title != "b" || ev[1].Op != "comment" {
		t.Fatalf("since %d: %d events, seq %d", seq, len(ev), now)
	}
}

// A crash mid-write can leave a torn last line; the list opens without it.
func TestATornLastLineIsDropped(t *testing.T) {
	l, p := open(t)
	l.Add("me", New{Title: "kept"})
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"seq":2,"op":"add","item":{"id":"x`)
	f.Close()
	again, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Items()) != 1 {
		t.Fatal("the torn line was not dropped cleanly")
	}
	// And it is gone from the file, so the next write starts a clean line.
	if _, err := again.Add("me", New{Title: "after"}); err != nil {
		t.Fatal(err)
	}
	third, err := Load(p)
	if err != nil || len(third.Items()) != 2 {
		t.Fatalf("a write after a torn line left the log unreadable: %v", err)
	}
	// And a damaged line in the middle is an error, not a shorter list.
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString("{not json}\n" + `{"seq":9,"op":"add","item":{"id":"y","title":"z","state":"open"}}` + "\n")
	f.Close()
	if _, err := Load(p); err == nil {
		t.Fatal("a damaged line in the middle was skipped")
	}
}

// The context is kept across a restart and never shows up as a change: apps
// apply changes by item, and it has none.
func TestTheContextIsKeptAndIsNotAChange(t *testing.T) {
	l, p := open(t)
	a, _ := l.Add("phone.home", New{Title: "buy milk"})
	if _, err := l.SetContext("phone.home", Context{Place: " Prague ", Currency: "CZK", Notes: "car: Octavia"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.SetContext("phone.home", Context{Notes: string(make([]byte, MaxNotes+1))}); err == nil {
		t.Fatal("notes past the limit were kept")
	}
	l.Comment("phone.home", a.ID, "skimmed")

	again, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c := again.Context()
	if c.Place != "Prague" || c.Currency != "CZK" || c.By != "phone.home" || c.Updated.IsZero() {
		t.Fatalf("context after reopening: %+v", c)
	}
	if ev, _ := again.Since(again.Seq()); ev == nil {
		t.Fatal("nothing new came back as nil, which an app reads as null")
	}
	ev, seq := again.Since(0)
	if seq != 3 || len(ev) != 2 {
		t.Fatalf("since 0: seq %d, %d events, want 3 and 2", seq, len(ev))
	}
	for _, e := range ev {
		if e.Op == "context" {
			t.Fatal("the context came back as a change")
		}
	}
}

// The summary is kept with who changed it last, and only a change moves that.
func TestTheSummaryKnowsWhoWroteIt(t *testing.T) {
	l, _ := open(t)
	a, _ := l.Add("phone.home", New{Title: "Makerfair"})
	sum := "**Sat 17 Oct**, [tickets](https://makerfaire.cz/brno)"
	it, err := l.Update("foray-home", a.ID, Patch{Summary: &sum})
	if err != nil || it.Summary != sum || it.SummaryBy != "foray-home" {
		t.Fatalf("set: %+v %v", it, err)
	}
	title := "Makerfair Brno"
	if it, _ = l.Update("phone.home", a.ID, Patch{Title: &title, Summary: &sum}); it.SummaryBy != "foray-home" {
		t.Fatalf("an unchanged summary changed hands: %+v", it)
	}
	long := strings.Repeat("x", MaxSummary+1)
	if _, err := l.Update("foray-home", a.ID, Patch{Summary: &long}); err == nil {
		t.Fatal("a summary past the limit was kept")
	}
}

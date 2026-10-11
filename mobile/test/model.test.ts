import assert from "node:assert/strict";
import { hasContext, isQuestion, dueBucket, dueLabel, sections, applyChanges, whoLabel, notesFromChanges, dueTodayNotes, baseUrl } from "../src/model.ts";
import type { Item, ListInfo, Change } from "../src/model.ts";

const now = new Date(2026, 9, 11, 14, 0);   // Sun 11 Oct 2026, 14:00 local
assert.equal(dueBucket(undefined, now), "");
assert.equal(dueBucket("2026-10-11", now), "today");
assert.equal(dueBucket("2026-10-10", now), "overdue");
assert.equal(dueBucket("2026-10-12", now), "tomorrow");
assert.equal(dueBucket("2026-10-20", now), "later");
assert.equal(dueBucket(new Date(2026, 9, 11, 9, 0).toISOString(), now), "overdue", "earlier today with a time");
assert.equal(dueBucket("garbage", now), "");
assert.equal(dueLabel("2026-10-11", now), "Today");
assert.equal(dueLabel("2026-10-12", now), "Tomorrow");
assert.equal(dueLabel("2026-10-14", now), "Wed");
assert.equal(dueLabel("2026-11-01", now), "1 Nov");
assert.equal(dueLabel("2027-01-05", now), "5 Jan 2027");
assert.equal(dueLabel(new Date(2026, 9, 12, 9, 5).toISOString(), now), "Tomorrow 09:05");

const it = (id: string, state: Item["state"], extra: Partial<Item> = {}): Item => ({ id, title: id, state, ...extra });
const s = sections([
  it("a", "open"), it("b", "open", { due: "2026-10-20" }), it("c", "open", { due: "2026-10-12" }),
  it("w", "waiting"), it("i", "inbox"), it("d", "done"), it("x", "dropped"), it("s", "suggested"),
]);
assert.deepEqual(s.open.map((x) => x.id), ["c", "b", "a"], "due soonest first, undated last");
assert.deepEqual(s.done.map((x) => x.id).sort(), ["d", "x"]);
assert.deepEqual([s.waiting.length, s.inbox.length, s.suggested.length], [1, 1, 1]);

const items = applyChanges([it("a", "open"), it("b", "open")], [
  { seq: 5, at: "", by: "p", op: "update", item: it("a", "done") },
  { seq: 4, at: "", by: "p", op: "update", item: it("a", "waiting") },
  { seq: 6, at: "", by: "p", op: "add", item: it("n", "open") },
]);
assert.equal(items.find((x) => x.id === "a")!.state, "done", "the latest event wins, whatever the order received");
assert.equal(items.length, 3);

assert.equal(whoLabel("phone.home", "foray-home", "laptop.home"), "phone");
assert.equal(whoLabel("phone.home", "foray-home", "phone.home"), "you");
assert.equal(whoLabel("foray-home", "foray-home", ""), "the assistant");
assert.equal(whoLabel("jimmy-crib/foray-home", "foray-home", ""), "the assistant");
assert.equal(whoLabel("k11/research", "foray-home", ""), "research");

const list: ListInfo = { name: "home", title: "Home", mesh: "home", session: "foray-home", seq: 0 };
const asked = it("t", "open", { title: "tires", comments: [{ by: "phone.home", at: "1", text: "hi" }, { by: "foray-home", at: "2", text: "Which car?" }] });
const helped = it("g", "open", { title: "garage", help: [{ task: "k11/research:7", kind: "find out", state: "completed", answer: "Two are open." }, { task: "k11/research:8", kind: "compare", state: "working" }] });
const ev: Change[] = [
  { seq: 1, at: "", by: "foray-home", op: "comment", item: asked },
  { seq: 2, at: "", by: "foray-home", op: "help", item: helped },
  { seq: 3, at: "", by: "laptop.home", op: "add", item: it("n", "open", { title: "milk" }) },
  { seq: 4, at: "", by: "phone.home", op: "add", item: it("m", "open", { title: "mine" }) },
  { seq: 5, at: "", by: "phone.home", op: "comment", item: it("t2", "open", { comments: [{ by: "phone.home", at: "3", text: "me" }] }) },
];
const notes = notesFromChanges(list, ev, new Set(), "phone.home");
assert.deepEqual(notes.map((n) => n.key), ["q:home:t:2", "h:home:g:k11/research:7", "a:home:n"]);
assert.equal(notes[0].body, "The assistant asks: Which car?");
assert.equal(notes[1].body, "Two are open.");
assert.deepEqual(notesFromChanges(list, ev, new Set(notes.map((n) => n.key)), "phone.home"), [], "never twice");
assert.deepEqual(notesFromChanges({ ...list, session: "" }, ev.slice(0, 1), new Set()), [], "no session: comments are just comments");

const due = dueTodayNotes(list, [it("a", "open", { due: "2026-10-11" }), it("b", "done", { due: "2026-10-11" }), it("c", "open", { due: "2026-10-09" }), it("d", "open", { due: "2026-10-12" })], now, new Set());
assert.deepEqual(due.map((n) => [n.item, n.title]), [["a", "Due today"], ["c", "Overdue"]]);

assert.equal(baseUrl("fdb0:9afc::1", 7389), "http://[fdb0:9afc::1]:7389");
assert.equal(baseUrl("[fdb0::1]:7390", 7389), "http://[fdb0::1]:7390");
assert.equal(baseUrl("jimmy-crib.office.mesh", 7389), "http://jimmy-crib.office.mesh:7389");
assert.equal(baseUrl("http://10.0.0.2:8000/", 7389), "http://10.0.0.2:8000");

assert.equal(hasContext(null), false);
assert.equal(hasContext({ place: "  ", notes: "" , by: "x", updated: "t" }), false, "by/updated alone isn't content");
assert.equal(hasContext({ currency: "CZK" }), true);

assert.equal(isQuestion("Which car?"), true);
assert.equal(isQuestion("**Kdy** to potřebuješ?\n"), true);
assert.equal(isQuestion("Is it the \"blue one?\""), true);
assert.equal(isQuestion("Shrnutí doplněno: dvě garáže mají v sobotu otevřeno."), false);
assert.equal(isQuestion("Why? Because it's cheaper."), false, "a question mark in the middle doesn't make it a question");
const noteEv = [{ seq: 1, at: "", by: "foray-home", op: "comment", item: it("u", "open", { title: "tires", comments: [{ by: "foray-home", at: "9", text: "Shrnutí doplněno." }] }) }];
assert.equal(notesFromChanges(list, noteEv as Change[], new Set())[0].body, "The assistant: Shrnutí doplněno.");

console.log("MODEL OK");

// What the app shows and when it notifies, as pure functions of the service's data (docs/api.md).
// No React, no network: tested in test/model.test.ts.
import { stripMarkdown } from "./md.ts";

export type State = "suggested" | "inbox" | "open" | "waiting" | "done" | "dropped";

export interface Comment { by: string; at: string; text: string; unsent?: boolean }   // unsent: in this phone's outbox, not sent yet
export interface Help { task?: string; worker?: string; kind: string; state: string; ask?: string; answer?: string; updated?: string }
export interface Item {
  id: string; title: string; notes?: string; state: State;
  due?: string; who?: string; tags?: string[];
  added_by?: string; suggested_by?: string; created?: string; updated?: string;
  comments?: Comment[]; help?: Help[]; calendar?: string;
  summary?: string; summary_by?: string;   // what's known about it, in markdown; usually the assistant's
  unsent?: boolean;   // changed on this phone while its computer didn't answer: in the outbox, not sent yet
}
export interface ListInfo {
  name: string; title: string; mesh: string; session?: string; seq: number;
  counts?: Partial<Record<State, number>>;
}
/** What the household tells the assistant about itself (GET/PUT /v1/lists/{list}/context). */
/** An agent the list's assistant may ask for help: MACHINE/SESSION, and what it's good for. */
export interface Helper { agent: string; for?: string }
export interface ListContext {
  place?: string; language?: string; currency?: string; notes?: string; helpers?: Helper[]; by?: string; updated?: string;
  trust?: Record<string, string>;   // only on a PUT reply: per helper, "accepting" / "no longer accepting" / an error
}
export const validAgent = (a: string) => /^[^\s/]+\/[^\s/]+$/.test(a.trim());
/** Trust results worth showing: anything but the two OK answers, as "agent: what went wrong". */
export const trustProblems = (t: Record<string, string> | undefined) =>
  Object.entries(t || {}).filter(([, v]) => v !== "accepting" && v !== "no longer accepting").map(([a, v]) => `${a}: ${v}`);
/** Base URL of shrooms-agent on the same machine as a Foray service (port 7387). */
export const agentBase = (foray: string) => foray.replace(/:\d+$/, ":7387");
export const hasContext = (c: ListContext | null | undefined) =>
  !!c && !!((c.place || "").trim() || (c.language || "").trim() || (c.currency || "").trim() || (c.notes || "").trim());

export interface Change { seq: number; at: string; by: string; op: "add" | "update" | "comment" | "help" | string; item: Item }

// ── dates ────────────────────────────────────────────────────────────────────
// `due` is "2026-11-01" (a day, in the phone's own time zone) or an RFC 3339 time.
export function dueDate(due: string | undefined): Date | null {
  if (!due) return null;
  const day = /^(\d{4})-(\d{2})-(\d{2})$/.exec(due);
  if (day) return new Date(Number(day[1]), Number(day[2]) - 1, Number(day[3]));
  const t = new Date(due);
  return isNaN(t.getTime()) ? null : t;
}
const dayStart = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
const hasTime = (due: string) => due.includes("T");

/** "today", "tomorrow", "overdue", "later" or "" (no due). */
export function dueBucket(due: string | undefined, now: Date): "" | "overdue" | "today" | "tomorrow" | "later" {
  const d = dueDate(due);
  if (!d) return "";
  const diff = Math.round((dayStart(d) - dayStart(now)) / 864e5);
  if (diff < 0) return "overdue";
  if (diff === 0) return hasTime(due!) && d.getTime() < now.getTime() ? "overdue" : "today";
  return diff === 1 ? "tomorrow" : "later";
}

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const DAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
/** Friendly due label: "Today", "Tomorrow 09:00", "Sat 1 Nov", "1 Nov 2027". */
export function dueLabel(due: string | undefined, now: Date): string {
  const d = dueDate(due);
  if (!d) return "";
  const time = hasTime(due!) ? ` ${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}` : "";
  const diff = Math.round((dayStart(d) - dayStart(now)) / 864e5);
  if (diff === 0) return "Today" + time;
  if (diff === 1) return "Tomorrow" + time;
  if (diff === -1) return "Yesterday" + time;
  const sameYear = d.getFullYear() === now.getFullYear();
  const base = diff > 1 && diff < 7 ? DAYS[d.getDay()] : `${d.getDate()} ${MONTHS[d.getMonth()]}${sameYear ? "" : " " + d.getFullYear()}`;
  return base + time;
}

// ── ordering ─────────────────────────────────────────────────────────────────
export interface Sections { open: Item[]; waiting: Item[]; inbox: Item[]; done: Item[]; suggested: Item[] }

const dueKey = (it: Item) => { const d = dueDate(it.due); return d ? d.getTime() : Infinity; };
const newest = (a: Item, b: Item) => (b.updated || b.created || "").localeCompare(a.updated || a.created || "");

/** Open first (due soonest, undated last), then waiting, inbox; done and dropped folded; suggested apart. */
export function sections(items: Item[]): Sections {
  const s: Sections = { open: [], waiting: [], inbox: [], done: [], suggested: [] };
  for (const it of items) {
    if (it.state === "done" || it.state === "dropped") s.done.push(it);
    else if (it.state === "waiting") s.waiting.push(it);
    else if (it.state === "inbox") s.inbox.push(it);
    else if (it.state === "suggested") s.suggested.push(it);
    else s.open.push(it);
  }
  const byDue = (a: Item, b: Item) => dueKey(a) - dueKey(b) || newest(a, b);
  s.open.sort(byDue); s.waiting.sort(byDue); s.inbox.sort(newest); s.done.sort(newest); s.suggested.sort(newest);
  return s;
}

// ── sorting and grouping (as the Basecamp view does) ─────────────────────────
export type SortBy = "due" | "newest" | "changed" | "az";
export type GroupBy = "state" | "none" | "who" | "tag";
export const SORTS: { v: SortBy; label: string }[] = [
  { v: "due", label: "Due" }, { v: "newest", label: "Newest" }, { v: "changed", label: "Changed" }, { v: "az", label: "A–Z" },
];
export const GROUPS: { v: GroupBy; label: string }[] = [
  { v: "state", label: "State" }, { v: "none", label: "None" }, { v: "who", label: "Who" }, { v: "tag", label: "Tag" },
];
export interface Group { key: string; title: string; items: Item[] }

export function compareBy(sort: SortBy): (a: Item, b: Item) => number {
  const desc = (x?: string, y?: string) => (y || "").localeCompare(x || "");
  switch (sort) {
    case "newest": return (a, b) => desc(a.created, b.created);
    case "changed": return (a, b) => desc(a.updated, b.updated);
    case "az": return (a, b) => a.title.localeCompare(b.title, undefined, { sensitivity: "base" });
    default: return (a, b) => dueKey(a) - dueKey(b) || desc(a.updated, b.updated);   // no due date last
  }
}

/** The assistant's last word on an active item is a question: it's waiting on you. */
export function needsYou(it: Item, session: string | undefined): boolean {
  if (!session || !(it.state === "inbox" || it.state === "open" || it.state === "waiting")) return false;
  const c = it.comments || [];
  const last = c[c.length - 1];
  return !!last && (last.by === session || last.by.endsWith("/" + session)) && isQuestion(last.text);
}

/**
 * The list as shown: "Needs you" first (and not again below), then the active items grouped and
 * sorted, then done/dropped (folded by the screen) and suggestions apart. Empty groups are left out.
 */
export function arrange(items: Item[], session: string | undefined, sort: SortBy, group: GroupBy):
    { needs: Item[]; groups: Group[]; done: Item[]; suggested: Item[] } {
  const cmp = compareBy(sort);
  const active = items.filter((it) => it.state === "inbox" || it.state === "open" || it.state === "waiting");
  const needs = active.filter((it) => needsYou(it, session)).sort(cmp);
  const rest = active.filter((it) => !needsYou(it, session));
  const groups: Group[] = [];
  const push = (key: string, title: string, xs: Item[]) => { if (xs.length) groups.push({ key, title, items: [...xs].sort(cmp) }); };
  if (group === "none") push("all", "To do", rest);
  else if (group === "who" || group === "tag") {
    const by = new Map<string, Item[]>();
    for (const it of rest) {
      const keys = group === "who" ? [(it.who || "").trim()] : ((it.tags || []).length ? it.tags! : [""]);
      for (const k of new Set(keys)) (by.get(k) || by.set(k, []).get(k)!).push(it);
    }
    const order = [...by.keys()].sort((x, y) => (x === "" ? 1 : y === "" ? -1 : x.localeCompare(y, undefined, { sensitivity: "base" })));
    for (const k of order) push(`${group}:${k}`, k ? (group === "tag" ? "#" + k : k) : (group === "who" ? "Anyone" : "No tag"), by.get(k)!);
  } else {
    push("inbox", "New", rest.filter((it) => it.state === "inbox"));
    push("open", "To do", rest.filter((it) => it.state === "open"));
    push("waiting", "Waiting", rest.filter((it) => it.state === "waiting"));
  }
  const done = items.filter((it) => it.state === "done" || it.state === "dropped").sort(compareBy("changed"));
  const suggested = items.filter((it) => it.state === "suggested").sort(compareBy("newest"));
  return { needs, groups, done, suggested };
}

/** Fold /changes events into the items: the latest event for an id is its state. */
export function applyChanges(items: Item[], events: Change[]): Item[] {
  if (!events.length) return items;
  const byId = new Map(items.map((i) => [i.id, i]));
  for (const e of [...events].sort((a, b) => a.seq - b.seq)) if (e && e.item && e.item.id) byId.set(e.item.id, e.item);
  return [...byId.values()];
}

// ── "who" in plain words ─────────────────────────────────────────────────────
/** "phone.home" → "phone"; "k11/research" → "research"; the list's session → "the assistant". */
export function whoLabel(by: string | undefined, session: string | undefined, me: string | undefined): string {
  if (!by) return "";
  if (me && by === me) return "you";
  if (session && (by === session || by.endsWith("/" + session))) return "the assistant";
  if (by.includes("/")) return by.slice(by.lastIndexOf("/") + 1);
  const dot = by.indexOf(".");
  return dot > 0 ? by.slice(0, dot) : by;
}

export const HELP_KINDS: { kind: string; label: string; hint: string }[] = [
  { kind: "analyze", label: "Analyze", hint: "let the assistant work out what would help" },
  { kind: "find out", label: "Find out", hint: "e.g. which garages are open on Saturday" },
  { kind: "compare", label: "Compare", hint: "e.g. three options with prices" },
  { kind: "draft", label: "Draft", hint: "e.g. the email to the garage" },
  { kind: "plan", label: "Plan", hint: "e.g. break it into steps" },
];

/** A comment that asks something: its plain text ends with a question mark (also the full-width one). */
export const isQuestion = (text: string) => /[?？]["'”’)\]]*$/.test(stripMarkdown(text || "").trim());

// ── notifications ────────────────────────────────────────────────────────────
export interface Note { key: string; title: string; body: string; list: string; item: string }

/**
 * What to tell the person about a batch of changes: the assistant asked something (a comment by the
 * list's session), help came back (a help entry turned completed). `seen` holds keys already
 * notified, so a re-poll never repeats one; `me` is this device's name, so its own actions are quiet.
 */
export function notesFromChanges(list: ListInfo, events: Change[], seen: Set<string>, me?: string): Note[] {
  const out: Note[] = [];
  const add = (n: Note) => { if (!seen.has(n.key) && !out.some((o) => o.key === n.key)) out.push(n); };
  for (const e of events) {
    const it = e.item;
    if (!it || !it.id) continue;
    if (e.op === "comment" && list.session) {
      const last = (it.comments || [])[(it.comments || []).length - 1];
      if (last && (last.by === list.session || last.by.endsWith("/" + list.session)))
        add({ key: `q:${list.name}:${it.id}:${last.at}`, title: `${it.title}`,
              body: `${isQuestion(last.text) ? "The assistant asks" : "The assistant"}: ${stripMarkdown(last.text)}`, list: list.name, item: it.id });
    }
    for (const h of it.help || []) {
      if (h.state === "completed")
        add({ key: `h:${list.name}:${it.id}:${h.task || h.kind}`, title: `Help is back: ${it.title}`,
              body: stripMarkdown(h.answer || "").slice(0, 200) || `The ${h.kind} you asked for is done.`, list: list.name, item: it.id });
    }
    if (e.op === "add" && me && e.by !== me && !e.item.suggested_by)
      add({ key: `a:${list.name}:${it.id}`, title: `New on ${list.title}`, body: it.title, list: list.name, item: it.id });
  }
  return out;
}

/** Items due today (or overdue) that are still to do, once per item per day. */
export function dueTodayNotes(list: ListInfo, items: Item[], now: Date, seen: Set<string>): Note[] {
  const day = `${now.getFullYear()}-${now.getMonth() + 1}-${now.getDate()}`;
  const out: Note[] = [];
  for (const it of items) {
    if (it.state !== "open" && it.state !== "waiting" && it.state !== "inbox") continue;
    const b = dueBucket(it.due, now);
    if (b !== "today" && b !== "overdue") continue;
    const key = `d:${list.name}:${it.id}:${day}`;
    if (seen.has(key)) continue;
    out.push({ key, title: b === "today" ? "Due today" : "Overdue", body: it.title, list: list.name, item: it.id });
  }
  return out;
}

/** Base URL for an address the person typed or the mesh gave: bare IPv6 gets brackets. */
export function baseUrl(address: string, port: number): string {
  let a = address.trim().replace(/^https?:\/\//, "").replace(/\/+$/, "");
  const bracket = /^\[(.+)\](?::(\d+))?$/.exec(a);
  if (bracket) return `http://[${bracket[1]}]:${bracket[2] || port}`;
  if ((a.match(/:/g) || []).length >= 2) return `http://[${a}]:${port}`;   // bare IPv6
  const hp = /^(.+):(\d+)$/.exec(a);
  return hp ? `http://${hp[1]}:${hp[2]}` : `http://${a}:${port}`;
}

// ── an item's reference: to hand an item to an agent, or open it on a phone ───
// What "Copy reference" copies: the item's own address, as the Basecamp view's itemRef. GETting it
// gives an agent the item, its list and how to change it. (foray://item links open an item on a
// phone, from a QR or another app; they aren't part of the copy.)
const enc = (s: string) => (/^[0-9A-Za-z:.\-_]+$/.test(s) ? s : encodeURIComponent(s));
export function hostOf(base: string): { address: string; port: number } | null {
  const m = /^https?:\/\/(\[([^\]]+)\]|[^:/\[\]]+)(?::(\d+))?/.exec(base || "");
  if (!m) return null;
  return { address: m[2] || m[1], port: m[3] ? Number(m[3]) : 80 };
}
export function itemLink(address: string, port: number, list: string, id: string): string {
  return `foray://item?address=${enc(address)}&list=${enc(list)}&id=${enc(id)}` + (port && port !== 7389 ? `&port=${port}` : "");
}
export function itemRef(base: string, list: { name: string }, item: { id: string }): string {
  return `${base.replace(/\/+$/, "")}/v1/lists/${encodeURIComponent(list.name)}/items/${encodeURIComponent(item.id)}`;
}

export type ForayLink =
  | { kind: "add"; address: string; port: number }
  | { kind: "item"; address: string; port: number; list: string; id: string };
/** foray://add?address=…[&port=N] and foray://item?address=…&list=…&id=…[&port=N]; anything else is null. */
export function parseForayLink(url: string | null | undefined): ForayLink | null {
  const m = /^foray:\/\/(add|item)\/?(?:\?(.*))?$/.exec((url || "").trim());
  if (!m) return null;
  const q = new Map<string, string>();
  for (const kv of (m[2] || "").split("&")) {
    const i = kv.indexOf("=");
    if (i <= 0) continue;
    try { q.set(kv.slice(0, i), decodeURIComponent(kv.slice(i + 1).replace(/\+/g, " "))); } catch { /* a malformed escape: skip it */ }
  }
  const address = (q.get("address") || "").trim().replace(/^\[|\]$/g, "");
  if (!address) return null;
  const port = Number(q.get("port")) || 7389;
  if (m[1] === "add") return { kind: "add", address, port };
  const list = (q.get("list") || "").trim(), id = (q.get("id") || "").trim();
  return list && id ? { kind: "item", address, port, list, id } : null;
}

// ── writes waiting for the list's computer (the outbox, docs/design.md §7.1) ──
// An add, an update or a comment made while the list's computer doesn't answer is kept here and sent
// when it does, oldest first. Nothing merges on the phone: the service decides, and the next catch-up
// shows what it decided. Until then the list shows the writes on top of the kept copy, marked unsent.
export type AddBody = { title: string; notes?: string; due?: string; who?: string; tags?: string[]; state?: string };
export type PatchBody = Partial<Pick<Item, "title" | "notes" | "state" | "due" | "who" | "tags" | "summary">>;
export type NewOp = { kind: "add"; body: AddBody } | { kind: "patch"; item: string; body: PatchBody } | { kind: "comment"; item: string; text: string };
export type OutOp = NewOp & { id: string; at: string; base: string; list: string };
/** An added item has no id of its own until it is sent; until then it goes by its op's id. */
export const UNSENT_PREFIX = "unsent-";
export const isUnsentId = (id: string | undefined) => !!id && id.startsWith(UNSENT_PREFIX);

/** The list as this phone shows it: the kept or fetched items, with the unsent writes on top. */
export function withOutbox(items: Item[], ops: OutOp[], me: string): Item[] {
  if (!ops.length) return items;
  const byId = new Map(items.map((i) => [i.id, i]));
  const order = items.map((i) => i.id);
  for (const op of ops) {
    if (op.kind === "add") {
      const b = op.body;
      byId.set(op.id, { id: op.id, title: b.title, notes: b.notes, due: b.due, who: b.who, tags: b.tags,
        state: (b.state as State) || "open", added_by: me, created: op.at, updated: op.at, unsent: true });
      order.push(op.id);
      continue;
    }
    const cur = byId.get(op.item);
    if (!cur) continue;   // gone from the list: the service will say what happened when it's sent
    if (op.kind === "patch") byId.set(op.item, { ...cur, ...op.body, updated: op.at, unsent: true });
    else byId.set(op.item, { ...cur, comments: [...(cur.comments || []), { by: me, at: op.at, text: op.text, unsent: true }], unsent: true });
  }
  return order.map((id) => byId.get(id)!).filter(Boolean);
}

/** After an unsent add is sent: later ops on that item now name the id the service gave it. */
export function remapOps(ops: OutOp[], from: string, to: string): OutOp[] {
  return ops.map((o) => (o.kind !== "add" && o.item === from ? { ...o, item: to } : o));
}

/** The lists shown: each computer that answered replaces its own lists; the others keep the copy. */
export function mergeFound<T extends { base: string; list: { mesh: string; title: string; name: string } }>(prev: T[], fresh: T[], answered: string[]): T[] {
  const ok = new Set(answered);
  const out = [...prev.filter((f) => !ok.has(f.base)), ...fresh];
  return out.sort((a, b) => a.list.mesh.localeCompare(b.list.mesh) || (a.list.title || a.list.name).localeCompare(b.list.title || b.list.name));
}

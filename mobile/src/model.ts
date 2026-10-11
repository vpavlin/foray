// What the app shows and when it notifies, as pure functions of the service's data (docs/api.md).
// No React, no network: tested in test/model.test.ts.

export type State = "suggested" | "inbox" | "open" | "waiting" | "done" | "dropped";

export interface Comment { by: string; at: string; text: string }
export interface Help { task?: string; worker?: string; kind: string; state: string; ask?: string; answer?: string; updated?: string }
export interface Item {
  id: string; title: string; notes?: string; state: State;
  due?: string; who?: string; tags?: string[];
  added_by?: string; suggested_by?: string; created?: string; updated?: string;
  comments?: Comment[]; help?: Help[]; calendar?: string;
}
export interface ListInfo {
  name: string; title: string; mesh: string; session?: string; seq: number;
  counts?: Partial<Record<State, number>>;
}
/** What the household tells the assistant about itself (GET/PUT /v1/lists/{list}/context). */
export interface ListContext { place?: string; language?: string; currency?: string; notes?: string; by?: string; updated?: string }
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
  { kind: "find out", label: "Find out", hint: "e.g. which garages are open on Saturday" },
  { kind: "compare", label: "Compare", hint: "e.g. three options with prices" },
  { kind: "draft", label: "Draft", hint: "e.g. the email to the garage" },
  { kind: "plan", label: "Plan", hint: "e.g. break it into steps" },
];

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
        add({ key: `q:${list.name}:${it.id}:${last.at}`, title: `${it.title}`, body: `The assistant asks: ${last.text}`, list: list.name, item: it.id });
    }
    for (const h of it.help || []) {
      if (h.state === "completed")
        add({ key: `h:${list.name}:${it.id}:${h.task || h.kind}`, title: `Help is back: ${it.title}`,
              body: (h.answer || "").slice(0, 200) || `The ${h.kind} you asked for is done.`, list: list.name, item: it.id });
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

// What the phone remembers: the computers to ask for lists, where each list's change feed is up to,
// which notifications it already showed, and the name the service gives this phone. Also a copy of
// what it last saw (the lists found, and each list's items) to show at once on open, and the outbox
// of writes waiting for a computer that didn't answer (docs/design.md §7.1).
import AsyncStorage from "@react-native-async-storage/async-storage";
import type { SortBy, GroupBy, Item, ListInfo, ListContext, OutOp } from "./model";

export interface Machine { address: string; port: number }
const K = { machines: "foray.machines", seq: "foray.seq", seen: "foray.seen", me: "foray.me", drafts: "foray.drafts", view: "foray.view",
  found: "foray.found", outbox: "foray.outbox", copy: "foray.copy." };

/** A list as this phone last saw it, from the computer named by `base`. */
export interface Copy { list: ListInfo; items: Item[]; asks: { from: string; title: string }[]; context: ListContext | null; seq: number; at: string }
/** A list found on a computer (kept so the lists show before any computer answers). */
export interface FoundCopy { base: string; machine: string; list: ListInfo }
const copyKey = (base: string, list: string) => K.copy + base + "|" + list;

// Outbox changes go one at a time: adding a write and removing a sent one must not overwrite each other.
let outboxChain: Promise<unknown> = Promise.resolve();

async function getJson<T>(k: string, fallback: T): Promise<T> {
  try { const v = await AsyncStorage.getItem(k); return v ? (JSON.parse(v) as T) : fallback; } catch { return fallback; }
}
const setJson = (k: string, v: unknown) => AsyncStorage.setItem(k, JSON.stringify(v)).catch(() => {});

export const store = {
  machines: () => getJson<Machine[]>(K.machines, []),
  setMachines: (m: Machine[]) => setJson(K.machines, m),
  seqs: () => getJson<Record<string, number>>(K.seq, {}),
  setSeqs: (s: Record<string, number>) => setJson(K.seq, s),
  async seen(): Promise<Set<string>> { return new Set(await getJson<string[]>(K.seen, [])); },
  // Keep the newest 1000 keys: plenty for a household list, bounded forever.
  setSeen: (s: Set<string>) => setJson(K.seen, [...s].slice(-1000)),
  me: () => getJson<Record<string, string>>(K.me, {}),
  setMe: (m: Record<string, string>) => setJson(K.me, m),
  // Unsent notes, per item ("base|list|id" → text), so Back never loses what was typed.
  drafts: () => getJson<Record<string, string>>(K.drafts, {}),
  setDrafts: (d: Record<string, string>) => setJson(K.drafts, d),
  // How this device sorts and groups lists.
  async view(): Promise<{ sort: SortBy; group: GroupBy } | null> {
    const v = await getJson<any>(K.view, null);
    const ok = v && ["due", "newest", "changed", "az"].includes(v.sort) && ["state", "none", "who", "tag"].includes(v.group);
    return ok ? { sort: v.sort, group: v.group } : null;
  },
  setView: (v: { sort: SortBy; group: GroupBy }) => setJson(K.view, v),

  found: () => getJson<FoundCopy[]>(K.found, []),
  setFound: (f: FoundCopy[]) => setJson(K.found, f),
  copy: async (base: string, list: string): Promise<Copy | null> => {
    const c = await getJson<Copy | null>(copyKey(base, list), null);
    return c && c.list && Array.isArray(c.items) && typeof c.seq === "number" ? c : null;
  },
  setCopy: (base: string, list: string, c: Copy) => setJson(copyKey(base, list), c),
  /** Forgetting a computer drops what this phone kept from it: its lists, their copies and unsent writes. */
  async forget(base: string) {
    try {
      const keys = (await AsyncStorage.getAllKeys()).filter((k) => k.startsWith(K.copy + base + "|"));
      if (keys.length) await AsyncStorage.multiRemove(keys);
    } catch { /* nothing kept */ }
    await setJson(K.found, (await getJson<FoundCopy[]>(K.found, [])).filter((f) => f.base !== base));
    await store.updateOutbox((ops) => ops.filter((o) => o.base !== base));
  },

  outbox: () => getJson<OutOp[]>(K.outbox, []),
  /** Change the outbox; changes run one after another. Returns the new outbox. */
  updateOutbox(f: (ops: OutOp[]) => OutOp[]): Promise<OutOp[]> {
    const run = outboxChain.then(async () => { const next = f(await getJson<OutOp[]>(K.outbox, [])); await setJson(K.outbox, next); return next; });
    outboxChain = run.catch(() => {});
    return run;
  },
};

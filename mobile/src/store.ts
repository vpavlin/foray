// What the phone remembers: the computers to ask for lists, where each list's change feed is up to,
// which notifications it already showed, and the name the service gives this phone.
import AsyncStorage from "@react-native-async-storage/async-storage";

export interface Machine { address: string; port: number }
const K = { machines: "foray.machines", seq: "foray.seq", seen: "foray.seen", me: "foray.me", drafts: "foray.drafts" };

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
};

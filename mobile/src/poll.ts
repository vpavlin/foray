// One pass over every list on every remembered computer: find what changed since last time and
// notify about the things worth a buzz. The foreground app and the background task both run it.
import * as Notifications from "expo-notifications";
import { api } from "./api";
import { baseUrl, notesFromChanges, dueTodayNotes, ListInfo } from "./model";
import { store } from "./store";

export interface Found { base: string; machine: string; list: ListInfo }

let channelReady = false;
async function ensureChannel() {
  if (channelReady) return;
  await Notifications.setNotificationChannelAsync("foray", { name: "Foray", importance: Notifications.AndroidImportance.DEFAULT }).catch(() => {});
  channelReady = true;
}

/** Every list the remembered computers answer for. A computer that doesn't answer is skipped. */
export async function findLists(): Promise<{ found: Found[]; unreachable: string[] }> {
  const machines = await store.machines();
  const found: Found[] = [], unreachable: string[] = [];
  await Promise.all(machines.map(async (m) => {
    const base = baseUrl(m.address, m.port);
    try {
      const r = await api.lists(base);
      for (const l of r.lists || []) found.push({ base, machine: r.machine || m.address, list: l });
    } catch { unreachable.push(m.address); }
  }));
  found.sort((a, b) => a.list.mesh.localeCompare(b.list.mesh) || a.list.title.localeCompare(b.list.title));
  return { found, unreachable };
}

/** Check every list; notify unless `quiet` (e.g. the list is open on screen). Returns the lists found. */
export async function checkAll(opts: { notify: boolean; quietList?: string } = { notify: true }): Promise<Found[]> {
  const { found } = await findLists();
  const seqs = await store.seqs(), seen = await store.seen(), me = await store.me();
  // What each computer calls this phone, so its own changes don't notify it.
  for (const b of new Set(found.map((f) => f.base))) { try { const w = await api.whoami(b); if (w && w.name) me[b] = w.name; } catch { /* keep the last known */ } }
  await store.setMe(me);
  const now = new Date();
  const toShow: { key: string; title: string; body: string; data: Record<string, string> }[] = [];
  for (const f of found) {
    const key = f.base + "|" + f.list.name;
    const since = seqs[key];
    try {
      if (since === undefined) { seqs[key] = f.list.seq; }   // first sight: don't replay history
      else if (f.list.seq !== since) {
        const ch = await api.changes(f.base, f.list.name, since);
        if (!ch.reset) for (const n of notesFromChanges(f.list, ch.events || [], seen, me[f.base]))
          toShow.push({ key: n.key, title: n.title, body: n.body, data: { base: f.base, list: n.list, item: n.item } });
        seqs[key] = typeof ch.seq === "number" ? ch.seq : f.list.seq;
      }
      if (opts.notify) {
        const full = await api.list(f.base, f.list.name);
        for (const n of dueTodayNotes(f.list, full.items || [], now, seen))
          toShow.push({ key: n.key, title: n.title, body: n.body, data: { base: f.base, list: n.list, item: n.item } });
      }
    } catch { /* this list is unreachable now; next time */ }
  }
  for (const n of toShow) {
    seen.add(n.key);
    if (!opts.notify || n.data.list === opts.quietList) continue;
    await ensureChannel();
    await Notifications.scheduleNotificationAsync({ content: { title: n.title, body: n.body, data: n.data }, trigger: null }).catch(() => {});
  }
  await store.setSeqs(seqs);
  await store.setSeen(seen);
  return found;
}

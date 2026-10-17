// The outbox (docs/design.md §7.1): writes made while a list's computer didn't answer, sent when it
// does, oldest first. The service decides what is true; the phone only replays what was done.
import { api, ApiError } from "./api";
import { store } from "./store";
import { Item, NewOp, OutOp, UNSENT_PREFIX, isUnsentId, remapOps } from "./model";

/** Send one write, with its outbox id as the Idempotency-Key: if the answer to a first try was lost
 *  and the write goes out again from the outbox, the service applies it once (design §7.1).
 *  An unsent item's id can't be sent: the caller queues those. */
export function sendOp(base: string, list: string, op: OutOp): Promise<Item> {
  if (op.kind === "add") return api.add(base, list, op.body, op.id);
  if (op.kind === "patch") return api.patch(base, list, op.item, op.body, op.id);
  return api.comment(base, list, op.item, op.text, op.id);
}

/** "Can't reach it": keep the write and try later. Anything else is the service's answer. */
export const offline = (e: unknown) => e instanceof ApiError && e.status === 0;

let n = 0;
/** A write with its id, made before the first try: that id is its Idempotency-Key, sent or queued. */
export function makeOp(base: string, list: string, op: NewOp): OutOp {
  const id = `${UNSENT_PREFIX}${Date.now().toString(36)}-${(n++).toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
  return { ...op, id, at: new Date().toISOString(), base, list } as OutOp;
}
/** Keep a write (under the id it was already tried with) until its computer answers. */
export async function enqueue(out: OutOp): Promise<OutOp> {
  await store.updateOutbox((ops) => [...ops, out]);
  return out;
}

/** Is anything for this list (or this computer, without a list) still waiting? */
export async function pending(base: string, list?: string): Promise<OutOp[]> {
  return (await store.outbox()).filter((o) => o.base === base && (list === undefined || o.list === list));
}

export interface Flushed {
  sent: { list: string; item: Item }[];
  ids: Record<string, string>;   // an unsent item's id → the id the service gave it
  failed: string[];              // writes the service refused, in words
  left: number;                  // still waiting (the computer stopped answering)
}

const running = new Map<string, Promise<Flushed>>();

/** Send what's waiting for one computer, oldest first; stop at the first "can't reach it". */
export function flush(base: string): Promise<Flushed> {
  const cur = running.get(base);
  if (cur) return cur;
  const p = doFlush(base).finally(() => running.delete(base));
  running.set(base, p);
  return p;
}

async function doFlush(base: string): Promise<Flushed> {
  const res: Flushed = { sent: [], ids: {}, failed: [], left: 0 };
  for (;;) {
    const next = (await store.outbox()).find((o) => o.base === base);
    if (!next) break;
    // A write to an item whose add the service refused: there is nothing to write to.
    if (next.kind !== "add" && isUnsentId(next.item)) {
      await store.updateOutbox((ops) => ops.filter((o) => o.id !== next.id));
      continue;
    }
    try {
      const item = await sendOp(base, next.list, next);
      res.sent.push({ list: next.list, item });
      await store.updateOutbox((ops) => {
        const rest = ops.filter((o) => o.id !== next.id);
        return next.kind === "add" && item && item.id ? remapOps(rest, next.id, item.id) : rest;
      });
      if (next.kind === "add" && item && item.id) res.ids[next.id] = item.id;
    } catch (e) {
      if (offline(e)) { res.left = (await pending(base)).length; break; }
      // The service said no (it's read-only for this phone, the item is gone…): drop the write and say so.
      await store.updateOutbox((ops) => ops.filter((o) => o.id !== next.id));
      const what = next.kind === "add" ? `Adding “${next.body.title}”` : next.kind === "comment" ? "A note" : "A change";
      res.failed.push(`${what} wasn't accepted: ${e instanceof Error ? e.message : String(e)}`);
    }
  }
  return res;
}

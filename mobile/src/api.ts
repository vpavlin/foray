// The Foray service's HTTP API (docs/api.md). Every call has a timeout: a computer that is off or
// off-mesh must never leave the app spinning.
import type { Item, ListInfo, Change, ListContext } from "./model";

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) { super(message); this.status = status; }
}

async function req<T>(base: string, path: string, init: { method?: string; body?: unknown } = {}, timeoutMs = 8000): Promise<T> {
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), timeoutMs);
  let res: Response;
  try {
    res = await fetch(base + path, {
      method: init.method || "GET",
      headers: init.body !== undefined ? { "Content-Type": "application/json" } : undefined,
      body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
      signal: ctl.signal,
    });
  } catch {
    throw new ApiError(0, "Can't reach it right now.");
  } finally { clearTimeout(timer); }
  const text = await res.text();
  let data: any = null;
  try { data = text ? JSON.parse(text) : null; } catch { data = null; }
  if (!res.ok) {
    const msg = res.status === 403 ? "This phone can look at the list but not change it."
      : (data && typeof data.error === "string" ? data.error : `Something went wrong (${res.status}).`);
    throw new ApiError(res.status, msg);
  }
  return data as T;
}

const enc = encodeURIComponent;
export interface WhoAmI { name: string; mesh: string; local: boolean; can_write: boolean; machine: string }
export const api = {
  whoami: (b: string) => req<WhoAmI>(b, "/v1/whoami", {}, 5000),
  lists: (b: string) => req<{ machine: string; lists: ListInfo[] }>(b, "/v1/lists", {}, 5000),
  context: (b: string, l: string) => req<ListContext>(b, `/v1/lists/${enc(l)}/context`),
  setContext: (b: string, l: string, c: { place: string; language: string; currency: string; notes: string }) =>
    req<ListContext>(b, `/v1/lists/${enc(l)}/context`, { method: "PUT", body: c }),
  list: (b: string, l: string) => req<{ list: ListInfo; items: Item[]; context?: ListContext; asks?: { from: string; title: string; at: string }[]; suggest_from?: string[] }>(b, `/v1/lists/${enc(l)}`),
  changes: (b: string, l: string, since: number) => req<{ seq: number; events: Change[]; reset?: boolean }>(b, `/v1/lists/${enc(l)}/changes?since=${since}`),
  add: (b: string, l: string, body: { title: string; notes?: string; due?: string; who?: string; tags?: string[]; state?: string }) =>
    req<Item>(b, `/v1/lists/${enc(l)}/items`, { method: "POST", body }),
  patch: (b: string, l: string, id: string, body: Partial<Pick<Item, "title" | "notes" | "state" | "due" | "who" | "tags">>) =>
    req<Item>(b, `/v1/lists/${enc(l)}/items/${enc(id)}`, { method: "PATCH", body }),
  comment: (b: string, l: string, id: string, text: string) => req<Item>(b, `/v1/lists/${enc(l)}/items/${enc(id)}/comments`, { method: "POST", body: { text } }),
  ask: (b: string, l: string, id: string, kind: string, text?: string) => req<Item>(b, `/v1/lists/${enc(l)}/items/${enc(id)}/ask`, { method: "POST", body: { kind, text } }),
  allow: (b: string, l: string, from: string) => req<unknown>(b, `/v1/lists/${enc(l)}/allow`, { method: "POST", body: { from } }),
  ignore: (b: string, l: string, from: string) => req<unknown>(b, `/v1/lists/${enc(l)}/ignore`, { method: "POST", body: { from } }),
};

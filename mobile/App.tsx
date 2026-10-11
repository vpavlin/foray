// Foray: the household's shared lists. Plain words in the main flow: lists, things to do, who, when.
import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  Alert, AppState, Linking, KeyboardAvoidingView, Modal, Platform, Pressable, RefreshControl, ScrollView, StyleSheet,
  Text, TextInput, ToastAndroid, View,
} from "react-native";
import { StatusBar } from "expo-status-bar";
import { SafeAreaProvider, SafeAreaView } from "react-native-safe-area-context";
import DateTimePicker from "@react-native-community/datetimepicker";
import * as Notifications from "expo-notifications";
import { api, ApiError } from "./src/api";
import { store, Machine } from "./src/store";
import { checkAll, findLists, Found } from "./src/poll";
import { registerBackground } from "./src/background";
import { Item, ListInfo, State, sections, applyChanges, dueBucket, dueLabel, whoLabel, HELP_KINDS, baseUrl } from "./src/model";

const C = {
  bg: "#F4F5F1", card: "#FFFFFF", text: "#1E2320", sub: "#67706A", faint: "#9AA29C", line: "#E2E5DF",
  accent: "#3D7A57", accentSoft: "#E3EFE7", warn: "#B4511F", warnSoft: "#F8E7DC", blue: "#2F6690",
};
const toast = (m: string) => { if (Platform.OS === "android") ToastAndroid.show(m, ToastAndroid.SHORT); };
const msg = (e: unknown) => (e instanceof Error ? e.message : String(e));
const cap = (s: string) => (s ? s.charAt(0).toUpperCase() + s.slice(1) : s);
const isoDay = (d: Date) => `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

Notifications.setNotificationHandler({
  handleNotification: async () => ({ shouldShowBanner: true, shouldShowList: true, shouldPlaySound: false, shouldSetBadge: false }),
});

type Open = { base: string; list: ListInfo };

export default function App() {
  return (
    <SafeAreaProvider>
      <SafeAreaView style={{ flex: 1, backgroundColor: C.bg }} edges={["top", "bottom"]}>
        <StatusBar style="dark" />
        <Main />
      </SafeAreaView>
    </SafeAreaProvider>
  );
}

function Main() {
  const [machines, setMachines] = useState<Machine[] | null>(null);
  const [found, setFound] = useState<Found[]>([]);
  const [unreachable, setUnreachable] = useState<string[]>([]);
  const [loading, setLoading] = useState(false);
  const [open, setOpen] = useState<Open | null>(null);
  const [openItem, setOpenItem] = useState<string | null>(null);
  const [settings, setSettings] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      setMachines(await store.machines());
      const r = await findLists();
      setFound(r.found); setUnreachable(r.unreachable);
    } finally { setLoading(false); }
  }, []);

  // foray://add?address=<overlay>[&port=N]: setting up by scanning a QR instead of typing.
  const onLink = useCallback(async (url: string | null) => {
    if (!url || !url.startsWith("foray://add")) return;
    const q = url.slice(url.indexOf("?") + 1).split("&").map((kv) => kv.split("=").map(decodeURIComponent));
    const get = (k: string) => (q.find(([a]) => a === k) || [])[1] || "";
    const address = get("address").trim();
    if (!address) return;
    const port = Number(get("port")) || 7389;
    const ms = await store.machines();
    if (!ms.some((m) => m.address === address && m.port === port)) await store.setMachines([...ms, { address, port }]);
    toast("Added. Looking for lists…");
    refresh();
  }, []);   // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    refresh();
    Linking.getInitialURL().then(onLink).catch(() => {});
    const linkSub = Linking.addEventListener("url", (e) => { onLink(e.url); });
    Notifications.requestPermissionsAsync().catch(() => {});
    registerBackground();
    // A tap on a notification opens its list and item.
    const sub = Notifications.addNotificationResponseReceivedListener((r) => {
      const d = r.notification.request.content.data as any;
      if (!d || !d.base || !d.list) return;
      findLists().then(({ found: fs }) => {
        const f = fs.find((x) => x.base === d.base && x.list.name === d.list);
        if (f) { setOpen({ base: f.base, list: f.list }); setOpenItem(d.item || null); }
      });
    });
    return () => { sub.remove(); linkSub.remove(); };
  }, [refresh, onLink]);

  // While the app is open: check every 30 s (notifications for lists not on screen).
  useEffect(() => {
    const t = setInterval(() => { if (AppState.currentState === "active") checkAll({ notify: true, quietList: open?.list.name }).then(setFound).catch(() => {}); }, 30000);
    return () => clearInterval(t);
  }, [open]);

  if (machines === null) return <View style={{ flex: 1 }} />;
  if (open) return <ListScreen base={open.base} info={open.list} initialItem={openItem} onBack={() => { setOpen(null); setOpenItem(null); refresh(); }} />;

  const byMesh = new Map<string, Found[]>();
  for (const f of found) { const k = f.list.mesh; (byMesh.get(k) || byMesh.set(k, []).get(k)!).push(f); }

  return (
    <View style={{ flex: 1 }}>
      <View style={s.header}>
        <Text style={s.appTitle}>Foray</Text>
        <Pressable hitSlop={10} onPress={() => setSettings(true)}><Text style={s.headerLink}>Computers</Text></Pressable>
      </View>
      <ScrollView contentContainerStyle={{ padding: 16, gap: 12 }} refreshControl={<RefreshControl refreshing={loading} onRefresh={refresh} />}>
        {machines.length === 0 && <AddMachine first onAdded={refresh} />}
        {machines.length > 0 && found.length === 0 && !loading && (
          <View style={s.card}>
            <Text style={s.body}>No lists found yet.</Text>
            <Text style={s.sub}>The computer that keeps your lists may be off, or this phone isn't connected to it. Pull down to try again.</Text>
          </View>
        )}
        {[...byMesh.entries()].map(([mesh, fs]) => (
          <View key={mesh} style={{ gap: 8 }}>
            {byMesh.size > 1 && <Text style={s.groupLabel}>{cap(mesh)}</Text>}
            {fs.map((f) => {
              const c = f.list.counts || {};
              const todo = (c.open || 0) + (c.inbox || 0);
              return (
                <Pressable key={f.base + f.list.name} style={s.listCard} onPress={() => setOpen({ base: f.base, list: f.list })}>
                  <Text style={s.listTitle}>{f.list.title || f.list.name}</Text>
                  <Text style={s.sub}>{[todo ? `${todo} to do` : "Nothing to do", c.waiting ? `${c.waiting} waiting` : ""].filter(Boolean).join(" · ")}</Text>
                </Pressable>
              );
            })}
          </View>
        ))}
        {unreachable.length > 0 && found.length > 0 && <Text style={s.hint}>Not answering right now: {unreachable.join(", ")}</Text>}
      </ScrollView>
      <Modal visible={settings} animationType="slide" onRequestClose={() => setSettings(false)}>
        <Settings onClose={() => { setSettings(false); refresh(); }} />
      </Modal>
    </View>
  );
}

function AddMachine({ first, onAdded }: { first?: boolean; onAdded: () => void }) {
  const [addr, setAddr] = useState("");
  const [busy, setBusy] = useState(false);
  const add = async () => {
    const a = addr.trim();
    if (!a) return;
    setBusy(true);
    try {
      const portMatch = /\]:(\d+)$|^[^:]+:(\d+)$/.exec(a);
      const m: Machine = { address: a, port: portMatch ? Number(portMatch[1] || portMatch[2]) : 7389 };
      try { await api.lists(baseUrl(m.address, m.port)); }
      catch { Alert.alert("No answer from that address", "Check the address, and that this phone is connected to the same network as that computer. You can add it anyway."); }
      const ms = await store.machines();
      if (!ms.some((x) => x.address === m.address && x.port === m.port)) await store.setMachines([...ms, m]);
      setAddr(""); onAdded();
    } finally { setBusy(false); }
  };
  return (
    <View style={s.card}>
      {first && <Text style={s.cardTitle}>Welcome to Foray</Text>}
      <Text style={s.sub}>{first ? "Foray keeps your household's lists on one of your computers. Type its address once; whoever set Foray up can tell you what it is." : "Add another computer that keeps lists."}</Text>
      <TextInput style={[s.input, { marginTop: 10 }]} value={addr} onChangeText={setAddr} placeholder="Computer address" placeholderTextColor={C.faint}
        autoCapitalize="none" autoCorrect={false} onSubmitEditing={add} />
      <Pressable onPress={add} disabled={busy || !addr.trim()} style={[s.primaryBtn, { marginTop: 10 }, (busy || !addr.trim()) && { opacity: 0.5 }]}>
        <Text style={s.primaryBtnT}>{busy ? "Looking…" : "Add"}</Text>
      </Pressable>
    </View>
  );
}

function Settings({ onClose }: { onClose: () => void }) {
  const [ms, setMs] = useState<Machine[]>([]);
  const load = useCallback(() => { store.machines().then(setMs); }, []);
  useEffect(load, [load]);
  const remove = (m: Machine) => Alert.alert("Forget this computer?", "Its lists stay where they are; this phone just stops showing them.", [
    { text: "Keep", style: "cancel" },
    { text: "Forget", style: "destructive", onPress: async () => { await store.setMachines(ms.filter((x) => x !== m)); load(); } },
  ]);
  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: C.bg }}>
      <View style={s.header}>
        <Pressable hitSlop={10} onPress={onClose}><Text style={s.headerLink}>‹ Back</Text></Pressable>
        <Text style={s.screenTitle}>Computers</Text>
        <View style={{ width: 50 }} />
      </View>
      <ScrollView contentContainerStyle={{ padding: 16, gap: 12 }}>
        {ms.map((m) => (
          <View key={m.address + m.port} style={[s.card, { flexDirection: "row", alignItems: "center" }]}>
            <Text style={[s.body, { flex: 1 }]} numberOfLines={1}>{m.address}{m.port !== 7389 ? `  (port ${m.port})` : ""}</Text>
            <Pressable hitSlop={8} onPress={() => remove(m)}><Text style={{ color: C.warn }}>Forget</Text></Pressable>
          </View>
        ))}
        <AddMachine onAdded={load} />
      </ScrollView>
    </SafeAreaView>
  );
}

// ── one list ─────────────────────────────────────────────────────────────────
function ListScreen({ base, info, initialItem, onBack }: { base: string; info: ListInfo; initialItem: string | null; onBack: () => void }) {
  const [list, setList] = useState<ListInfo>(info);
  const [items, setItems] = useState<Item[]>([]);
  const [asks, setAsks] = useState<{ from: string; title: string }[]>([]);
  const [loaded, setLoaded] = useState(false);
  const [error, setError] = useState("");
  const [readOnly, setReadOnly] = useState(false);
  const [me, setMe] = useState("");
  const [draft, setDraft] = useState("");
  const [showDone, setShowDone] = useState(false);
  const [showSuggested, setShowSuggested] = useState(false);
  const [sel, setSel] = useState<string | null>(initialItem);
  const seq = useRef(0);

  const load = useCallback(async () => {
    try {
      const r = await api.list(base, info.name);
      setList(r.list); setItems(r.items || []); setAsks(r.asks || []);
      seq.current = r.list.seq; setError(""); setLoaded(true);
    } catch (e) { setError(msg(e)); setLoaded(true); }
  }, [base, info.name]);
  useEffect(() => {
    load();
    store.me().then((m) => setMe(m[base] || ""));
    // Who the computer says this phone is, and whether it may change the list.
    api.whoami(base).then(async (w) => {
      if (!w) return;
      if (w.name) { setMe(w.name); const m = await store.me(); m[base] = w.name; await store.setMe(m); }
      setReadOnly(w.can_write === false);
    }).catch(() => {});
  }, [load, base]);

  // Stay current while on screen: the change feed every 5 s.
  useEffect(() => {
    const t = setInterval(async () => {
      if (AppState.currentState !== "active") return;
      try {
        const ch = await api.changes(base, info.name, seq.current);
        if (ch.reset) { load(); return; }
        if (ch.events && ch.events.length) setItems((xs) => applyChanges(xs, ch.events));
        if (typeof ch.seq === "number") seq.current = ch.seq;
        setError("");
      } catch (e) { setError(msg(e)); }
    }, 5000);
    return () => clearInterval(t);
  }, [base, info.name, load]);

  const upsert = (it: Item) => setItems((xs) => applyChanges(xs, [{ seq: Number.MAX_SAFE_INTEGER, at: "", by: "", op: "update", item: it }]));
  const guard = async (f: () => Promise<Item | void>, ok?: string) => {
    try { const it = await f(); if (it) upsert(it); if (ok) toast(ok); return true; }
    catch (e) {
      if (e instanceof ApiError && e.status === 403) setReadOnly(true);
      Alert.alert("Couldn't do that", msg(e)); return false;
    }
  };
  const addOne = async () => {
    const t = draft.trim();
    if (!t) return;
    setDraft("");
    const ok = await guard(async () => {
      return api.add(base, info.name, { title: t, state: "open" });
    });
    if (!ok) setDraft(t);
  };
  const toggleDone = (it: Item) => guard(() => api.patch(base, info.name, it.id, { state: it.state === "done" ? "open" : "done" }), it.state === "done" ? "Back on the list" : "Done");

  const sec = useMemo(() => sections(items), [items]);
  const now = new Date();
  const selected = items.find((x) => x.id === sel) || null;

  const Row = ({ it }: { it: Item }) => {
    const b = dueBucket(it.due, now), done = it.state === "done" || it.state === "dropped";
    const helpOpen = (it.help || []).filter((h) => h.state !== "completed" && h.state !== "failed" && h.state !== "canceled").length;
    const lastC = (it.comments || [])[(it.comments || []).length - 1];
    const asked = !!lastC && !!list.session && (lastC.by === list.session || lastC.by.endsWith("/" + list.session));
    return (
      <Pressable style={s.row} onPress={() => setSel(it.id)}>
        <Pressable hitSlop={10} onPress={() => toggleDone(it)} style={[s.check, done && { backgroundColor: C.accent, borderColor: C.accent }]}>
          {done && <Text style={{ color: "#fff", fontSize: 13, fontWeight: "700" }}>✓</Text>}
        </Pressable>
        <View style={{ flex: 1 }}>
          <Text style={[s.body, done && { color: C.faint, textDecorationLine: "line-through" }]}>{it.title}</Text>
          {(() => {
            const bits = [
              it.due ? dueLabel(it.due, now) : "",
              it.who || "",
              ...(it.tags || []).map((t) => "#" + t),
              (it.comments || []).length ? `${(it.comments || []).length} 💬` : "",
              helpOpen ? "help on the way" : "",
            ].filter(Boolean);
            return bits.length ? <Text style={[s.sub, !done && (b === "overdue" || b === "today") && { color: C.warn }]} numberOfLines={1}>{bits.join(" · ")}</Text> : null;
          })()}
          {asked && !done && <Text style={[s.sub, { color: C.blue }]} numberOfLines={1}>The assistant asked: {lastC!.text}</Text>}
        </View>
      </Pressable>
    );
  };
  const Section = ({ title, list: xs }: { title: string; list: Item[] }) => xs.length ? (
    <View style={{ gap: 6 }}>
      <Text style={s.groupLabel}>{title}</Text>
      <View style={s.card0}>{xs.map((it, i) => <View key={it.id} style={i > 0 && s.sep}><Row it={it} /></View>)}</View>
    </View>
  ) : null;

  return (
    <View style={{ flex: 1 }}>
      <View style={s.header}>
        <Pressable hitSlop={10} onPress={onBack}><Text style={s.headerLink}>‹ Lists</Text></Pressable>
        <Text style={s.screenTitle} numberOfLines={1}>{list.title || list.name}</Text>
        <View style={{ width: 50 }} />
      </View>
      {!readOnly && (
        <View style={s.addBar}>
          <TextInput style={[s.input, { flex: 1 }]} value={draft} onChangeText={setDraft} placeholder="Add something…" placeholderTextColor={C.faint}
            onSubmitEditing={addOne} returnKeyType="done" blurOnSubmit={false} />
          <Pressable onPress={addOne} disabled={!draft.trim()} style={[s.primaryBtn, { paddingHorizontal: 16 }, !draft.trim() && { opacity: 0.5 }]}><Text style={s.primaryBtnT}>Add</Text></Pressable>
        </View>
      )}
      <ScrollView contentContainerStyle={{ padding: 16, paddingTop: 8, gap: 14 }} refreshControl={<RefreshControl refreshing={false} onRefresh={load} />} keyboardShouldPersistTaps="handled">
        {!!error && <Text style={[s.sub, { color: C.warn }]}>{error}</Text>}
        {readOnly && <Text style={s.hint}>This phone can look at the list but not change it.</Text>}
        {asks.map((a) => (
          <View key={a.from} style={[s.card, { backgroundColor: C.accentSoft }]}>
            <Text style={s.body}>{a.from} would like to suggest things for this list.</Text>
            {!!a.title && <Text style={s.sub}>First suggestion: “{a.title}”</Text>}
            <View style={{ flexDirection: "row", gap: 18, marginTop: 8 }}>
              <Pressable onPress={() => guard(async () => { await api.allow(base, info.name, a.from); load(); }, "Allowed")}><Text style={{ color: C.accent, fontWeight: "600" }}>Allow</Text></Pressable>
              <Pressable onPress={() => guard(async () => { await api.ignore(base, info.name, a.from); load(); }, "Ignored")}><Text style={{ color: C.sub }}>Ignore</Text></Pressable>
            </View>
          </View>
        ))}
        {loaded && !error && sec.open.length + sec.waiting.length + sec.inbox.length === 0 && (
          <Text style={[s.sub, { textAlign: "center", marginTop: 20 }]}>Nothing to do. Add something above.</Text>
        )}
        <Section title="To do" list={sec.open} />
        <Section title="Waiting" list={sec.waiting} />
        <Section title="New" list={sec.inbox} />
        {sec.suggested.length > 0 && (
          <Pressable onPress={() => setShowSuggested(!showSuggested)}><Text style={s.fold}>{showSuggested ? "Hide" : "Show"} suggestions ({sec.suggested.length})</Text></Pressable>
        )}
        {showSuggested && <Section title="Suggested" list={sec.suggested} />}
        {sec.done.length > 0 && (
          <Pressable onPress={() => setShowDone(!showDone)}><Text style={s.fold}>{showDone ? "Hide" : "Show"} done ({sec.done.length})</Text></Pressable>
        )}
        {showDone && <Section title="Done" list={sec.done} />}
      </ScrollView>
      <Modal visible={!!selected} animationType="slide" onRequestClose={() => setSel(null)}>
        {selected && <ItemSheet base={base} list={list} item={selected} me={me} readOnly={readOnly} items={items}
          onClose={() => setSel(null)} onChanged={upsert} onReadOnly={() => setReadOnly(true)} />}
      </Modal>
    </View>
  );
}

// ── one item ─────────────────────────────────────────────────────────────────
const STATES: { s: State; label: string }[] = [
  { s: "open", label: "To do" }, { s: "waiting", label: "Waiting" }, { s: "done", label: "Done" }, { s: "dropped", label: "Not doing" },
];

function ItemSheet({ base, list, item, me, readOnly, items, onClose, onChanged, onReadOnly }: {
  base: string; list: ListInfo; item: Item; me: string; readOnly: boolean; items: Item[];
  onClose: () => void; onChanged: (it: Item) => void; onReadOnly: () => void;
}) {
  const [title, setTitle] = useState(item.title);
  const [notes, setNotes] = useState(item.notes || "");
  const [who, setWho] = useState(item.who || "");
  const [tags, setTags] = useState((item.tags || []).join(", "));
  const [picker, setPicker] = useState(false);
  const [reply, setReply] = useState("");
  const [askKind, setAskKind] = useState<string | null>(null);
  const [askText, setAskText] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => { setTitle(item.title); setNotes(item.notes || ""); setWho(item.who || ""); setTags((item.tags || []).join(", ")); }, [item.id]);   // eslint-disable-line

  const now = new Date();
  const knownWho = useMemo(() => [...new Set(items.map((x) => (x.who || "").trim()).filter(Boolean))].slice(0, 6), [items]);
  const run = async (f: () => Promise<Item>, ok?: string) => {
    if (busy) return false;
    setBusy(true);
    try { onChanged(await f()); if (ok) toast(ok); return true; }
    catch (e) { if (e instanceof ApiError && e.status === 403) onReadOnly(); Alert.alert("Couldn't do that", msg(e)); return false; }
    finally { setBusy(false); }
  };
  const patch = (p: Parameters<typeof api.patch>[3], ok?: string) => run(() => api.patch(base, list.name, item.id, p), ok);
  const tagList = (t: string) => t.split(",").map((x) => x.trim().replace(/^#/, "")).filter(Boolean);
  const dirty = title.trim() !== item.title || notes.trim() !== (item.notes || "") || who.trim() !== (item.who || "")
    || tagList(tags).join(",") !== (item.tags || []).join(",");
  const save = async () => {
    if (!title.trim()) { Alert.alert("It needs a name"); return; }
    const p: any = {};
    if (title.trim() !== item.title) p.title = title.trim();
    if (notes.trim() !== (item.notes || "")) p.notes = notes.trim();
    if (who.trim() !== (item.who || "")) p.who = who.trim();
    if (tagList(tags).join(",") !== (item.tags || []).join(",")) p.tags = tagList(tags);
    await patch(p, "Saved");
  };
  const setDue = (d: string) => patch({ due: d }, d ? "Date set" : "Date removed");
  const send = async () => { const t = reply.trim(); if (!t) return; if (await run(() => api.comment(base, list.name, item.id, t))) setReply(""); };
  const askHelp = async () => {
    if (!askKind) return;
    if (await run(() => api.ask(base, list.name, item.id, askKind, askText.trim() || undefined), "Asked")) { setAskKind(null); setAskText(""); }
  };

  return (
    <SafeAreaView style={{ flex: 1, backgroundColor: C.bg }}>
      <View style={s.header}>
        <Pressable hitSlop={10} onPress={() => { if (dirty) Alert.alert("Save your changes?", "", [{ text: "Discard", style: "destructive", onPress: onClose }, { text: "Save", onPress: async () => { await save(); onClose(); } }]); else onClose(); }}>
          <Text style={s.headerLink}>‹ {list.title || list.name}</Text>
        </Pressable>
        {!readOnly && dirty ? <Pressable hitSlop={10} onPress={save}><Text style={[s.headerLink, { fontWeight: "700" }]}>Save</Text></Pressable> : <View style={{ width: 40 }} />}
      </View>
      <KeyboardAvoidingView style={{ flex: 1 }} behavior={Platform.OS === "ios" ? "padding" : undefined}>
        <ScrollView contentContainerStyle={{ padding: 16, gap: 12 }} keyboardShouldPersistTaps="handled">
          <TextInput style={s.titleInput} value={title} onChangeText={setTitle} editable={!readOnly} multiline />
          <View style={s.chips}>
            {STATES.map((x) => (
              <Pressable key={x.s} onPress={() => !readOnly && item.state !== x.s && patch({ state: x.s })}
                style={[s.chip, item.state === x.s && { backgroundColor: C.accent, borderColor: C.accent }]}>
                <Text style={[s.chipT, item.state === x.s && { color: "#fff" }]}>{x.label}</Text>
              </Pressable>
            ))}
          </View>
          {item.state === "suggested" && !readOnly && (
            <View style={[s.card, { backgroundColor: C.accentSoft }]}>
              <Text style={s.body}>Suggested by {whoLabel(item.suggested_by, list.session, me)}.</Text>
              <View style={{ flexDirection: "row", gap: 18, marginTop: 6 }}>
                <Pressable onPress={() => patch({ state: "open" }, "Added to the list")}><Text style={{ color: C.accent, fontWeight: "600" }}>Add to the list</Text></Pressable>
                <Pressable onPress={() => patch({ state: "dropped" }, "Dismissed")}><Text style={{ color: C.sub }}>No thanks</Text></Pressable>
              </View>
            </View>
          )}

          <Text style={s.label}>When</Text>
          <View style={s.chips}>
            <Text style={[s.body, { marginRight: 6 }, (dueBucket(item.due, now) === "overdue") && { color: C.warn }]}>{item.due ? dueLabel(item.due, now) : "No date"}</Text>
            {!readOnly && <>
              <Pressable style={s.chip} onPress={() => setDue(isoDay(now))}><Text style={s.chipT}>Today</Text></Pressable>
              <Pressable style={s.chip} onPress={() => setDue(isoDay(new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1)))}><Text style={s.chipT}>Tomorrow</Text></Pressable>
              <Pressable style={s.chip} onPress={() => setPicker(true)}><Text style={s.chipT}>Pick…</Text></Pressable>
              {!!item.due && <Pressable style={s.chip} onPress={() => setDue("")}><Text style={s.chipT}>Clear</Text></Pressable>}
            </>}
          </View>
          {picker && (
            <DateTimePicker value={(item.due && /^\d{4}-\d{2}-\d{2}$/.test(item.due)) ? new Date(item.due + "T12:00:00") : now} mode="date"
              onChange={(e, d) => { setPicker(false); if (e.type === "set" && d) setDue(isoDay(d)); }} />
          )}

          <Text style={s.label}>Who</Text>
          <TextInput style={s.input} value={who} onChangeText={setWho} editable={!readOnly} placeholder="Anyone" placeholderTextColor={C.faint} />
          {!readOnly && knownWho.length > 0 && (
            <View style={s.chips}>{knownWho.map((w) => <Pressable key={w} style={[s.chip, who === w && { borderColor: C.accent }]} onPress={() => setWho(w)}><Text style={s.chipT}>{w}</Text></Pressable>)}</View>
          )}
          <Text style={s.label}>Tags</Text>
          <TextInput style={s.input} value={tags} onChangeText={setTags} editable={!readOnly} placeholder="e.g. car, shopping" placeholderTextColor={C.faint} autoCapitalize="none" />
          <Text style={s.label}>Notes</Text>
          <TextInput style={[s.input, { minHeight: 70, textAlignVertical: "top" }]} value={notes} onChangeText={setNotes} editable={!readOnly} multiline placeholder="Anything worth remembering" placeholderTextColor={C.faint} />
          {!readOnly && dirty && <Pressable onPress={save} style={s.primaryBtn}><Text style={s.primaryBtnT}>Save</Text></Pressable>}

          {/* help */}
          {(item.help || []).length > 0 && <Text style={s.label}>Help</Text>}
          {(item.help || []).map((h, i) => (
            <View key={(h.task || "") + i} style={s.card}>
              <Text style={s.body}>{cap(h.kind)}{h.ask ? `: ${h.ask}` : ""}</Text>
              <Text style={[s.sub, h.state === "completed" && { color: C.accent }]}>{h.state === "completed" ? "Done" : h.state === "failed" ? "Couldn't be done" : "Working on it…"}</Text>
              {!!h.answer && <Text style={[s.body, { marginTop: 6 }]} selectable>{h.answer}</Text>}
            </View>
          ))}
          {!readOnly && !!list.session && (
            <View style={{ gap: 8 }}>
              <Text style={s.label}>Ask the assistant to help</Text>
              <View style={s.chips}>{HELP_KINDS.map((k) => (
                <Pressable key={k.kind} style={[s.chip, askKind === k.kind && { backgroundColor: C.accent, borderColor: C.accent }]} onPress={() => setAskKind(askKind === k.kind ? null : k.kind)}>
                  <Text style={[s.chipT, askKind === k.kind && { color: "#fff" }]}>{k.label}</Text>
                </Pressable>
              ))}</View>
              {askKind && <>
                <TextInput style={s.input} value={askText} onChangeText={setAskText} placeholder={HELP_KINDS.find((k) => k.kind === askKind)!.hint} placeholderTextColor={C.faint} />
                <Pressable onPress={askHelp} disabled={busy} style={[s.primaryBtn, busy && { opacity: 0.5 }]}><Text style={s.primaryBtnT}>Ask</Text></Pressable>
              </>}
            </View>
          )}

          {/* conversation */}
          <Text style={s.label}>Conversation</Text>
          {(item.comments || []).length === 0 && <Text style={s.sub}>No messages yet.</Text>}
          {(item.comments || []).map((c, i) => {
            const mine = !!me && c.by === me;
            const bot = !!list.session && (c.by === list.session || c.by.endsWith("/" + list.session));
            return (
              <View key={i} style={[s.bubble, mine ? s.bubbleMine : bot ? s.bubbleBot : null]}>
                <Text style={s.bubbleWho}>{cap(whoLabel(c.by, list.session, me))}</Text>
                <Text style={s.body} selectable>{c.text}</Text>
              </View>
            );
          })}
          {!readOnly && (
            <View style={{ flexDirection: "row", gap: 8 }}>
              <TextInput style={[s.input, { flex: 1 }]} value={reply} onChangeText={setReply} placeholder={list.session ? "Write to the assistant or the others…" : "Write a note…"} placeholderTextColor={C.faint} multiline />
              <Pressable onPress={send} disabled={!reply.trim() || busy} style={[s.primaryBtn, { paddingHorizontal: 14, alignSelf: "flex-end" }, (!reply.trim() || busy) && { opacity: 0.5 }]}><Text style={s.primaryBtnT}>Send</Text></Pressable>
            </View>
          )}
          <Text style={[s.hint, { marginTop: 8 }]}>Added by {whoLabel(item.added_by, list.session, me) || "someone"}{item.suggested_by ? `, suggested by ${whoLabel(item.suggested_by, list.session, me)}` : ""}.</Text>
        </ScrollView>
      </KeyboardAvoidingView>
    </SafeAreaView>
  );
}

const s = StyleSheet.create({
  header: { flexDirection: "row", alignItems: "center", justifyContent: "space-between", paddingHorizontal: 16, paddingVertical: 12, gap: 10 },
  appTitle: { fontSize: 26, fontWeight: "700", color: C.text, letterSpacing: 0.2 },
  screenTitle: { fontSize: 18, fontWeight: "600", color: C.text, flex: 1, textAlign: "center" },
  headerLink: { fontSize: 15, color: C.accent },
  groupLabel: { fontSize: 12, fontWeight: "600", color: C.sub, textTransform: "uppercase", letterSpacing: 0.8, marginTop: 4 },
  card: { backgroundColor: C.card, borderRadius: 12, padding: 14, borderWidth: 1, borderColor: C.line },
  card0: { backgroundColor: C.card, borderRadius: 12, borderWidth: 1, borderColor: C.line, overflow: "hidden" },
  cardTitle: { fontSize: 18, fontWeight: "600", color: C.text, marginBottom: 4 },
  listCard: { backgroundColor: C.card, borderRadius: 12, padding: 16, borderWidth: 1, borderColor: C.line, gap: 2 },
  listTitle: { fontSize: 18, fontWeight: "600", color: C.text },
  body: { fontSize: 15, color: C.text, lineHeight: 21 },
  sub: { fontSize: 13, color: C.sub, lineHeight: 18 },
  hint: { fontSize: 12, color: C.faint, lineHeight: 17 },
  label: { fontSize: 12, fontWeight: "600", color: C.sub, textTransform: "uppercase", letterSpacing: 0.8, marginTop: 6 },
  input: { backgroundColor: C.card, borderWidth: 1, borderColor: C.line, borderRadius: 10, paddingHorizontal: 12, paddingVertical: 10, fontSize: 15, color: C.text },
  titleInput: { fontSize: 22, fontWeight: "600", color: C.text, paddingVertical: 4 },
  primaryBtn: { backgroundColor: C.accent, borderRadius: 10, paddingVertical: 11, alignItems: "center", justifyContent: "center" },
  primaryBtnT: { color: "#fff", fontSize: 15, fontWeight: "600" },
  addBar: { flexDirection: "row", gap: 8, paddingHorizontal: 16, paddingBottom: 8 },
  row: { flexDirection: "row", alignItems: "flex-start", gap: 12, paddingHorizontal: 14, paddingVertical: 12 },
  sep: { borderTopWidth: 1, borderTopColor: C.line },
  check: { width: 22, height: 22, borderRadius: 11, borderWidth: 2, borderColor: C.faint, alignItems: "center", justifyContent: "center", marginTop: 1 },
  fold: { fontSize: 14, color: C.accent, paddingVertical: 4 },
  chips: { flexDirection: "row", flexWrap: "wrap", gap: 8, alignItems: "center" },
  chip: { borderWidth: 1, borderColor: C.line, backgroundColor: C.card, borderRadius: 16, paddingHorizontal: 12, paddingVertical: 6 },
  chipT: { fontSize: 13, color: C.text },
  bubble: { backgroundColor: C.card, borderRadius: 12, padding: 10, borderWidth: 1, borderColor: C.line, maxWidth: "88%", alignSelf: "flex-start" },
  bubbleMine: { alignSelf: "flex-end", backgroundColor: C.accentSoft, borderColor: C.accentSoft },
  bubbleBot: { backgroundColor: "#EAF1F7", borderColor: "#EAF1F7" },
  bubbleWho: { fontSize: 11, color: C.sub, marginBottom: 2, fontWeight: "600" },
});

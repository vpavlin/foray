// A small markdown subset for what the assistant and other agents write: paragraphs, headings,
// - and 1. lists, block quotes, **bold**, *italic*, `code`, ``` code blocks and links. The text comes
// from models and other agents, so: images become their alt text (nothing is fetched), raw HTML is
// shown as text (never interpreted), and only http(s) URLs become links. Pure; tested in test/md.test.ts.

export type Inline =
  | { t: "text"; s: string }
  | { t: "b"; c: Inline[] }
  | { t: "i"; c: Inline[] }
  | { t: "code"; s: string }
  | { t: "link"; url: string; c: Inline[] };

export type Block =
  | { t: "p"; c: Inline[] }
  | { t: "h"; level: number; c: Inline[] }
  | { t: "ul"; items: Inline[][] }
  | { t: "ol"; start: number; items: Inline[][] }
  | { t: "quote"; c: Inline[] }
  | { t: "code"; s: string };

export const safeUrl = (u: string): string | null => {
  const url = u.trim().replace(/^<|>$/g, "");
  return /^https?:\/\/[^\s<>"']+$/i.test(url) ? url : null;
};

// ── inline ───────────────────────────────────────────────────────────────────
// The earliest match wins; ties go to the order below. A code span that starts first wins, so nothing
// inside `…` is parsed.
const INLINE: { re: RegExp; make: (m: RegExpExecArray) => Inline[] }[] = [
  { re: /\\([\\`*_[\]()#+\-.!>])/, make: (m) => [{ t: "text", s: m[1] }] },   // a backslash escape is that character
  { re: /`([^`\n]+)`/, make: (m) => [{ t: "code", s: m[1] }] },
  { re: /!\[([^\]\n]*)\]\(([^)\s]*)(?:\s+"[^"]*")?\)/, make: (m) => (m[1] ? [{ t: "text", s: m[1] }] : []) },   // image → its alt text
  { re: /\[([^\]\n]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/, make: (m) => {
      const url = safeUrl(m[2]);
      return url ? [{ t: "link", url, c: parseInline(m[1]) }] : parseInline(m[1]);   // not http(s): just its text
    } },
  { re: /<(https?:\/\/[^\s<>]+)>/, make: (m) => [{ t: "link", url: m[1], c: [{ t: "text", s: m[1] }] }] },
  { re: /\*\*(?=\S)([\s\S]*?\S)\*\*|__(?=\S)([\s\S]*?\S)__/, make: (m) => [{ t: "b", c: parseInline(m[1] ?? m[2]) }] },
  { re: /\*(?=[^\s*])([^*\n]*?[^\s*])\*/, make: (m) => [{ t: "i", c: parseInline(m[1]) }] },
  // _italic_ only at word edges (snake_case stays as it is); no lookbehind, which Hermes may lack.
  { re: /(^|[^A-Za-z0-9_])_(?=[^\s_])([^_\n]*?[^\s_])_(?![A-Za-z0-9_])/, make: (m) => [{ t: "text", s: m[1] }, { t: "i", c: parseInline(m[2]) }] },
  { re: /https?:\/\/[^\s<>()"']*[^\s<>()"'.,;:!?]/, make: (m) => [{ t: "link", url: m[0], c: [{ t: "text", s: m[0] }] }] },
];

export function parseInline(src: string): Inline[] {
  const out: Inline[] = [];
  let rest = src;
  const pushText = (s: string) => {
    if (!s) return;
    const last = out[out.length - 1];
    if (last && last.t === "text") last.s += s; else out.push({ t: "text", s });
  };
  while (rest) {
    let best: { idx: number; len: number; nodes: Inline[] } | null = null;
    for (const r of INLINE) {
      const m = r.re.exec(rest);
      if (m && (!best || m.index < best.idx)) best = { idx: m.index, len: m[0].length, nodes: r.make(m) };
    }
    if (!best) { pushText(rest); break; }
    pushText(rest.slice(0, best.idx));
    for (const n of best.nodes) n.t === "text" ? pushText(n.s) : out.push(n);
    rest = rest.slice(best.idx + best.len);
  }
  return out;
}

// ── blocks ───────────────────────────────────────────────────────────────────
const UL = /^\s{0,3}[-*+]\s+(.*)$/, OL = /^\s{0,3}(\d{1,9})[.)]\s+(.*)$/, H = /^\s{0,3}(#{1,6})\s+(.*?)\s*#*\s*$/;
const FENCE = /^\s{0,3}(```|~~~)/, QUOTE = /^\s{0,3}>\s?(.*)$/, RULE = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/;

export function parseMarkdown(src: string): Block[] {
  const lines = (src || "").replace(/\r\n?/g, "\n").split("\n");
  const blocks: Block[] = [];
  let para: string[] = [];
  const flush = () => { if (para.length) { blocks.push({ t: "p", c: parseInline(para.join("\n")) }); para = []; } };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fence = FENCE.exec(line);
    if (fence) {
      flush();
      const body: string[] = [];
      for (i++; i < lines.length && !lines[i].trim().startsWith(fence[1]); i++) body.push(lines[i]);
      blocks.push({ t: "code", s: body.join("\n") });
      continue;
    }
    if (!line.trim()) { flush(); continue; }
    if (RULE.test(line)) { flush(); continue; }   // a rule is just a break
    const h = H.exec(line);
    if (h) { flush(); blocks.push({ t: "h", level: h[1].length, c: parseInline(h[2]) }); continue; }
    const q = QUOTE.exec(line);
    if (q) {
      flush();
      const body = [q[1]];
      while (i + 1 < lines.length && QUOTE.test(lines[i + 1])) body.push(QUOTE.exec(lines[++i])![1]);
      blocks.push({ t: "quote", c: parseInline(body.join("\n")) });
      continue;
    }
    const ul = UL.exec(line), ol = OL.exec(line);
    if (ul || ol) {
      flush();
      const ordered = !!ol && !ul;
      const items: string[][] = [[(ordered ? ol![2] : ul![1])]];
      // Further items, and indented continuation lines of the current one.
      while (i + 1 < lines.length) {
        const nx = lines[i + 1];
        const nul = UL.exec(nx), nol = OL.exec(nx);
        if (ordered ? (nol && !nul) : nul) { items.push([ordered ? nol![2] : nul![1]]); i++; continue; }
        if (nx.trim() && /^\s{2,}/.test(nx) && !FENCE.test(nx)) { items[items.length - 1].push(nx.trim()); i++; continue; }
        break;
      }
      const inl = items.map((it) => parseInline(it.join("\n")));
      blocks.push(ordered ? { t: "ol", start: Number(ol![1]), items: inl } : { t: "ul", items: inl });
      continue;
    }
    para.push(line);
  }
  flush();
  return blocks;
}

// ── plain text, for one-line previews and notifications ──────────────────────
const plainInline = (xs: Inline[]): string => xs.map((x) => (x.t === "text" || x.t === "code" ? x.s : plainInline(x.c))).join("");
export function stripMarkdown(src: string): string {
  return parseMarkdown(src).map((b) =>
    b.t === "code" ? b.s
      : b.t === "ul" || b.t === "ol" ? b.items.map(plainInline).join("; ")
      : plainInline(b.c),
  ).join(" ").replace(/\s+/g, " ").trim();
}

/** One plain line for a list row: the first paragraph or list (a leading heading is skipped). */
export function previewLine(src: string, max = 140): string {
  const blocks = parseMarkdown(src);
  const b = blocks.find((x) => x.t === "p" || x.t === "ul" || x.t === "ol" || x.t === "quote") || blocks[0];
  if (!b) return "";
  const text = b.t === "code" ? b.s : b.t === "ul" || b.t === "ol" ? b.items.map(plainInline).join("; ") : plainInline(b.c);
  const one = text.replace(/\s+/g, " ").trim();
  return one.length > max ? one.slice(0, max - 1).trimEnd() + "…" : one;
}

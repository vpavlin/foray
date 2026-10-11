import assert from "node:assert/strict";
import { parseMarkdown, parseInline, stripMarkdown, safeUrl, previewLine } from "../src/md.ts";

const j = (x: unknown) => JSON.stringify(x);
// inline
assert.equal(j(parseInline("a **b** c")), j([{ t: "text", s: "a " }, { t: "b", c: [{ t: "text", s: "b" }] }, { t: "text", s: " c" }]));
assert.equal(j(parseInline("*it* and _it_")), j([{ t: "i", c: [{ t: "text", s: "it" }] }, { t: "text", s: " and " }, { t: "i", c: [{ t: "text", s: "it" }] }]));
assert.equal(j(parseInline("snake_case_name")), j([{ t: "text", s: "snake_case_name" }]), "snake_case is not italic");
assert.equal(j(parseInline("2 * 3 * 4")), j([{ t: "text", s: "2 * 3 * 4" }]), "spaced asterisks are not italic");
assert.equal(j(parseInline("use `a **b**`")), j([{ t: "text", s: "use " }, { t: "code", s: "a **b**" }]), "nothing parsed inside code");
assert.equal(j(parseInline("[Alza](https://alza.cz/x)")), j([{ t: "link", url: "https://alza.cz/x", c: [{ t: "text", s: "Alza" }] }]));
const js = parseInline("[click](javascript:alert(1))");
assert.ok(!js.some((n) => n.t === "link"), "javascript: is never a link");
assert.ok(!parseInline("[f](file:///etc/passwd)").some((n) => n.t === "link"), "file: is never a link");
assert.equal(j(parseInline("see ![a tyre](https://evil/x.png) here")), j([{ t: "text", s: "see a tyre here" }]), "images become alt text");
assert.equal(j(parseInline("![](https://evil/x.png)")), j([]), "an image without alt text disappears");
assert.equal(j(parseInline("<b>hi</b><img src=x onerror=y>")), j([{ t: "text", s: "<b>hi</b><img src=x onerror=y>" }]), "raw HTML stays text");
assert.equal(j(parseInline("go to https://example.com/a.")), j([{ t: "text", s: "go to " }, { t: "link", url: "https://example.com/a", c: [{ t: "text", s: "https://example.com/a" }] }, { t: "text", s: "." }]), "bare URL, trailing dot outside");
assert.equal(j(parseInline("\\*not bold\\*")), j([{ t: "text", s: "*not bold*" }]));
assert.equal(safeUrl(" https://a.b/c "), "https://a.b/c");
assert.equal(safeUrl("mailto:x@y"), null);

// blocks
const md = `# Garages open Saturday

Two are **open**:

- Pneu Praha, *Holešovice*: 8–12
- AutoCentrum: 9–13
  call first

1. Book online
2. Bring the [wheel lock](https://example.com/lock)

> Prices from their sites.

\`\`\`
Dear garage,
**not bold here**
\`\`\`
---
Done.`;
const b = parseMarkdown(md);
assert.deepEqual(b.map((x) => x.t), ["h", "p", "ul", "ol", "quote", "code", "p"]);
assert.equal((b[0] as any).level, 1);
assert.equal((b[2] as any).items.length, 2);
assert.equal(j((b[2] as any).items[1]), j([{ t: "text", s: "AutoCentrum: 9–13\ncall first" }]), "indented continuation joins its item");
assert.equal((b[3] as any).start, 1);
assert.equal((b[5] as any).s, "Dear garage,\n**not bold here**", "code blocks are verbatim");
assert.deepEqual(parseMarkdown(""), []);
assert.deepEqual(parseMarkdown("one\ntwo").map((x) => x.t), ["p"], "lines join into one paragraph");

// plain preview
assert.equal(stripMarkdown("**Which** car? See [the list](https://x.y) or ![img](https://e/p.png)"), "Which car? See the list or img");
assert.equal(stripMarkdown("- a\n- b"), "a; b");
assert.equal(previewLine("## Gist\n\nTwo garages are **open** Saturday.\n\n- [A](https://a.b)"), "Two garages are open Saturday.");
assert.equal(previewLine("# Only a heading"), "Only a heading");
assert.equal(previewLine(""), "");
assert.equal(previewLine("x".repeat(200)).length, 140);
console.log("MD OK");

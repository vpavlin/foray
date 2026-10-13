"""The list's session reads the web for itself: an MCP server (stdio) with
web_search and read_page, over Camoufox — a Firefox that sites take for a
person's, so a search or a shop page answers instead of showing a captcha.

Run by bin/foray-browser in the session's cage, which keeps its Python and
its browser in the session's directory. One browser for the process,
started at the first call; pages are read as text, without images, fonts or
media, and cut to a length a small model can take.

Searches are in a DuckDuckGo region (its kl code, country then language:
cz-cs, de-de, us-en, uk-en; wt-wt is none). The session passes the one that
fits the list's context; without one, the default is --region (setup.sh's,
in the session's .pi/mcp.json), else FORAY_SEARCH_REGION, else wt-wt.
"""

import json
import os
import re
import sys
import urllib.parse

MAX_CHARS = 8000
NO_REGION = "wt-wt"
browser = None
context = None


def valid_region(r):
    return bool(re.fullmatch(r"[a-z]{2}-[a-z]{2}", (r or "").strip().lower()))


def default_region(argv=None, env=None):
    argv = sys.argv[1:] if argv is None else argv
    env = os.environ if env is None else env
    for i, a in enumerate(argv):
        if a == "--region" and i + 1 < len(argv) and valid_region(argv[i + 1]):
            return argv[i + 1].strip().lower()
        if a.startswith("--region=") and valid_region(a[9:]):
            return a[9:].strip().lower()
    r = env.get("FORAY_SEARCH_REGION", "")
    return r.strip().lower() if valid_region(r) else NO_REGION


def pick_region(r):
    # An unknown or empty region is the default, not an error: a small model
    # may pass "Czechia" or nothing at all.
    return r.strip().lower() if valid_region(r) else DEFAULT_REGION


def languages(region):
    # A region's Accept-Language: "cz-cs" is cs-CZ, then English. None for
    # no region, or one that is not a country (xa-ar, xl-es).
    country, lang = region.split("-")
    if region == NO_REGION or country in ("wt", "xa", "xl"):
        return None
    country = {"uk": "GB"}.get(country, country.upper())
    return f"{lang}-{country},{lang};q=0.9" + (",en;q=0.8" if lang != "en" else "")


DEFAULT_REGION = default_region()


def start():
    global browser, context
    if context is None:
        from camoufox.sync_api import Camoufox
        lang = languages(DEFAULT_REGION)
        locale = lang.split(",")[0] + ",en-US" if lang else "en-US"
        browser = Camoufox(headless=True, humanize=False, locale=locale).__enter__()
        context = browser.new_context()
        context.route("**/*", lambda route: route.abort()
                      if route.request.resource_type in ("image", "media", "font")
                      else route.continue_())
    return context


def page_text(page):
    # The main part of the page when it has one, the whole of it otherwise.
    return page.evaluate("""() => {
        for (const sel of ['main', 'article', '[role=main]', '#content', 'body']) {
            const el = document.querySelector(sel)
            if (el && el.innerText && el.innerText.trim().length > 200) return el.innerText
        }
        return document.body ? document.body.innerText : ''
    }""")


def tidy(text, limit):
    text = re.sub(r"[ \t ]+", " ", text or "")
    text = re.sub(r"\n\s*\n+", "\n\n", text).strip()
    if len(text) > limit:
        text = text[:limit] + f"\n\n[cut at {limit} characters of {len(text)}; ask read_page for a part with start]"
    return text


def read_page(url, start_at=0, max_chars=MAX_CHARS):
    if not re.match(r"^https?://", url or ""):
        raise ValueError("only http and https pages")
    page = start().new_page()
    try:
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        try:
            page.wait_for_load_state("networkidle", timeout=5000)
        except Exception:
            pass
        text = page_text(page)[int(start_at or 0):]
        return f"{page.title()}\n{page.url}\n\n" + tidy(text, int(max_chars or MAX_CHARS))
    finally:
        page.close()


def unwrap(href):
    # DuckDuckGo's results go through its own redirect: the target is in uddg.
    q = urllib.parse.urlparse(href)
    if "duckduckgo.com" in q.netloc and q.path.startswith("/l/"):
        return urllib.parse.parse_qs(q.query).get("uddg", [href])[0]
    return href if not href.startswith("//") else "https:" + href


def web_search(query, region=None):
    region = pick_region(region)
    page = start().new_page()
    try:
        lang = languages(region)
        if lang:
            page.set_extra_http_headers({"Accept-Language": lang})
        url = "https://html.duckduckgo.com/html/?" + urllib.parse.urlencode({"q": query, "kl": region})
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        found = page.evaluate("""() => [...document.querySelectorAll('.result')].map(r => {
            const a = r.querySelector('a.result__a')
            const s = r.querySelector('.result__snippet')
            return a ? {title: a.innerText.trim(), href: a.getAttribute('href'), snippet: s ? s.innerText.trim() : ''} : null
        }).filter(Boolean)""")
        lines = []
        for i, r in enumerate(found[:8], 1):
            lines.append(f"{i}. {r['title']}\n   {unwrap(r['href'])}\n   {r['snippet']}")
        return f"region {region}\n" + ("\n".join(lines) or "nothing found")
    finally:
        page.close()


TOOLS = [
    {"name": "web_search",
     "description": "Search the web (DuckDuckGo). Returns up to 8 results: title, URL, snippet. Read the ones that matter with read_page. Give region for where the list's people live, from get_context.",
     "inputSchema": {"type": "object", "properties": {
         "query": {"type": "string", "description": "what to search for, in the words a person would use"},
         "region": {"type": "string", "description": "DuckDuckGo region, country then language: cz-cs (Czechia), de-de, fr-fr, uk-en, us-en; wt-wt for none. Without it: " + DEFAULT_REGION}},
         "required": ["query"]}},
    {"name": "read_page",
     "description": "Open a web page in a real browser and return its title, address and text (the main part; up to 8000 characters, more with start).",
     "inputSchema": {"type": "object", "properties": {
         "url": {"type": "string", "description": "http or https"},
         "start": {"type": "integer", "description": "skip this many characters, to read further"},
         "max_chars": {"type": "integer", "description": "at most this many characters (8000)"}},
         "required": ["url"]}},
]


def call(name, args):
    if os.environ.get("FORAY_BROWSER_NOT_READY"):
        raise RuntimeError("the browser is still being installed (a few minutes, the first time): "
                           "try again later, or ask another agent meanwhile")
    if name == "web_search":
        return web_search(args.get("query", ""), args.get("region"))
    if name == "read_page":
        return read_page(args.get("url", ""), args.get("start", 0), args.get("max_chars", MAX_CHARS))
    raise ValueError(f"no tool {name}")


def reply(id_, result=None, error=None):
    msg = {"jsonrpc": "2.0", "id": id_}
    if error is not None:
        msg["error"] = {"code": -32601, "message": error}
    else:
        msg["result"] = result
    sys.stdout.write(json.dumps(msg, ensure_ascii=False) + "\n")
    sys.stdout.flush()


def main():
    for line in sys.stdin:
        if not line.strip():
            continue
        try:
            req = json.loads(line)
        except ValueError:
            continue
        method, id_ = req.get("method"), req.get("id")
        if id_ is None:
            continue  # a notification
        if method == "initialize":
            reply(id_, {"protocolVersion": req.get("params", {}).get("protocolVersion", "2024-11-05"),
                        "capabilities": {"tools": {}},
                        "serverInfo": {"name": "foray-browser", "version": "0.1.0"},
                        "instructions": "A browser for looking things up yourself: web_search, then read_page on what matters."})
        elif method == "tools/list":
            reply(id_, {"tools": TOOLS})
        elif method == "tools/call":
            p = req.get("params", {})
            try:
                text, err = call(p.get("name"), p.get("arguments") or {}), False
            except Exception as e:
                text, err = f"{type(e).__name__}: {e}", True
            reply(id_, {"content": [{"type": "text", "text": text}], "isError": err})
        elif method == "ping":
            reply(id_, {})
        else:
            reply(id_, error=f"no method {method}")


if __name__ == "__main__":
    main()

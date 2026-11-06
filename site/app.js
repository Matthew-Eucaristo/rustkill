// rustkill site: an interactive replica of the TUI, plus small page helpers.
// Plain JS, no dependencies, no network. The items come from sample.js, which
// site/build.sh generates by running `rustkill scan --json` on the workspace
// demo/setup.sh builds: generic sample data, never a real machine. The replica
// runs in --dry-run mode, like the recording: nothing is ever removed.
(() => {
  "use strict";
  document.documentElement.classList.add("js");
  const reduceMotion = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const $ = (s, el = document) => el.querySelector(s);
  const $$ = (s, el = document) => [...el.querySelectorAll(s)];

  /* ---------- copy buttons ---------- */
  const copy = async (text) => {
    try { await navigator.clipboard.writeText(text); }
    catch {
      const t = Object.assign(document.createElement("textarea"), { value: text });
      document.body.append(t); t.select(); document.execCommand("copy"); t.remove();
    }
  };
  $$("[data-copy]").forEach((b) => b.addEventListener("click", async () => {
    await copy(b.dataset.copy);
    b.classList.add("done");
    clearTimeout(b._t); b._t = setTimeout(() => b.classList.remove("done"), 1600);
  }));

  /* ---------- install tabs (ARIA tabs pattern) ---------- */
  const tabs = $$('[role="tab"]');
  const pick = (t) => {
    tabs.forEach((x) => {
      const on = x === t;
      x.setAttribute("aria-selected", on); x.tabIndex = on ? 0 : -1;
      $("#" + x.getAttribute("aria-controls")).hidden = !on;
    });
    t.focus();
  };
  tabs.forEach((t, i) => {
    t.addEventListener("click", () => pick(t));
    t.addEventListener("keydown", (e) => {
      const d = e.key === "ArrowRight" ? 1 : e.key === "ArrowLeft" ? -1 : 0;
      if (d) { e.preventDefault(); pick(tabs[(i + d + tabs.length) % tabs.length]); }
    });
  });

  /* ---------- reveal on scroll ---------- */
  if (!reduceMotion && "IntersectionObserver" in window) {
    const io = new IntersectionObserver((es) => es.forEach((e) => {
      if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
    }), { rootMargin: "0px 0px -8% 0px" });
    $$(".sec h2, .sec .intro, .cat, .lvcard, .guards article, .checks li, #sizes .window, #use .window, .family, .usage, .codes, .platforms div, .recording").forEach((el, i) => {
      el.classList.add("reveal"); el.style.transitionDelay = (i % 4) * 60 + "ms"; io.observe(el);
    });
  }

  /* ---------- recording: video, GIF fallback ---------- */
  const video = $("#demo-video"), play = $("#play");
  if (video) {
    const toGif = () => {
      const img = Object.assign(new Image(1280, 580), { src: "demo.gif", alt: video.getAttribute("aria-label"), loading: "lazy" });
      video.replaceWith(img); play.hidden = true;
    };
    const srcs = $$("source", video);
    srcs[srcs.length - 1].addEventListener("error", toGif);
    const start = (manual) => { play.hidden = true; if (manual === true) video.controls = true; video.play().catch(() => { play.hidden = false; }); };
    play.addEventListener("click", () => start(true));
    video.addEventListener("click", () => { if (!video.controls) { if (video.paused) video.play(); else video.pause(); } });
    const saveData = navigator.connection && navigator.connection.saveData;
    if (!reduceMotion && !saveData && "IntersectionObserver" in window) {
      const vio = new IntersectionObserver((es) => es.forEach((e) => {
        if (e.isIntersecting) start(); else if (!video.paused) video.pause();
      }), { threshold: 0.5 });
      vio.observe(video);
    }
  }

  /* =====================================================================
     Formatting, as internal/format and internal/report do it
     ===================================================================== */
  const DATA = window.RUSTKILL_SAMPLE;
  const SCAN = DATA && DATA.scan;
  const NOW = SCAN ? Date.parse(SCAN.generated_at) : Date.now(); // ages as of the scan
  const DAY = 86400e3;
  const bytes = (n) => {
    if (n < 0) return "…";
    if (n < 1000) return n + " B";
    let div = 1000, exp = 0;
    for (let m = Math.floor(n / 1000); m >= 1000; m = Math.floor(m / 1000)) { div *= 1000; exp++; }
    return (n / div).toFixed(1) + " " + "kMGTPE"[exp] + "B";
  };
  const ago = (t) => {
    if (!t) return "?";
    const days = Math.floor((NOW - t) / DAY);
    if (days < 1) return "today";
    if (days < 14) return days + "d ago";
    if (days < 60) return Math.floor(days / 7) + "w ago";
    if (days < 365) return Math.floor(days / 30) + "mo ago";
    return (days / 365).toFixed(1) + "y ago";
  };
  const base = (p) => p.replace(/^.*[/\\]/, "").replace(/\.(exe|cmd)$/, "");
  // CommandText: the executable reduced to its name (the contract's paths are
  // already home-relative, like the TUI shows them).
  const cmdText = (it) => {
    const c = it.command || [];
    return c.length ? [base(c[0]), ...c.slice(1)].join(" ") : "";
  };

  /* ---------- the "How sizes are counted" panel and the JSON excerpt ---------- */
  const CAT = { project: [10, "Projects"], cache: [20, "Cargo caches and shared builds"], global: [30, "Installed with cargo install"], runtime: [40, "Toolchains, components and targets"] };
  if (SCAN) {
    const items = SCAN.items;
    const where = $("#where");
    if (where) {
      // One bar per category, its length the size on disk, split by level.
      const LV = [["safe", "sf", "g"], ["review", "rv", "y"], ["protected", "pr", "r"], ["live", "lv2", "l"]];
      const cats = Object.keys(CAT).map((c) => {
        const its = items.filter((it) => it.category === c && it.measured);
        const by = {};
        its.forEach((it) => { by[it.safety.level] = (by[it.safety.level] || 0) + Math.max(it.bytes.allocated, 0); });
        return { c, n: its.length, by, total: Object.values(by).reduce((a, b) => a + b, 0) };
      }).filter((x) => x.n);
      const max = Math.max(1, ...cats.map((x) => x.total));
      const escT = (t) => String(t).replace(/[&<>]/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[ch]));
      const note = $(".sh-note", where);
      where.innerHTML = cats.map((x) => {
        const segs = LV.filter(([l]) => x.by[l]).map(([l, cls]) => `<i class="${cls}" style="flex:${x.by[l]}"></i>`).join("") +
          (max > x.total ? `<i class="gap" style="flex:${max - x.total}"></i>` : "");
        const nums = LV.filter(([l]) => x.by[l]).map(([l, , cls]) => `<span class="${cls}">${bytes(x.by[l])} ${l}</span>`).join("");
        return `<div class="sh-row"><div class="sh-head"><code>${escT(CAT[x.c][1])}</code><span class="wh-n">${x.n} item(s)</span></div>` +
          `<div class="sh-bar" aria-hidden="true">${segs}</div><div class="sh-nums"><span><b>${bytes(x.total)}</b> on disk</span>${nums}</div></div>`;
      }).join("");
      if (note) where.append(note);
    }
    const one = items.find((i) => i.kind === "rust-docs") || items.find((i) => i.command);
    const pre = $("#json-item code");
    if (one && pre) {
      const pick = (({ id, fingerprint, kind, category, name, path, tool, action, command, regenerate, bytes: b, safety }) =>
        ({ id, fingerprint, kind, category, name, path, tool, action, command, regenerate, bytes: b, safety }))(one);
      const esc = (s) => s.replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c]));
      // One line per array, like jq -c for the short ones.
      const json = JSON.stringify(pick, null, 2).replace(/\[\n\s+([^\]{]*?)\n\s+\]/g, (m, inner) => "[" + inner.split(/,\n\s+/).join(", ") + "]");
      pre.innerHTML = esc(json)
        .replace(/^(\s*)("[a-z_]+")(:)/gm, '$1<span class="k">$2</span>$3')
        .replace(/: ("(?:[^"\\]|\\.)*")/g, ': <span class="s">$1</span>')
        .replace(/: (-?\d+)/g, ': <span class="n">$1</span>');
    }
    const cap = $("#term-caption");
    if (cap && DATA.os === "Linux") cap.insertAdjacentHTML("beforeend", " Generated on Linux, so sccache's cache sits in <code>~/.cache/sccache</code> rather than <code>~/Library/Caches</code>.");
  }

  /* =====================================================================
     The TUI replica (internal/ui: model.go and view.go)
     ===================================================================== */
  const term = $("#term");
  if (!term) return;
  if (!SCAN) { term.innerHTML = '<img src="demo.gif" alt="rustkill scanning a generated workspace" style="width:100%;height:auto">'; return; }
  const keysEl = $("#term-keys"), hint = $("#try-hint");

  const ITEMS = SCAN.items.map((it) => ({
    ...it,
    lastUse: it.project ? (it.project.last_activity ? Date.parse(it.project.last_activity.at) : 0) : it.last_used ? Date.parse(it.last_used.at) : 0,
  }));
  const ROOTS = (SCAN.roots || []).join(", ");
  const allowed = (it) => it.safety.level === "safe" || it.safety.level === "review";

  const st = {
    group: 0, sort: 0, filter: "", filtering: false, cursorKey: "", moved: false, offset: 0, legend: false,
    selected: new Set(), status: {}, confirm: null, queue: [], outcomes: [], quit: false,
  };
  let cols = 110, lines = 30, timer = 0;

  /* ---------- styled lines: arrays of [class, text] segments ---------- */
  const esc = (s) => String(s).replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]));
  const len = (s) => [...s].length;
  const S = (cls, text) => [[cls, String(text)]];
  const wid = (line) => line.reduce((n, [, t]) => n + len(t), 0);
  const plain = (line) => line.map(([, t]) => t).join("");
  const restyle = (line, cls) => S(cls, plain(line));
  const trunc = (line, n) => {
    if (wid(line) <= n) return line;
    const out = [];
    let left = n - 1;
    for (const [c, t] of line) {
      if (left <= 0) break;
      const chars = [...t];
      out.push([c, chars.slice(0, left).join("")]);
      left -= Math.min(left, chars.length);
    }
    out.push(["", "…"]);
    return out;
  };
  const html = (line) => line.map(([c, t]) => (c ? `<span class="${c}">${esc(t)}</span>` : esc(t))).join("");
  const padR = (s, n) => s + " ".repeat(Math.max(0, n - len(s)));
  const padL = (s, n) => " ".repeat(Math.max(0, n - len(s))) + s;
  const LVC = { safe: "g", review: "y", protected: "r", live: "m" };
  const lvc = (l) => LVC[l] || "m";
  const badge = (l) => S(lvc(l), ({ safe: "●", review: "▲", protected: "■", live: "◉" }[l] || "?") + " " + padR(l, 9));

  /* ---------- rows: groups and items in the current order ---------- */
  const GROUPS = ["project", "framework", "age"], SORTS = ["size", "idle longest", "name"];
  // Project.Framework and report.ProjectBadges.
  const framework = (p) => (p.tools && p.tools.length ? p.tools[0] : "Rust");
  const badges = (p) => {
    const parts = [];
    if (p.workspace) parts.push(p.workspace_members > 0 ? `workspace (${p.workspace_members})` : "workspace");
    if (p.edition) parts.push("edition " + p.edition);
    if (p.toolchain) parts.push("toolchain " + p.toolchain);
    if (p.tools && p.tools.length) parts.push(p.tools.join(", "));
    parts.push("active " + ago(p.last_activity ? Date.parse(p.last_activity.at) : 0));
    if (p.in_linked_worktree) parts.push("linked worktree");
    return parts.join(" · ");
  };
  const ageBucket = (p) => {
    const t = p.last_activity ? Date.parse(p.last_activity.at) : 0;
    if (!t) return ["5", "activity unknown"];
    const d = NOW - t;
    if (d >= 90 * DAY) return ["1", "idle 90+ days"];
    if (d >= 30 * DAY) return ["2", "idle 30–90 days"];
    if (d >= 7 * DAY) return ["3", "idle 7–30 days"];
    return ["4", "active this week"];
  };
  const visible = (it) => {
    if (!st.filter) return true;
    let hay = (it.path + " " + it.name + " " + it.kind + " " + (it.tool || "")).toLowerCase();
    if (it.project) hay += (" " + it.project.name + " " + (it.project.toolchain || "") + " " + (it.project.tools || []).join(" ")).toLowerCase();
    return hay.includes(st.filter.toLowerCase());
  };
  const rows = () => {
    const groups = new Map();
    ITEMS.forEach((it) => {
      if (!visible(it)) return;
      let [rank, title] = CAT[it.category] || [50, it.category];
      let key = it.category, sub = "";
      const p = it.project;
      if (p) {
        if (st.group === 0) { key = "p:" + p.path; title = p.path; sub = badges(p); }
        else if (st.group === 1) { key = "f:" + framework(p); title = framework(p); }
        else { const [b, t] = ageBucket(p); key = "a:" + b; title = t; rank = 10 + +b; }
      }
      let g = groups.get(key);
      if (!g) { g = { key, title, sub, rank, items: [], bytes: 0 }; groups.set(key, g); }
      g.items.push(it); g.bytes += Math.max(it.bytes.allocated, 0);
    });
    const order = [...groups.values()].sort((a, b) => {
      if (a.rank !== b.rank) return a.rank - b.rank;
      if (st.group === 2 || st.sort === 2) return a.title.toLowerCase() < b.title.toLowerCase() ? -1 : a.title.toLowerCase() > b.title.toLowerCase() ? 1 : 0;
      return b.bytes - a.bytes;
    });
    const out = [];
    order.forEach((g) => {
      g.items.sort((a, b) => {
        if (st.sort === 1) return a.lastUse - b.lastUse;
        if (st.sort === 2) { const x = a.path + a.name, y = b.path + b.name; return x < y ? -1 : x > y ? 1 : 0; }
        return b.bytes.allocated - a.bytes.allocated;
      });
      out.push({ group: g.key, title: g.title, sub: g.sub || g.items.length + " item(s)" });
      g.items.forEach((it) => out.push({ group: g.key, title: g.title, it }));
    });
    return out;
  };
  const rkey = (r) => (r.it ? r.it.id : "group:" + r.group);
  const firstItem = (rs) => { const r = rs.find((x) => x.it) || rs[0]; return r ? rkey(r) : ""; };
  const cursor = (rs) => Math.max(0, rs.findIndex((r) => rkey(r) === st.cursorKey));
  const selectable = (it) => { const s = st.status[it.id]; return it.measured && allowed(it) && !s; };
  const leaves = (r, rs = rows()) => (r.it ? (selectable(r.it) ? [r.it.id] : []) : rs.filter((x) => x.it && x.group === r.group && selectable(x.it)).map((x) => x.it.id));
  const bulk = (it) => it.safety.level === "safe" && it.bytes.allocated > 0;
  const matching = (f) => rows().filter((r) => r.it && selectable(r.it) && f(r.it)).map((r) => r.it.id);
  const toggle = (ids) => {
    const all = ids.length > 0 && ids.every((id) => st.selected.has(id));
    ids.forEach((id) => (all ? st.selected.delete(id) : st.selected.add(id)));
  };
  const top = () => { st.offset = 0; const rs = rows(); if (!st.moved && rs.length) st.cursorKey = firstItem(rs); };
  const find = (id) => ITEMS.find((it) => it.id === id);

  /* ---------- view pieces ---------- */
  const DETAIL = 5;
  const header = () => {
    let total = 0, free = 0, safe = 0;
    ITEMS.forEach((it) => {
      if (st.status[it.id] === "done" || !it.measured) return;
      total += it.bytes.allocated;
      if (allowed(it)) free += it.bytes.exclusive;
      if (it.safety.level === "safe") safe += it.bytes.exclusive;
    });
    const parts = [
      [[["brandb", " rustkill "], ["", " "], ["y", "● dry run"]], 0],
      [[["b", bytes(total)], ["", " on disk"]], 3],
      [[["c", bytes(free)], ["", " freeable"]], 1],
      [[["g", bytes(safe)], ["", " safe"]], 1],
      [S("f", ROOTS), 5],
    ];
    for (let drop = 6; ; drop--) {
      for (const sep of ["  ·  ", " · "]) {
        const keep = parts.filter(([, d]) => d < drop).map(([p]) => p);
        const line = [["", " "]];
        keep.forEach((p, i) => { if (i) line.push(["f", sep]); line.push(...p); });
        if (wid(line) <= cols || drop === 1) return line;
      }
    }
  };
  const layout = (rs) => ({
    bar: cols < 70 ? 0 : cols < 100 ? 6 : cols < 120 ? 10 : 14,
    frees: rs.some((r) => r.it && freesText(r.it)),
  });
  const freesText = (it) => (it.measured && it.bytes.shared > 0 && it.bytes.exclusive < Math.floor((it.bytes.allocated * 9) / 10) ? bytes(it.bytes.exclusive) : "");
  const columns = (l, bar, size, frees, idle, bdg) => {
    const out = [];
    if (l.bar > 0) out.push(...bar, ["", " ".repeat(Math.max(0, l.bar - wid(bar))) + "  "]);
    out.push(["", padL(size, 9) + "  "]);
    if (l.frees) out.push(["c", padL(frees, 9)], ["", "  "]);
    out.push(...idle, ["", "  "], ...bdg, ["", " "]);
    return out;
  };
  const EIGHTHS = ["", "▏", "▎", "▍", "▌", "▋", "▊", "▉"];
  const fine = (n, total, width) => {
    if (total <= 0 || n <= 0) return "";
    const units = Math.max(1, Math.min(Math.floor((n * width * 8) / total), width * 8));
    return "█".repeat(Math.floor(units / 8)) + EIGHTHS[units % 8];
  };
  const bar = (it, largest, width) => {
    if (width <= 0) return [];
    const t = it.bytes.allocated;
    if (!it.measured || t <= 0) return S("f", "·".repeat(width));
    const cells = [...fine(t, largest, width)];
    const own = Math.floor((cells.length * it.bytes.exclusive) / t);
    return [[lvc(it.safety.level), cells.slice(0, own).join("")], ["c", cells.slice(own).join("")], ["f", "·".repeat(width - cells.length)]];
  };
  const idle = (it) => {
    const t = it.lastUse;
    const s = padR(t ? ago(t).replace(/ ago$/, "") : "—", 5);
    if (!t) return S("f", s);
    const d = NOW - t;
    return S(d < 7 * DAY ? "g" : d >= 90 * DAY ? "y" : "", s);
  };
  const codeLabel = (c) => (c === "catalog_review" ? "check first" : c === "old_cargo" ? "old cargo uses it" : c.replace(/_/g, " "));
  const flags = (it) => {
    const out = [];
    it.safety.reasons.forEach((r) => { if (out.length) out.push(["", " "]); out.push([lvc(r.level), codeLabel(r.code)]); });
    if (it.command) { if (out.length) out.push(["", " "]); out.push(["m", "via " + cmdText(it)]); }
    return out;
  };
  const label = (it) => {
    if (it.project && st.group !== 0) return it.path;
    if (it.project) { const p = it.project.path; return it.path.startsWith(p + "/") ? it.path.slice(p.length + 1) : base(it.path); } // report.Label
    return it.name;
  };
  const leftTrim = (s, n) => { const r = [...s]; return r.length > n ? "…" + r.slice(r.length - n + 1).join("") : s; };
  const check = (ids, r) => {
    if (r.it) {
      const s = st.status[r.it.id];
      if (s === "done") return S("g", "✓");
      if (s === "working" || s === "queued") return S("f", "◌");
      if (!allowed(r.it)) return S("", " ");
    }
    const sel = ids.filter((id) => st.selected.has(id)).length;
    if (sel > 0 && sel === ids.length) return S("c", "●");
    if (sel > 0) return S("c", "◐");
    return S("f", "○");
  };
  const row = (r, isCur, largest, l, rs) => {
    const cur = isCur ? S("g cur", "▌") : S("", " ");
    if (!r.it) {
      const size = rs.filter((x) => x.it && x.group === r.group && st.status[x.it.id] !== "done").reduce((n, x) => n + Math.max(x.it.bytes.allocated, 0), 0);
      return [...cur, ["", " "], ...check(leaves(r, rs), r), ["", " "], ["h", r.title], ["", "  "], ["b", bytes(size)], ["", "  "], ["f", r.sub]];
    }
    const it = r.it;
    const cl = columns(l, bar(it, largest, l.bar), it.measured ? bytes(it.bytes.allocated) : "…", freesText(it), idle(it), badge(it.safety.level));
    const room = cols - 4 - wid(cl) - 1;
    let line = [...cl, ["b", leftTrim(label(it), Math.max(room, 8))], ["", "  "], ...flags(it)];
    const s = st.status[it.id];
    if (s === "done") line = restyle(line, "s");
    else if (s === "working" || s === "queued") line = restyle(line, "f");
    return [...cur, ["", " "], ...check([it.id], r), ["", " "], ...line];
  };
  const detail = (r) => {
    const pad = ["", "  "];
    if (!r) return [];
    if (!r.it) return [[pad, ["h", r.title], ["", "  "], ["f", r.sub]], [pad, ["f", "space selects every allowed item in the group · enter cleans the selection"]]];
    const it = r.it;
    const head = [pad, ["b", it.path || it.name]];
    if (it.measured) {
      head.push(["f", "  " + bytes(it.bytes.allocated) + " on disk"]);
      if (it.bytes.shared > 0) head.push(["f", ", frees " + bytes(it.bytes.exclusive) + " now; "], ["c", bytes(it.bytes.shared) + " shared"]);
    }
    let how = [["", "Action: move to the Trash"]];
    if (it.command) how = [["", "Action: "], ["m", cmdText(it)], ["f", " (the tool's own cleanup; deletes permanently)"]];
    if (!allowed(it)) {
      how = [[lvc(it.safety.level), "rustkill leaves this alone"]];
      if (it.note && it.safety.reasons.some((x) => x.code === "not_installed")) how.push(["f", ": " + it.note]);
    }
    if (it.regenerate) how.push(["f", "  ·  comes back with: "], ["", it.regenerate]);
    const why = [];
    it.safety.reasons.forEach((x) => { if (x.detail !== it.explain) why.push([[lvc(x.level), x.detail]]); });
    if (it.note) why.push(S("f", it.note));
    if (it.last_used) why.push(S("f", "last used " + ago(Date.parse(it.last_used.at)) + " (" + it.last_used.source + ")"));
    if (!why.length) why.push(S("g", "rebuildable, ignored by git, not in use"));
    const whyLine = [pad];
    why.forEach((w, i) => { if (i) whyLine.push(["f", " · "]); whyLine.push(...w); });
    return [head, [pad, ["", it.explain || ""]], [pad, ...how], whyLine].map((l) => trunc(l, cols - 1));
  };
  const summary = () => {
    let sel = 0, b = 0;
    st.selected.forEach((id) => { const it = find(id); if (it) { sel++; b += it.bytes.exclusive; } });
    const parts = [S("", `${sel} selected · frees ${bytes(b)}`)];
    let would = 0;
    st.outcomes.forEach((o) => { would += o.estimate; });
    if (would > 0) parts.push(S("y", "would free " + bytes(would)));
    if (st.queue.length) parts.push(S("f", st.queue.length + " queued"));
    if (st.filter || st.filtering) parts.push(S("c", "/" + st.filter + (st.filtering ? "▏" : "")));
    const line = [["", "  "]];
    parts.forEach((p, i) => { if (i) line.push(["f", "  ·  "]); line.push(...p); });
    return line;
  };
  const KEYHINTS = [["↑↓ move", 5], ["space select", 0], ["s safe in projects", 1], ["a all safe", 3], ["enter clean", 0],
    ["/ filter", 4], ["tab group", 2], ["o sort", 4], ["R rescan", 6], ["? legend", 0], ["q quit", 0]];
  const help = () => {
    if (st.legend) return S("f", "  any key closes");
    if (st.confirm) {
      if (!plan().run.length) return S("f", "  Nothing here can be cleaned. Any key closes");
      return [["y", "  Proceed?"], ["", "  "], ["b", "y"], ["f", " yes   any other key cancels"]];
    }
    if (st.filtering) return S("f", "  type to filter  enter keep  esc clear");
    for (let drop = 7; ; drop--) {
      const s = "  " + KEYHINTS.filter(([, d]) => d < drop).map(([t]) => t).join("  ");
      if (len(s) <= cols || drop === 1) return S("f", s);
    }
  };
  const plan = () => {
    const run = [], refuse = [];
    (st.confirm || []).forEach((id) => {
      const it = find(id);
      if (!it) return;
      if (it.measured && allowed(it)) run.push(it); else refuse.push(it);
    });
    return { run, refuse };
  };
  const confirmLines = (height) => {
    const { run, refuse } = plan();
    const trash = run.filter((it) => !it.command), cmds = run.filter((it) => it.command);
    const sum = (a) => a.reduce((n, it) => n + it.bytes.exclusive, 0);
    const heading = (cls, text, n) => {
      const w = Math.min(cols, 100) - 2, total = bytes(n);
      return S(cls, text + " ".repeat(Math.max(2, w - len(text) - total.length)) + total);
    };
    const out = [[], S("y", "  Dry run: nothing will change."), []];
    const room = height - 6;
    const list = (items, show) => {
      for (let i = 0; i < items.length; i++) {
        if (i >= Math.floor(room / 2) && items.length > Math.floor(room / 2)) { out.push(S("f", `      … and ${items.length - i} more`)); return; }
        out.push([["", "      " + padL(bytes(items[i].bytes.exclusive), 9) + "  "], ...show(items[i])]);
      }
    };
    if (trash.length) { out.push(heading("g", "  Move to the Trash · recoverable", sum(trash))); list(trash, (it) => S("", it.path)); }
    if (cmds.length) { out.push([], heading("m", "  Official commands · the tool deletes them", sum(cmds))); list(cmds, (it) => S("", cmdText(it))); }
    if (refuse.length) {
      out.push([], S("r", `  Not included (${refuse.length}):`));
      for (let i = 0; i < refuse.length; i++) {
        if (out.length >= height - 1) { out.push(S("f", `      … and ${refuse.length - i} more`)); break; }
        const it = refuse[i];
        out.push([["", "      " + label(it) + "  "], ["f", it.safety.reasons.length ? it.safety.reasons[0].detail : "not measured yet"]]);
      }
    }
    return out;
  };
  // ansi.Wrap: word wrap that keeps each word's style.
  const para = (out, indent, line) => {
    const w = Math.max(20, cols - indent.length - 1);
    const words = [];
    line.forEach(([c, t]) => t.split(/( )/).forEach((p) => { if (p) words.push([c, p]); }));
    let cur = [], n = 0;
    const flush = () => { while (cur.length && cur[cur.length - 1][1] === " ") cur.pop(); out.push([["", indent], ...cur]); cur = []; n = 0; };
    words.forEach(([c, t]) => {
      if (t === " " && n === 0) return;
      if (t !== " " && n + len(t) > w && n > 0) flush();
      cur.push([c, t]); n += len(t);
    });
    if (cur.length) flush();
  };
  const legendLines = () => {
    const out = [[], [["", "  "], ["b", "Safety"]]];
    para(out, "    ", [...badge("safe"), ["", "  rebuildable and idle; "], ["b", "s"], ["", " selects these in projects, "], ["b", "a"], ["", " everywhere"]]);
    para(out, "    ", [...badge("review"), ["", "  allowed, but worth a look: recent project or build, a shared target, installers you built, worktree, costs a download or reinstall"]]);
    para(out, "    ", [...badge("protected"), ["", "  shown, never removed: git tracks it or does not ignore it, the default toolchain, or its tool is missing"]]);
    para(out, "    ", [...badge("live"), ["", "  cargo, rustc or rust-analyzer is using it right now, or holds its build lock"]]);
    out.push([]);
    para(out, "  ", S("", "Moving to the Trash frees space when you empty it; official commands free it right away."));
    out.push([]);
    para(out, "  ", [["b", "SIZE"], ["", " is on disk. "], ["c", "FREES"], ["", " appears when much of it is shared (APFS clones, hard links) and is what removing it frees now."]]);
    para(out, "  ", [["", "Folders (target, Cargo's caches) move to the Trash. Toolchains, components and installed tools are removed by their own tool ("], ["m", "rustup toolchain uninstall, rustup component remove, cargo uninstall"], ["", "). Free space is measured from the disk, not assumed."]]);
    out.push([]);
    para(out, "  ", [["b", "Keys"], ["", "  ↑↓/jk move · space select · s safe in projects · a all safe · enter clean (asks first) · / filter · tab group by project, framework or idle time · o sort by size, idle time or name · R rescan · q quit"]]);
    return out;
  };

  /* ---------- after q: the shell and the dry-run line report.Receipt prints ---------- */
  const exited = () => {
    const out = [[["g", "$ "], ["", "rustkill --dry-run"]]];
    if (st.outcomes.length) {
      let nT = 0, nC = 0, bT = 0, bC = 0;
      st.outcomes.forEach((o) => { if (o.action === "command") { nC++; bC += o.estimate; } else { nT++; bT += o.estimate; } });
      out.push([]);
      para(out, "", S("", `Dry run: would move ${nT} item(s), ${bytes(bT)}, to the Trash and run ${nC} official command(s) (${bytes(bC)}). Nothing changed.`));
    }
    out.push([["g", "$ "], ["cur g", "▌"]]);
    return out;
  };

  /* ---------- render ---------- */
  const measure = () => {
    const probe = document.createElement("span");
    probe.textContent = "0".repeat(100);
    probe.style.cssText = "position:absolute;visibility:hidden;white-space:pre";
    term.append(probe);
    const cw = probe.getBoundingClientRect().width / 100;
    probe.remove();
    const cs = getComputedStyle(term);
    const lh = parseFloat(cs.lineHeight) || 18;
    // The TUI needs 60 columns; narrower screens scroll the replica sideways.
    cols = Math.max(60, Math.floor((term.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight)) / cw));
    lines = Math.max(20, Math.round((term.clientHeight - parseFloat(cs.paddingTop) - parseFloat(cs.paddingBottom)) / lh));
  };
  const render = () => {
    measure();
    const out = []; // [line, rowIndex|undefined]
    const push = (l, i) => out.push([trunc(l, cols), i]);
    if (st.quit) {
      exited().forEach((l) => push(l));
    } else {
      push(header());
      push([]); // no scan warnings in the demo
      const rs = rows();
      const cur = cursor(rs);
      let body = Math.max(3, lines - 6 - DETAIL);
      const full = st.legend || st.confirm;
      if (full) body += 1 + DETAIL;
      let ls = [];
      if (st.legend) ls = legendLines().map((l) => [l]);
      else if (st.confirm) ls = confirmLines(body).map((l) => [l]);
      else if (!rs.length && st.filter) ls = [[[]], [S("f", "  Nothing matches “" + st.filter + "”. esc clears the filter.")]];
      else {
        const l = layout(rs);
        const hdr = [["f", "    "]];
        columns(l, [], "SIZE", "FREES", S("", padR("IDLE", 5)), S("", padR("SAFETY", 11))).forEach(([c, t]) => hdr.push([c === "c" ? "cf" : "f", t]));
        hdr.push(["f", "ITEM  (by " + GROUPS[st.group] + ", " + SORTS[st.sort] + ")"]);
        push(hdr);
        body--;
        if (cur < st.offset) st.offset = cur;
        if (cur >= st.offset + body) st.offset = cur - body + 1;
        st.offset = Math.max(0, Math.min(st.offset, Math.max(0, rs.length - body)));
        const largest = Math.max(1, ...rs.filter((r) => r.it).map((r) => r.it.bytes.allocated));
        for (let i = st.offset; i < Math.min(rs.length, st.offset + body); i++) ls.push([row(rs[i], i === cur, largest, l, rs), i]);
      }
      for (let i = 0; i < body; i++) push(ls[i] ? ls[i][0] : [], ls[i] ? ls[i][1] : undefined);
      if (!full) {
        push(S("f", "─".repeat(cols)));
        const d = rs.length ? detail(rs[cur]) : [];
        for (let i = 0; i < DETAIL; i++) push(d[i] || []);
      }
      push(summary());
      push(help());
    }
    term.innerHTML = out.map(([l, i]) => (i === undefined ? `<span class="ln">${html(l)}</span>` : `<span class="row ln" data-row="${i}">${html(l)}</span>`)).join("");
    renderKeys();
  };

  const KEYS = {
    list: [["↑", ""], ["↓", ""], ["space", "select"], ["s", "safe in projects", true], ["a", "all safe"], ["⏎", "clean", true], ["tab", "group"], ["o", "sort"], ["?", "legend"], ["q", "quit"]],
    confirm: [["y", "yes, dry run", true], ["esc", "cancel"]],
    legend: [["esc", "close", true]],
    quit: [["r", "run it again", true]],
  };
  const renderKeys = () => {
    let set = st.quit ? KEYS.quit : st.legend ? KEYS.legend : st.confirm ? KEYS.confirm : KEYS.list;
    if (set === KEYS.list) {
      const anySel = st.selected.size > 0;
      set = set.map(([k, l, p]) => [k, l, k === "⏎" ? anySel : k === "s" ? !anySel && !st.outcomes.length : false]);
    }
    keysEl.innerHTML = set.map(([k, l, p]) => `<button type="button" data-key="${esc(k)}" class="${p ? "primary" : ""}"><b>${esc(k)}</b>${esc(l)}</button>`).join("");
  };

  /* ---------- the dry run: one item at a time, as clean.Act would ---------- */
  const startRun = () => {
    const { run } = plan();
    run.forEach((it) => { st.status[it.id] = "queued"; st.queue.push(it.id); st.selected.delete(it.id); });
    st.confirm = null;
    step();
  };
  const step = () => {
    clearTimeout(timer);
    const id = st.queue.shift();
    if (!id) return render();
    st.status[id] = "working";
    render();
    timer = setTimeout(() => {
      const it = find(id);
      st.status[id] = "done";
      st.outcomes.push({ id, action: it.command ? "command" : "trash", estimate: it.bytes.exclusive });
      step();
    }, reduceMotion ? 0 : 110);
  };
  const reset = () => {
    clearTimeout(timer);
    Object.assign(st, { group: 0, sort: 0, filter: "", filtering: false, moved: false, offset: 0, legend: false, selected: new Set(), status: {}, confirm: null, queue: [], outcomes: [], quit: false });
    st.cursorKey = firstItem(rows());
  };

  /* ---------- keys (Model.key) ---------- */
  const key = (k, text) => {
    if (hint) hint.classList.add("gone");
    if (st.quit) { if (k === "r" || k === "R" || k === "enter") { reset(); render(); return true; } return false; }
    if (st.legend) { st.legend = false; render(); return true; }
    if (st.confirm) { if (k === "y" || k === "Y") startRun(); else { st.confirm = null; render(); } return true; }
    if (st.filtering) {
      if (k === "enter" || k === "esc") { if (k === "esc") st.filter = ""; st.filtering = false; }
      else if (k === "backspace") st.filter = [...st.filter].slice(0, -1).join("");
      else if (text) st.filter += text;
      else return false;
      st.offset = 0; render(); return true;
    }
    const rs = rows();
    let i = cursor(rs);
    if (["up", "down", "pgup", "pgdown", "home", "end"].includes(k)) st.moved = true;
    const busy = st.queue.length > 0;
    switch (k) {
      case "q": case "esc":
        if (k === "esc" && st.filter) { st.filter = ""; break; }
        if (busy) return true;
        st.quit = true; break;
      case "?": st.legend = true; break;
      case "/": st.filtering = true; break;
      case "up": i--; break;
      case "down": i++; break;
      case "pgup": i -= Math.max(3, lines - 11); break;
      case "pgdown": i += Math.max(3, lines - 11); break;
      case "home": i = 0; break;
      case "end": i = rs.length - 1; break;
      case "tab": st.group = (st.group + 1) % 3; top(); render(); return true;
      case "o": st.sort = (st.sort + 1) % 3; top(); render(); return true;
      case "R": if (busy) return true; reset(); render(); return true;
      case "space": if (rs.length) toggle(leaves(rs[i], rs)); render(); return true;
      case "s": toggle(matching((it) => bulk(it) && it.category === "project")); render(); return true;
      case "a": toggle(matching(bulk)); render(); return true;
      case "enter": {
        let ids = rs.filter((r) => r.it && st.selected.has(r.it.id)).map((r) => r.it.id);
        if (!ids.length && rs.length) ids = rs[i].it ? [rs[i].it.id] : leaves(rs[i], rs);
        ids = ids.filter((id) => !st.status[id]);
        if (ids.length) st.confirm = ids;
        render(); return true;
      }
      default: return false;
    }
    if (rs.length && !st.quit && !st.legend && !st.filtering) { i = Math.max(0, Math.min(i, rs.length - 1)); st.cursorKey = rkey(rs[i]); }
    render();
    return true;
  };
  const KEYMAP = { ArrowUp: "up", k: "up", ArrowDown: "down", j: "down", PageUp: "pgup", PageDown: "pgdown", Home: "home", g: "home", End: "end", G: "end",
    Enter: "enter", Escape: "esc", Backspace: "backspace", Delete: "enter", d: "enter", Tab: "tab", " ": "space" };
  term.addEventListener("keydown", (e) => {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    if (e.key === "Tab" && e.shiftKey) return; // keep keyboard focus movement possible
    const text = e.key.length === 1 ? e.key : "";
    const k = st.filtering && text && text !== " " ? "text" : KEYMAP[e.key] || (text || null);
    if (!k) return;
    if (key(k, text) !== false) e.preventDefault();
  });
  keysEl.addEventListener("click", (e) => {
    const b = e.target.closest("button[data-key]");
    if (!b) return;
    const map = { "↑": "up", "↓": "down", "⏎": "enter" };
    key(map[b.dataset.key] || b.dataset.key);
  });
  term.addEventListener("click", (e) => {
    term.focus({ preventScroll: true });
    if (st.legend || st.confirm || st.quit) return;
    const r = e.target.closest("[data-row]");
    if (!r) return;
    const rs = rows(), i = +r.dataset.row;
    if (rkey(rs[i]) === st.cursorKey) key("space");
    else { st.moved = true; st.cursorKey = rkey(rs[i]); render(); }
  });

  let rt;
  addEventListener("resize", () => { clearTimeout(rt); rt = setTimeout(render, 80); });
  reset();
  render();
})();

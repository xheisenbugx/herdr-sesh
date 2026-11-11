// The session picker (cmd-p) for Herdr: running spaces, the sessions in
// herdr-sesh's own config (~/.config/herdr-sesh/config.toml), and zoxide
// directories, with a live preview. Enter goes to a space, or opens a directory
// as a new space (running the config's startup_command, like sesh).

import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex, InputState, Input, ScrollHandle } from "gpui-base";
import { run, theme, props, closeOverlay, hc, exec, home, readTextFile, spaces, panes, agents, settings, machines } from "herdr";
import * as L from "./lib.js";

const SOURCES = [
  { key: "all", label: "all", icon: null, ctrl: "a" },
  { key: "space", label: "spaces", icon: "grid", ctrl: "t" },
  { key: "config", label: "configs", icon: "gear", ctrl: "g" },
  { key: "zoxide", label: "zoxide", icon: "folder", ctrl: "z" },
  { key: "remote", label: "machines", icon: "branch", ctrl: "r" },
];
const RANK = { blocked: 0, done: 1, working: 2, idle: 3 };

// herdr-sesh's config file, and just enough TOML for its [[session]] tables and [default_session].
const CONFIG = ".config/herdr-sesh/config.toml";
// A line without its comment: a # starts one only outside a quoted string.
function stripComment(line) {
  let q = null;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (q === '"' && c === "\\") { i++; continue; }
    if (q) { if (c === q) q = null; continue; }
    if (c === '"' || c === "'") q = c;
    else if (c === "#") return line.slice(0, i);
  }
  return line;
}
const ESC = { n: "\n", t: "\t", r: "\r", '"': '"', "\\": "\\" };
const unescape = (x) => x.replace(/\\(u[0-9a-fA-F]{4}|.)/g, (_m, e) => (e.length === 5 ? String.fromCharCode(parseInt(e.slice(1), 16)) : ESC[e] ?? e));
// A folder without its trailing slash ("/" stays "/").
const trim = (p) => (p && p.length > 1 ? p.replace(/\/+$/, "") : p);
function parseSesh(text) {
  const out = { sessions: [], startup: null };
  let cur = null;
  for (const raw of (text ?? "").split("\n")) {
    const line = stripComment(raw).trim();
    if (!line) continue;
    const head = line.replace(/\s+/g, "");
    if (head === "[[session]]") { cur = {}; out.sessions.push(cur); continue; }
    if (head.startsWith("[")) { cur = head === "[default_session]" ? (out.def = {}) : null; continue; }
    const m = line.match(/^([\w-]+)\s*=\s*(?:"((?:[^"\\]|\\.)*)"|'([^']*)')\s*$/);
    if (m && cur) cur[m[1]] = m[2] !== undefined ? unescape(m[2]) : m[3];
  }
  out.startup = out.def?.startup_command ?? null;
  out.sessions = out.sessions.filter((s) => s.name && s.path);
  return out;
}

export default class Sessions extends View {
  init(_props, cx) {
    this.filter = "";
    this.source = "all";
    this.showPreview = true;
    this.selKey = null; // the selected row's key: rows re-sort while the dialog is open
    this.scrolledTo = null;
    this.loading = new Set();
    this.zoxide = [];
    this.config = { sessions: [], startup: null };
    this.preview = {};
    this.remote = null; // [{ machine, path }] once asked (ctrl-r)
    this.scroll = ScrollHandle.new();
    this.input = InputState.new({ placeholder: "Filter sessions..." });
    this.input.on("change", (_e, cx) => { this.filter = this.input.value(); this.selKey = null; this.refresh(cx); cx.notify(); });
    this.input.on("submit", (_e) => this.connect(this.selected()));
    this.input.focus();
    cx.timer.after(300, () => this.input.focus());
    cx.spawn(async (cx) => {
      const h = home();
      const text = await readTextFile(`${h}/${CONFIG}`).catch(() => null);
      this.config = parseSesh(text);
      try {
        const r = await exec("zoxide", ["query", "-l"], { timeout: 5000 });
        if (r.code === 0) this.zoxide = r.stdout.split("\n").filter(Boolean);
      } catch {}
      this.refresh(cx);
      cx.notify();
    });
    cx.timer.every(1500, (cx) => this.refresh(cx));
  }

  close() { closeOverlay(props().overlay); }
  selected(rows = this.rows()) { return rows.find((r) => r.key === this.selKey) ?? rows[0] ?? null; }
  tilde(p) { const h = home(); return p.startsWith(h) ? "~" + p.slice(h.length) : p; }

  rows() {
    const fz = L.fuzzy(this.filter);
    const open = new Set();
    const out = [];
    if (this.source === "all" || this.source === "space") {
      const sp = spaces().map((p) => ({ kind: "space", key: p.id, name: p.name, space: p, status: p.status, path: p.folder }));
      sp.sort((a, b) => (RANK[a.status] ?? 4) - (RANK[b.status] ?? 4));
      for (const r of sp) out.push(r);
    }
    for (const p of spaces()) {
      if (p.machine) continue;
      if (p.folder) open.add(trim(p.folder));
      const tabs = new Set(p.tabs.map((tb) => tb.id));
      for (const x of panes()) if (x.cwd && tabs.has(x.tab)) open.add(trim(x.cwd));
    }
    const h = home();
    if (this.source === "all" || this.source === "config")
      for (const s of this.config.sessions) {
        const path = trim(s.path.replace(/^~(?=\/|$)/, h));
        out.push({ kind: "config", key: `c:${s.name}`, name: s.name, path, startup: s.startup_command ?? null });
      }
    if (this.source === "all" || this.source === "zoxide")
      for (const z of this.zoxide) if (!open.has(z)) out.push({ kind: "zoxide", key: `z:${z}`, name: this.tilde(z), path: z });
    if (this.source === "remote")
      for (const r of this.remote ?? []) out.push({ kind: "remote", key: `r:${r.machine}:${r.path}`, name: `${r.machine}:${r.path}`, path: r.path, machine: r.machine });
    return this.filter ? out.filter((r) => fz(`${r.name} ${r.path ?? ""}`)) : out;
  }

  refresh(cx) {
    if (!this.showPreview) return;
    const r = this.selected();
    // One look at a time per row: a slow machine mustn't pile up `ls` calls.
    if (!r || this.loading.has(r.key)) return;
    this.loading.add(r.key);
    cx.spawn(async (cx) => {
      let lines = null;
      if (r.kind === "space") {
        const tab = r.space.tabs.find((x) => x.active) ?? r.space.tabs[0];
        const p = panes().find((x) => x.tab === tab?.id && x.focused) ?? panes().find((x) => x.tab === tab?.id && x.terminal);
        if (p?.terminal) {
          const res = await hc(["read", p.terminal]);
          if (res.code === 0) lines = L.screenLines(res.stdout);
        }
      } else if (r.kind === "remote") {
        try {
          const res = await exec("ls", ["-1Ap", r.path], { on: r.machine, timeout: 6000 });
          lines = res.code === 0 ? res.stdout.split("\n").filter(Boolean) : [res.stderr.trim()];
        } catch { lines = ["(unreachable)"]; }
      } else if (r.path) {
        try {
          const res = await exec("ls", ["-1Ap", r.path], { timeout: 3000 });
          lines = res.code === 0 ? res.stdout.split("\n").filter(Boolean) : [res.stderr.trim()];
        } catch { lines = ["(not found)"]; }
      }
      this.preview[r.key] = lines ?? [];
      this.loading.delete(r.key);
      cx.notify();
    });
  }

  connect(r) {
    if (!r) return;
    this.close();
    if (r.kind === "space") return run("space.select", { id: r.space.id });
    if (r.kind === "remote") {
      const there = spaces().find((p) => p.machine === r.machine && p.folder === r.path);
      if (there) return run("space.select", { id: there.id });
      return run("space.new", { folder: r.path, machine: r.machine });
    }
    // These are all on this computer: only its spaces count, and new ones start here
    // (machine "", not the focused space's machine).
    const local = spaces().filter((p) => !p.machine);
    // A config session is its space by name first.
    if (r.kind === "config") {
      const named = local.find((p) => p.named && p.name === r.name);
      if (named) return run("space.select", { id: named.id });
    }
    // Otherwise a space already there (by its folder, or a terminal in it) is reused.
    const there = local.find((p) => trim(p.folder) === r.path) ?? local.find((p) => panes().some((x) => trim(x.cwd) === r.path && p.tabs.some((tb) => tb.id === x.tab)));
    if (there) return run("space.select", { id: there.id });
    const cmd = r.startup ?? this.config.startup;
    const name = r.kind === "config" ? r.name : undefined;
    run("space.new", { folder: r.path, machine: "", ...(name ? { name } : {}), ...(cmd ? { argv: ["sh", "-c", cmd] } : {}) });
  }

  // Directories on the other machines (ctrl-r): each one's zoxide list.
  loadRemote(cx) {
    if (this.remote) return;
    this.remote = [];
    cx.spawn(async (cx) => {
      const out = [];
      for (const m of machines()) {
        if (!m.name || m.layout === "unavailable") continue;
        try {
          const r = await exec("zoxide", ["query", "-l"], { on: m.name, timeout: 8000 });
          if (r.code === 0) for (const path of r.stdout.split("\n").filter(Boolean)) out.push({ machine: m.name, path });
        } catch {}
      }
      this.remote = out;
      cx.notify();
    });
  }

  kill(r, cx) {
    if (!r) return;
    if (r.kind === "space") run("space.close", { id: r.space.id });
    else if (r.kind === "zoxide") cx.spawn(async (cx) => {
      await exec("zoxide", ["remove", r.path]);
      this.zoxide = this.zoxide.filter((z) => z !== r.path);
      cx.notify();
    });
  }

  key(e, cx) {
    const rows = this.rows();
    const k = e.key;
    const c = e.modifiers.control;
    const src = SOURCES.find((s) => c && s.ctrl === k);
    const i = Math.max(0, rows.findIndex((r) => r.key === this.selected(rows)?.key));
    const pick = (j) => { if (rows.length) this.selKey = rows[Math.max(0, Math.min(rows.length - 1, j))].key; };
    if (k === "escape") return this.close();
    if (k === "down" || (c && (k === "n" || k === "j"))) pick(i + 1);
    else if (k === "up" || (c && (k === "p" || k === "k"))) pick(i - 1);
    else if (src) { this.source = src.key; this.selKey = null; if (src.key === "remote") this.loadRemote(cx); }
    else if (c && k === "x") this.kill(this.selected(rows), cx);
    else if (c && k === "o") this.showPreview = !this.showPreview;
    else return;
    this.refresh(cx);
    cx.stop_propagation();
    cx.notify();
  }

  icon(t, r) {
    if (r.kind === "space") {
      if (r.status && r.status !== "idle") return L.glyph(t, r.status);
      return svg(L.ICON("grid")).size(12).flex_none().text_color(t.ansi4 ?? t.accent);
    }
    if (r.kind === "config") return svg(L.ICON("gear")).size(12).flex_none().text_color(t.muted);
    if (r.kind === "remote") return svg(L.ICON("branch")).size(12).flex_none().text_color(t.ansi5 ?? t.accent);
    return svg(L.ICON("folder")).size(12).flex_none().text_color(t.ansi6 ?? t.done);
  }

  render() {
    const t = theme();
    const rows = this.rows();
    const sel = this.selected(rows);
    const at = rows.indexOf(sel);
    // Bring the selection into view when it changes, not on every redraw (the timer
    // redraws every 1.5 s, which would undo scrolling the list with the mouse).
    if (sel && sel.key !== this.scrolledTo) { this.scrolledTo = sel.key; this.scroll.scroll_to_item(at); }
    const hint = (k, d) => h_flex().gap(4).child(div().font_bold().text_color(t.accent).child(k)).child(div().text_color(t.muted).child(d));
    const dot = () => div().text_color(t.dim).child("·");
    const lines = sel ? this.preview[sel.key] ?? [] : [];
    const tail = sel?.kind === "space" ? lines.slice(-(this.previewRows ?? 30)) : lines.slice(0, this.previewRows ?? 30);

    const sources = h_flex().gap(6).items_center();
    SOURCES.forEach((s, i) => {
      if (i) sources.child(dot());
      const on = this.source === s.key;
      sources.child(h_flex().id(`src-${s.key}`).gap(4).items_center().cursor_pointer()
        .text_color(on ? t.accent : t.muted).when(on, (el) => el.font_bold())
        .on_click((_e, cx) => { this.source = s.key; this.selKey = null; if (s.key === "remote") this.loadRemote(cx); this.refresh(cx); cx.notify(); })
        .when(!!s.icon, (el) => el.child(svg(L.ICON(s.icon)).size(11).text_color(on ? t.accent : t.muted)))
        .child(s.label));
    });

    return v_flex()
      .id("sesh-sessions")
      .on_key_down((e, cx) => this.key(e, cx))
      .size_full()
      .bg(t.background)
      .rounded(10)
      .border_1()
      .border_color(t.border)
      .px(24)
      .pt(22)
      .text_size(13)
      .text_color(t.foreground)
      .child(L.hatchTitle(t, "Sessions", null))
      .child(h_flex().items_center().child(sources.flex_1()).child(div().text_color(t.muted).child(String(rows.length))))
      .child(h_flex().items_center().gap(6).pt(2).pb(8).pl(12)
        .child(div().text_color(t.accent).font_bold().child("›"))
        .child(Input.new(this.input).flex_1().text_size(13).text_color(t.foreground)))
      .child(
        h_flex().flex_1().min_h(0).items_start()
          .child(
            v_flex().when(this.showPreview, (el) => el.w("40%").border_r(1).border_color(t.border).pr(6)).when(!this.showPreview, (el) => el.flex_1())
              .h_full().min_h(0)
              .child(v_flex().id("ss-list").flex_1().min_h(0).overflow_y_scrollbar().track_scroll(this.scroll).children(rows.map((r) => {
                const on = r.key === sel?.key;
                return h_flex().id(`ss-${r.key}`).w_full().gap(8).items_center().pl(on ? 7 : 10).pr(8).py(1).cursor_pointer()
                  .when(on, (el) => el.bg(t.selected).border_l(3).border_color(t.accent))
                  .hover((s) => (on ? s : s.bg(t.hover)))
                  .on_click((ev, cx) => { if (ev.click_count > 1 || on) this.connect(r); else { this.selKey = r.key; this.refresh(cx); cx.notify(); } })
                  .child(this.icon(t, r))
                  .child(div().flex_1().min_w(0).truncate().text_color(t.foreground).when(r.kind === "space" && r.space.active, (d) => d.font_bold()).child(r.name))
                  .when(r.status === "blocked", (el) => el.child(div().text_color(t.blocked).text_size(11).child("needs you")));
              }))),
          )
          .when(this.showPreview, (el) => el.child(
            v_flex().flex_1().h_full().min_w(0).pl(18).child(v_flex().id("ss-preview").flex_1().min_h(0).overflow_hidden()
              .on_size_change((size, cx) => {
                const r = Math.max(5, Math.floor(size.height / ((settings().zoom ?? 1) * 13 * 1.45)));
                if (r !== this.previewRows) { this.previewRows = r; cx.notify(); }
              })
              .children(tail.map((l, n) => div().id(`sp-${n}`).whitespace_nowrap().overflow_hidden()
                .text_color(sel?.kind === "space" ? t.foreground : l.endsWith("/") ? t.ansi4 ?? t.accent : t.foreground).child(l || " ")))),
          )),
      )
      .child(h_flex().flex_none().h(40).gap(8).items_center().border_t(1).border_color(t.border).mt(6)
        .child(hint("↵", "connect")).child(dot()).child(hint("^o", this.showPreview ? "hide preview" : "show preview")).child(dot())
        .child(hint("^a ^t ^g ^z ^r", "source")).child(dot()).child(hint("^x", "kill")).child(dot()).child(hint("esc", "quit")));
  }
}

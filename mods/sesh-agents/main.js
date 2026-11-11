// The agent picker (cmd-a) for Herdr: every thread, in the sidebar's
// sections, with the selected agent's screen (or its diff) beside the list.
// Type to filter (fzf style), act on an agent without leaving.

import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex, InputState, Input, ScrollHandle } from "gpui-base";
import { run, theme, props, closeOverlay, hc, exec, focused, settings, spaces } from "herdr";
import * as L from "./lib.js";

const KEYS = [
  ["enter", "go to the agent"],
  ["ctrl-y", "approve a waiting agent"],
  ["ctrl-x", "deny a waiting agent, interrupt a working one"],
  ["ctrl-o", "reply"],
  ["ctrl-s", "mark a finished agent seen"],
  ["ctrl-g", "show its diff instead of its screen, and back"],
  ["alt-d", "the full review, in a pane"],
  ["ctrl-d / ctrl-u", "scroll the preview"],
  ["alt-p alt-s alt-z alt-e", "pin, settle, snooze 1h, wake"],
  ["alt-b", "open its pull request"],
  ["alt-w", "close its pane (press twice; forgets a closed thread)"],
  ["alt-space / alt-m", "its actions, by the sidebar's letters"],
  ["↑ ↓ / ctrl-n ctrl-p", "move"],
  ["f1", "every key"],
  ["esc", "quit"],
];
const SHELVES = [
  { key: "working", label: "Working", icon: "working" },
  { key: "snoozed", label: "Snoozed", icon: "clock" },
  { key: "settled", label: "Settled", icon: "box" },
];

export default class Agents extends View {
  init(_props, cx) {
    this.filter = "";
    this.sel = null;
    this.open = { working: true, snoozed: false, settled: false };
    this.diff = false;
    this.help = false;
    this.replying = null; // the address you're replying to (set by ctrl-o), else null
    this.busy = false;
    this.wheel = 0;
    this.scrolledTo = null;
    this.shelfScroll = ScrollHandle.new();
    this.screen = {};
    this.diffs = {};
    this.offset = 0;
    this.previewRows = 30;
    this.scroll = ScrollHandle.new();
    this.input = InputState.new({ placeholder: "filter by title, space or branch" });
    this.input.on("change", (_e, cx) => {
      if (this.replying) return;
      this.filter = this.input.value();
      this.sel = null;
      cx.notify();
    });
    this.input.on("submit", (_e, cx) => {
      const th = this.current();
      if (this.replying) {
        // To the agent you pressed ctrl-o on, wherever the selection has gone since.
        const to = L.threads().all.find((x) => x.address === this.replying);
        const text = this.input.value();
        this.endReply();
        if (to) cx.spawn(async () => L.reply(to, text));
        cx.notify();
        return;
      }
      if (th) { this.close(); L.go(th); }
    });
    this.input.focus();
    cx.timer.after(300, () => this.input.focus());
    this.facts = new L.Facts();
    this.facts.start(cx, () => this.model().all);
    cx.timer.every(1000, (cx) => this.refresh(cx));
    this.refresh(cx);
  }

  close() { closeOverlay(props().overlay); }
  // Back to filtering: the box gets the filter again (while still "replying", so the
  // change event this fires doesn't reset the selection).
  endReply() {
    this.input.set_value(this.filter);
    this.replying = null;
  }
  machineOf(th) { return spaces().find((p) => p.id === th.space)?.machine ?? null; }

  model() { return L.threads({ filter: this.filter }); }
  items(m) {
    const out = [...m.pinned, ...m.inbox];
    for (const s of SHELVES) {
      out.push({ shelf: s.key });
      if (this.open[s.key] || this.filter) out.push(...m[s.key]);
    }
    return out;
  }
  keyOf(it) { return it.shelf ? `shelf:${it.shelf}` : it.address; }
  current() { return this.model().all.find((t) => t.address === this.sel) ?? null; }

  refresh(cx) {
    const th = this.current();
    // One look at a time: a big diff can take longer than the 1 s timer.
    if (!th || th.closed || this.busy) return;
    this.busy = true;
    cx.spawn(async (cx) => {
      try {
        if (this.diff) {
          if (!th.cwd) { this.diffs[th.address] = ["(no folder)"]; return; }
          // On the agent's own machine: its folder is a path there.
          const on = this.machineOf(th);
          try {
            const stat = await exec("git", ["diff", "HEAD", "--stat"], { cwd: th.cwd, on, timeout: 5000 });
            if (stat.code !== 0) { this.diffs[th.address] = [stat.stderr.trim() || "(not a git repository)"]; return; }
            const body = await exec("git", ["diff", "HEAD"], { cwd: th.cwd, on, timeout: 5000 });
            const text = (stat.stdout + "\n" + body.stdout).trim();
            this.diffs[th.address] = text ? text.split("\n") : ["(no changes)"];
          } catch { this.diffs[th.address] = ["(couldn't read the diff)"]; }
        } else {
          const r = await hc(["read", th.address, "--lines", "400"]);
          if (r.code === 0) this.screen[th.address] = L.screenLines(r.stdout);
        }
      } finally {
        this.busy = false;
        cx.notify();
      }
    });
  }
  // How far the preview can scroll: past its last line there's nothing to show.
  maxOffset(th) {
    const lines = (th && (this.diff ? this.diffs[th.address] : this.screen[th.address])) ?? [];
    return Math.max(0, lines.length - this.previewRows);
  }

  move(items, d) {
    if (!items.length) return;
    const i = items.findIndex((it) => this.keyOf(it) === this.sel);
    this.sel = this.keyOf(items[(Math.max(0, i) + d + items.length) % items.length]);
    this.offset = 0;
  }

  key(e, cx) {
    const m = this.model();
    const items = this.items(m);
    const it = items.find((x) => this.keyOf(x) === this.sel);
    const th = it && !it.shelf ? it : null;
    const k = e.key;
    const c = e.modifiers.control;
    const a = e.modifiers.alt;
    if (this.help) { this.help = false; }
    else if (k === "escape") {
      if (this.replying) this.endReply();
      else return this.close();
    }
    else if (k === "down" || (c && (k === "n" || k === "j"))) { this.move(items, 1); this.refresh(cx); }
    else if (k === "up" || (c && (k === "p" || k === "k"))) { this.move(items, -1); this.refresh(cx); }
    else if (k === "enter" && it?.shelf) { this.open[it.shelf] = !this.open[it.shelf]; }
    else if (c && k === "y" && th) cx.spawn(async () => L.approve(th));
    else if (c && k === "x" && th) cx.spawn(async () => L.deny(th));
    else if (c && k === "o" && th && !th.closed) { this.replying = th.address; this.input.set_value(""); this.input.focus(); }
    else if (c && k === "s" && th) cx.spawn(async () => L.markSeen(th));
    else if (c && k === "g") { this.diff = !this.diff; this.offset = 0; this.busy = false; this.refresh(cx); }
    else if (c && (k === "d" || k === "u")) {
      const half = Math.floor(this.previewRows / 2);
      const down = k === "d";
      // The screen counts back from its last line; the diff counts down from its first.
      this.offset = Math.max(0, Math.min(this.maxOffset(th), this.offset + (this.diff === down ? half : -half)));
    }
    else if (a && (k === "space" || k === "m")) { const pr = th ? this.facts.of(th).git?.pr ?? null : null; this.close(); L.openMenu(th, { from: "picker", pr }); return; }
    else if (a && k === "p" && th) L.togglePin(th);
    else if (a && k === "s" && th) L.toggleSettle(th);
    else if (a && k === "z" && th) L.snooze(th, Date.now() + 3600e3);
    else if (a && k === "e" && th) L.wake(th);
    else if (a && k === "b" && th?.cwd) L.openPR(th);
    else if (a && k === "w" && th) L.closeThread(th);
    else if (a && k === "d" && th?.cwd) { this.close(); L.review(th); return; }
    else if (k === "f1") this.help = true;
    else return;
    cx.stop_propagation();
    cx.notify();
  }

  render() {
    const t = theme();
    const m = this.model();
    const items = this.items(m);
    if (!items.some((it) => this.keyOf(it) === this.sel)) {
      const here = m.all.find((x) => x.address === focused()?.terminal);
      const first = m.order[0] ?? items.find((x) => !x.shelf);
      this.sel = here && items.includes(here) && !this.filter ? here.address : first ? this.keyOf(first) : null;
    }
    const now = Date.now();
    const th = this.current();

    const list = [];
    let selChild = -1, selShelf = -1;
    const row = (x) => {
      if (x.address === this.sel) selChild = list.length;
      const f = this.facts.of(x);
      list.push(L.threadRow(t, x, {
        selected: x.address === this.sel, now, need: f.need, git: f.git, id: `ag-${x.address}`, wide: true,
        onMenu: () => { this.close(); L.openMenu(x, { from: "picker", pr: f.git?.pr ?? null }); },
        onClick: (ev, cx) => { if (ev.click_count > 1) { this.close(); L.go(x); } else { this.sel = x.address; this.offset = 0; this.refresh(cx); cx.notify(); } },
      }));
    };
    if (m.pinned.length) { list.push(L.sectionHead(t, "pin", "PINNED", m.pinned.length)); m.pinned.forEach(row); }
    list.push(L.sectionHead(t, "inbox", "INBOX", m.inbox.length));
    m.inbox.forEach(row);
    const shelves = [];
    for (const s of SHELVES) {
      const key = `shelf:${s.key}`;
      const on = this.sel === key;
      const isOpen = this.open[s.key] || !!this.filter;
      if (on) selShelf = shelves.length;
      shelves.push(h_flex().id(key).pl(on ? 7 : 10).pr(10).py(2).gap(6).items_center().cursor_pointer()
        .when(on, (el) => el.bg(t.selected).border_l(3).border_color(t.accent))
        .hover((st) => (on ? st : st.bg(t.hover)))
        .on_click((_e, cx) => { this.open[s.key] = !this.open[s.key]; this.sel = key; cx.notify(); })
        .child(div().w(10).text_color(t.dim).child(isOpen ? "▾" : "▸"))
        .child(svg(L.ICON(s.icon)).size(12).text_color(s.key === "working" ? t.working : t.muted))
        .child(div().flex_1().child(s.label))
        .child(div().text_color(t.muted).child(String(m[s.key].length))));
      if (isOpen) for (const x of m[s.key]) {
        if (x.address === this.sel) selShelf = shelves.length;
        const f = this.facts.of(x);
        shelves.push(L.threadRow(t, x, { selected: x.address === this.sel, now, need: f.need, git: f.git, id: `as-${x.address}`, wide: true,
          onMenu: () => { this.close(); L.openMenu(x, { from: "picker", pr: f.git?.pr ?? null }); },
          onClick: (ev, cx) => { if (ev.click_count > 1) { this.close(); L.go(x); } else { this.sel = x.address; this.offset = 0; this.refresh(cx); cx.notify(); } } }));
      }
    }
    // Bring the selection into view when it changes, not on every redraw (the timer
    // redraws every second, which would undo scrolling the list with the mouse).
    if (this.sel !== this.scrolledTo) {
      this.scrolledTo = this.sel;
      if (selChild >= 0) this.scroll.scroll_to_item(selChild);
      if (selShelf >= 0) this.shelfScroll.scroll_to_item(selShelf);
    }

    const badge = (icon, color, n) => h_flex().gap(3).items_center().text_color(color).child(svg(L.ICON(icon)).size(11).text_color(color)).child(String(n));
    const hint = (k, d) => h_flex().gap(4).child(div().font_bold().text_color(t.accent).child(k)).child(div().text_color(t.muted).child(d));
    const dot = () => div().text_color(t.dim).child("·");

    return v_flex()
      .id("sesh-agents")
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
      .child(L.hatchTitle(t, "Agents", div().id("ag-new").cursor_pointer().text_color(t.accent).on_click(() => { this.close(); run("tab.new", { argv: ["claude"] }); }).child("+")))
      .child(h_flex().items_center().gap(10).text_color(t.muted)
        .child(div().flex_1().child(`all spaces · ${m.all.length}`))
        .when(m.counts.blocked > 0, (el) => el.child(badge("bell", t.blocked, m.counts.blocked)))
        .when(m.counts.working > 0, (el) => el.child(badge("working", t.working, m.counts.working)))
        .when(m.counts.done > 0, (el) => el.child(badge("check", t.done, m.counts.done))))
      .child(h_flex().items_center().gap(6).pt(2).pb(8)
        .child(div().text_color(t.accent).font_bold().child(this.replying ? `reply to ${m.all.find((x) => x.address === this.replying)?.title ?? ""} ›` : "›"))
        .child(Input.new(this.input).flex_1().text_size(13).text_color(t.foreground)))
      .child(
        h_flex().flex_1().min_h(0).items_start()
          .child(
            v_flex().w("46%").h_full().min_h(0).pr(10).border_r(1).border_color(t.border)
              .child(v_flex().id("ag-list").flex_1().min_h(0).overflow_y_scrollbar().track_scroll(this.scroll).children(list))
              .child(v_flex().id("ag-shelves").flex_none().max_h("45%").overflow_y_scrollbar().track_scroll(this.shelfScroll).pt(6).border_t(1).border_color(t.border).children(shelves)),
          )
          .child(this.help ? this.keysView(t) : this.preview(t, th)),
      )
      .child(h_flex().flex_none().h(40).gap(8).items_center().border_t(1).border_color(t.border).mt(6)
        .child(hint("^y", "approve")).child(dot()).child(hint("^x", "deny")).child(dot()).child(hint("^o", "reply")).child(dot())
        .child(hint("↵", "go")).child(dot()).child(hint("alt+z", "snooze")).child(dot()).child(hint("^g", this.diff ? "screen" : "diff")).child(dot()).child(hint("alt+m", "more")).child(dot()).child(hint("f1", "keys")));
  }

  keysView(t) {
    return v_flex().flex_1().h_full().pl(16).gap(3)
      .child(div().font_bold().text_color(t.accent).pb(4).child("KEYS"))
      .children(KEYS.map(([k, d]) => h_flex().gap(12).child(div().w(190).flex_none().text_color(t.accent).font_bold().child(k)).child(div().flex_1().text_color(t.muted).child(d))));
  }

  preview(t, th) {
    if (!th) return v_flex().flex_1().h_full().pl(16).text_color(t.dim).child("no agent selected");
    if (th.closed) return v_flex().flex_1().h_full().min_w(0).pl(16).gap(6)
      .child(h_flex().gap(8).items_center().text_color(t.muted).font_bold()
        .child(div().flex_none().child(`CLOSED · ${th.title.toUpperCase()}`)).child(div().flex_1().h(1).bg(t.border)))
      .child(div().text_color(t.dim).child(`${th.spaceName || "no space"} · ${(th.cwd ?? "").replace(/^\/Users\/[^/]+/, "~")} · closed ${L.age(Date.now() - th.at)} ago`))
      .child(div().text_color(t.muted).child(`↵ resumes it (${th.app === "codex" ? "codex resume" : "claude --resume"} ${th.id.slice(0, 8)}…) · alt-w forgets it`));
    const f = this.facts.of(th);
    const lines = (this.diff ? this.diffs[th.address] : this.screen[th.address]) ?? [];
    const rows = this.previewRows;
    const end = Math.max(0, lines.length - this.offset);
    const shown = this.diff ? lines.slice(this.offset, this.offset + rows) : lines.slice(Math.max(0, end - rows), end);
    const color = (l) => {
      if (!this.diff) return t.foreground;
      if (/^\+(?!\+\+)/.test(l)) return t.done;
      if (/^-(?!--)/.test(l)) return t.ansi1 ?? t.blocked;
      if (/^@@/.test(l)) return t.accent;
      if (/^(diff|index|\+\+\+|---)/.test(l)) return t.muted;
      return t.foreground;
    };
    const statusWord = { blocked: "waiting", done: "done", working: "working", idle: "idle" }[th.status];
    const statusColor = t[th.status] ?? t.dim;
    return v_flex().flex_1().h_full().min_w(0).pl(16)
      .child(h_flex().gap(8).items_center().text_color(t.muted).font_bold()
        .child(div().flex_none().child(`${this.diff ? "DIFF" : "SCREEN"} · ${th.title.toUpperCase()}`))
        .child(div().flex_1().h(1).bg(t.border)))
      .child(h_flex().gap(6).items_center()
        .child(L.glyph(t, th.status))
        .child(div().text_color(statusColor).child(statusWord))
        .child(div().text_color(t.dim).child(`· ${L.age(Date.now() - th.at)} · ${th.spaceName} › ${th.a?.tab_name ?? ""} · ${th.a?.program?.name ?? "agent"}`)))
      .child(h_flex().gap(10).text_color(t.dim)
        .child(div().truncate().child((th.cwd ?? "").replace(/^\/Users\/[^/]+/, "~")))
        .when(!!th.branch, (el) => el.child(div().text_color(t.muted).child(th.branch)))
        .when(!!f.git?.ab?.ahead, (el) => el.child(`↑${f.git.ab.ahead}`))
        .when(!!f.git?.pr, (el) => el.child(div().text_color(t.done).child(`#${f.git.pr.number}`))))
      .when(!!f.need, (el) => el.child(div().text_color(t.blocked).child(f.need)))
      .child(div().h(1).bg(t.border).my(6))
      .child(
        v_flex().id("ag-preview").flex_1().min_h(0).overflow_hidden()
          .on_size_change((size, cx) => {
            const lh = 13 * 1.45;
            const r = Math.max(5, Math.floor(size.height / ((settings().zoom ?? 1) * lh)));
            if (r !== this.previewRows) { this.previewRows = r; cx.notify(); }
          })
          .on_scroll_wheel((e, cx) => {
            // A negative delta scrolls down (as in Herdr's own lists). Small trackpad
            // steps add up until they make a whole line.
            this.wheel += e.delta_lines?.y ?? e.delta.y / 18;
            const n = Math.trunc(this.wheel);
            this.wheel -= n;
            if (n) {
              // The screen counts back from its last line; the diff counts down from its first.
              this.offset = Math.max(0, Math.min(this.maxOffset(th), this.offset + (this.diff ? -n : n)));
              cx.notify();
            }
            cx.stop_propagation();
          })
          .children(shown.map((l, n) => div().id(`pl-${n}`).whitespace_nowrap().overflow_hidden().text_color(color(l)).child(l || " "))),
      );
  }
}

// The thread sidebar from ksesh, for Herdr: every agent sorted into Pinned,
// Inbox, and the Working / Snoozed / Settled shelves (closed threads too,
// resumable). Click a thread to go to it, right click (or space / m) for its
// actions, or come in with cmd-shift-b and drive it from the keyboard (? lists keys).

import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex, InputState, Input, ScrollHandle } from "gpui-base";
import { focused, run, theme, nextCommand, setAgentOrder, layout, shared, setShared } from "herdr";
import * as L from "./lib.js";

const SHELVES = [
  { key: "working", label: "Working", icon: "working" },
  { key: "snoozed", label: "Snoozed", icon: "clock" },
  { key: "settled", label: "Settled", icon: "box" },
];

const KEYS = [
  ["enter / click", "go to the thread (open a shelf, resume a closed one)"],
  ["esc", "back to your window"],
  ["j k g G / tab", "move / next section"],
  ["1–9", "go to the nth thread"],
  ["a  x  r", "approve, deny or interrupt, reply"],
  ["p / K J", "pin or unpin / move a pin up or down"],
  ["s", "settle or restore"],
  ["z / w", "snooze (1h … next week, or any time) / wake"],
  ["e", "mark a finished thread seen"],
  ["u", "undo the last pin, settle, snooze or wake"],
  ["d", "review its changes"],
  ["P", "open its pull request"],
  ["n / N", "new thread / new thread in a new worktree"],
  ["R", "rename the thread"],
  ["X", "close its pane (forget a closed thread)"],
  ["A", "archive: remove its worktree"],
  ["space / m / right click", "everything you can do to it, as a menu"],
  ["/", "filter"],
  ["o / f", "group the inbox by space / only this space"],
  ["< >", "narrower, wider"],
  ["q", "hide the sidebar"],
  ["?", "every key"],
];

function find(node, id) {
  if (node?.id === id) return node;
  for (const c of node?.children ?? []) { const f = find(c, id); if (f) return f; }
  return null;
}

export default class Sidebar extends View {
  init(_props, cx) {
    this.handle = cx.focus_handle();
    this.scroll = ScrollHandle.new();
    this.sel = null; // address, or "shelf:<key>"
    this.open = { working: false, snoozed: false, settled: false };
    this.mode = null; // "filter" | "reply" | "help"
    const v = shared("sesh.view") ?? {};
    this.only = !!v.only;
    this.group = !!v.group;
    this.filter = "";
    this.back = null;
    this.hover = null;
    this.filterInput = InputState.new({ placeholder: "filter by title, space or branch" });
    this.filterInput.on("change", (_e, cx) => { this.filter = this.filterInput.value(); cx.notify(); });
    this.filterInput.on("submit", (_e, cx) => { this.mode = null; this.handle.focus(); cx.notify(); });
    this.replyInput = InputState.new({ placeholder: "reply…" });
    this.replyInput.on("submit", (_e, cx) => {
      const th = this.current();
      const text = this.replyInput.value();
      this.replyInput.set_value("");
      this.mode = null;
      this.handle.focus();
      if (th) cx.spawn(async () => L.reply(th, text));
      cx.notify();
    });
    this.facts = new L.Facts();
    this.facts.start(cx, () => this.model().all);
    cx.timer.every(30000, (cx) => cx.notify());
    cx.spawn(async (cx) => {
      for (;;) {
        const c = await nextCommand();
        this.command(c);
        cx.notify();
      }
    });
  }

  // Commands from keys (sesh-bar) and the actions menu's Sidebar items.
  command(c) {
    if (c === "focus" || c === "enter") this.enter();
    else if (c === "toggle-focus") { if (this.handle.is_focused()) this.leave(); else this.enter(); }
    else if (c === "filter") { this.enter(); this.mode = "filter"; this.filterInput.focus(); }
    else if (c === "group") this.setView({ group: !this.group });
    else if (c === "only") this.setView({ only: !this.only });
    else if (c === "narrow" || c === "wide") this.resize(c === "wide" ? 24 : -24);
    else if (c === "help") { this.enter(); this.mode = "help"; }
    else if (c === "hide") { this.leave(); run("layout.hide", { id: "sidebar" }); }
  }
  setView(v) {
    if ("group" in v) this.group = v.group;
    if ("only" in v) this.only = v.only;
    setShared("sesh.view", { group: this.group, only: this.only });
  }
  resize(d) {
    const w = find(layout(), "sidebar")?.size?.px ?? 340;
    run("layout.resize", { id: "sidebar", px: Math.max(220, Math.min(560, w + d)) });
  }

  enter() {
    const f = focused();
    if (f?.pane) this.back = f.pane;
    this.handle.focus();
  }
  leave() {
    this.mode = this.mode === "help" ? null : this.mode;
    if (this.back) run("pane.focus", { pane: this.back });
    else run("pane.go", { direction: "right" });
  }

  model() {
    const space = this.only ? focused()?.space ?? null : null;
    return L.threads({ filter: this.filter, space });
  }

  // The rows you can move through, in screen order.
  items(m) {
    const out = [...m.pinned, ...this.inboxOrder(m)];
    for (const s of SHELVES) {
      out.push({ shelf: s.key });
      if (this.open[s.key] || this.filter) out.push(...m[s.key]);
    }
    return out;
  }
  inboxOrder(m) {
    if (!this.group) return m.inbox;
    const bySpace = new Map();
    for (const th of m.inbox) { if (!bySpace.has(th.spaceName)) bySpace.set(th.spaceName, []); bySpace.get(th.spaceName).push(th); }
    return [...bySpace.values()].flat();
  }
  keyOf(it) { return it.shelf ? `shelf:${it.shelf}` : it.address; }
  current() {
    const m = this.model();
    return m.all.find((t) => t.address === this.sel) ?? null;
  }

  activate(it, cx) {
    if (!it) return;
    if (it.shelf) { this.open[it.shelf] = !this.open[it.shelf]; this.sel = `shelf:${it.shelf}`; cx?.notify(); return; }
    this.sel = it.address;
    if (this.mode === "filter") this.mode = null;
    L.go(it);
  }
  menu(th, at = "pointer") {
    L.openMenu(th, { at, from: "sidebar", pr: th?.cwd ? this.facts.git[th.cwd]?.pr ?? null : null });
  }
  act(name, th, cx) {
    if (name === "snooze1h") L.snooze(th, Date.now() + 3600e3);
    else L.runAction(name, th);
    cx?.notify();
  }

  key(e, cx) {
    const k = e.key;
    // Typing in the filter or reply box belongs to the box: only Esc is ours.
    if (this.mode === "filter" || this.mode === "reply") {
      if (k === "escape") {
        if (this.mode === "filter") { this.filter = ""; this.filterInput.set_value(""); }
        else this.replyInput.set_value("");
        this.mode = null;
        this.handle.focus();
        cx.stop_propagation();
        cx.notify();
      }
      return;
    }
    const m = this.model();
    const items = this.items(m);
    const i = Math.max(0, items.findIndex((it) => this.keyOf(it) === this.sel));
    const it = items[i];
    const th = it && !it.shelf ? it : null;
    const live = th && !th.closed ? th : null;
    const ch = e.key_char ?? "";
    const mod = e.modifiers;
    if (mod.platform || mod.control || mod.alt) return;
    if (this.mode === "help") { this.mode = null; cx.stop_propagation(); cx.notify(); return; }

    const move = (j) => { if (items.length) this.sel = this.keyOf(items[(j + items.length) % items.length]); };
    if (k === "escape") { if (this.filter) { this.filter = ""; this.filterInput.set_value(""); } else this.leave(); }
    else if (k === "down" || ch === "j") move(i + 1);
    else if (k === "up" || ch === "k") move(i - 1);
    else if (ch === "g") move(0);
    else if (ch === "G") move(items.length - 1);
    else if (k === "tab") {
      const sec = (x) => (x.shelf ? x.shelf : x.pinned ? "pinned" : "inbox");
      const here = it ? sec(it) : null;
      const j = items.findIndex((x, n) => n > i && sec(x) !== here);
      move(j < 0 ? 0 : j);
    }
    else if (k === "enter") this.activate(it, cx);
    else if (k === "space" || ch === "m") this.menu(th);
    else if (/^[1-9]$/.test(ch)) this.activate(m.order[Number(ch) - 1], cx);
    else if (ch === "a" && live) cx.spawn(async () => L.approve(live));
    else if (ch === "x" && live) cx.spawn(async () => L.deny(live));
    else if (ch === "r" && live) { this.mode = "reply"; this.replyInput.set_value(""); this.replyInput.focus(); }
    else if (ch === "p" && live) L.togglePin(live);
    else if (ch === "K" && live?.pinned) L.movePin(live, -1);
    else if (ch === "J" && live?.pinned) L.movePin(live, 1);
    else if (ch === "s" && live) L.toggleSettle(live);
    else if (ch === "z" && live) L.openMenu(live, { from: "sidebar", view: "snooze" });
    else if (ch === "w" && live) L.wake(live);
    else if (ch === "e" && live) cx.spawn(async () => L.markSeen(live));
    else if (ch === "u") L.undo();
    else if (ch === "d" && live) L.review(live);
    else if (ch === "P" && live) L.openPR(live);
    else if (ch === "n") L.newThread(th);
    else if (ch === "N") L.newWorktreeThread(th);
    else if (ch === "R" && live) L.rename(live);
    else if (ch === "X" && th) L.closeThread(th);
    else if (ch === "A" && live) L.archive(live);
    else if (ch === "/") { this.mode = "filter"; this.filterInput.focus(); }
    else if (ch === "o") this.setView({ group: !this.group });
    else if (ch === "f") this.setView({ only: !this.only });
    else if (ch === "<" || ch === ">") this.resize(ch === ">" ? 24 : -24);
    else if (ch === "q") this.command("hide");
    else if (ch === "?") this.mode = "help";
    else return;
    cx.stop_propagation();
    cx.notify();
  }

  render() {
    const t = theme();
    const m = this.model();
    setAgentOrder(m.order.map((x) => x.address));
    const items = this.items(m);
    if (!items.some((it) => this.keyOf(it) === this.sel)) {
      const here = m.all.find((x) => !x.closed && x.a.focused);
      this.sel = here ? here.address : items[0] ? this.keyOf(items[0]) : null;
    }
    const now = Date.now();
    const kids = [];
    let selChild = -1;
    const rowOf = (th, prefix) => L.threadRow(t, th, {
      selected: th.address === this.sel, now, ...this.facts.of(th), id: `${prefix}-${th.address}`,
      hovered: this.hover === th.address,
      onHover: (h, cx) => { const v = h ? th.address : this.hover === th.address ? null : this.hover; if (v !== this.hover) { this.hover = v; cx.notify(); } },
      onClick: (_e, cx) => { this.activate(th, cx); cx.notify(); },
      onAct: (name, cx) => this.act(name, th, cx),
      onMenu: (_e, cx) => { this.sel = th.address; this.menu(th); cx.notify(); },
    });
    const row = (th) => {
      if (th.address === this.sel) selChild = kids.length;
      kids.push(rowOf(th, "th"));
      if (th.address === this.sel && this.mode === "reply") kids.push(this.replyBox(t));
    };

    if (m.pinned.length) { kids.push(L.sectionHead(t, "pin", "PINNED", m.pinned.length)); m.pinned.forEach(row); }
    kids.push(L.sectionHead(t, "inbox", "INBOX", m.inbox.length));
    if (this.group) {
      let last = null;
      for (const th of this.inboxOrder(m)) {
        if (th.spaceName !== last) { last = th.spaceName; kids.push(div().pl(10).pt(4).text_color(t.dim).child(`${last}`)); }
        row(th);
      }
    } else m.inbox.forEach(row);
    if (!m.inbox.length) kids.push(div().pl(10).py(4).text_color(t.dim).child(this.filter ? "nothing matches" : "nothing needs you"));

    // The shelves, folded at the bottom.
    const shelfKids = [];
    for (const s of SHELVES) {
      const key = `shelf:${s.key}`;
      const on = this.sel === key;
      const isOpen = this.open[s.key] || !!this.filter;
      shelfKids.push(
        h_flex().id(key).w_full().pl(on ? 7 : 10).pr(10).py(2).gap(6).items_center().cursor_pointer()
          .when(on, (el) => el.bg(t.selected).border_l(3).border_color(t.accent))
          .hover((st) => (on ? st : st.bg(t.hover)))
          .on_click((_e, cx) => { this.activate({ shelf: s.key }, cx); })
          .child(div().w(10).flex_none().text_color(t.dim).child(isOpen ? "▾" : "▸"))
          .child(svg(L.ICON(s.icon)).size(12).flex_none().text_color(s.key === "working" ? t.working : t.muted))
          .child(div().flex_1().text_color(t.foreground).child(s.label))
          .child(div().flex_none().text_color(t.muted).child(String(m[s.key].length))),
      );
      if (isOpen) for (const th of m[s.key]) {
        shelfKids.push(rowOf(th, "sh"));
        if (th.address === this.sel && this.mode === "reply") shelfKids.push(this.replyBox(t));
      }
    }
    if (selChild >= 0) this.scroll.scroll_to_item(selChild);

    const counts = m.counts;
    const badge = (icon, color, n) => h_flex().gap(3).items_center().text_color(color).child(svg(L.ICON(icon)).size(11).text_color(color)).child(String(n));

    return v_flex()
      .id("sesh-sidebar")
      .track_focus(this.handle)
      .on_key_down((e, cx) => this.key(e, cx))
      .size_full()
      .bg(t.chrome)
      .text_size(13)
      .text_color(t.foreground)
      .pt(40)
      .child(
        v_flex().px(12).pb(6).gap(2).flex_none()
          .child(L.hatchTitle(t, "Threads",
            h_flex().gap(6).flex_none().items_center()
              .child(div().id("th-more").cursor_pointer().text_color(t.muted).hover((s) => s.text_color(t.foreground))
                .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
                .on_click(() => this.menu(null)).child(svg(L.ICON("more")).size(13).text_color(t.muted)))
              .child(div().id("th-new").cursor_pointer().text_color(t.accent).hover((s) => s.text_color(t.foreground))
                .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
                .on_click(() => L.newThread(null)).child("+"))))
          .child(h_flex().items_center().gap(8).text_color(t.muted)
            .child(div().flex_1().truncate().child(`${this.only ? "this space" : "all spaces"} · ${m.all.filter((x) => !x.closed).length}${this.group ? " · grouped" : ""}`))
            .when(counts.blocked > 0, (el) => el.child(badge("bell", t.blocked, counts.blocked)))
            .when(counts.working > 0, (el) => el.child(badge("working", t.working, counts.working)))
            .when(counts.done > 0, (el) => el.child(badge("check", t.done, counts.done))))
          .when(this.mode === "filter" || !!this.filter, (el) => el.child(
            h_flex().items_center().gap(6).pt(4)
              .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
              .child(div().text_color(t.accent).font_bold().child("›"))
              .child(Input.new(this.filterInput).flex_1().text_size(13).text_color(t.foreground)))),
      )
      .when(this.mode === "help", (el) => el.child(this.help(t)))
      .when(this.mode !== "help", (el) => el
        .child(
          v_flex().id("th-list").flex_1().min_h(0).overflow_y_scrollbar().track_scroll(this.scroll).px(2)
            .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
            .on_mouse_down("right", (_e, cx) => { this.menu(null); cx.stop_propagation(); })
            .children(kids),
        )
        .child(
          v_flex().id("th-shelves").flex_none().max_h(340).overflow_y_scrollbar().mx(12).pt(6).border_t(1).border_color(t.border)
            .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
            .children(shelfKids),
        ))
      .child(this.footer(t));
  }

  replyBox(t) {
    return h_flex().id("reply").mx(10).my(4).px(8).h(28).gap(6).items_center().rounded(4).border_1().border_color(t.accent).bg(t.background)
      .child(div().text_color(t.accent).font_bold().child("›"))
      .child(Input.new(this.replyInput).flex_1().text_size(13).text_color(t.foreground));
  }

  help(t) {
    return v_flex().id("th-help").flex_1().min_h(0).overflow_y_scrollbar().px(12).pt(8).gap(3)
      .child(div().font_bold().text_color(t.accent).pb(4).child("Keys (any key closes)"))
      .children(KEYS.map(([k, d]) => h_flex().gap(10).child(div().w(120).flex_none().text_color(t.accent).font_bold().child(k)).child(div().flex_1().text_color(t.muted).child(d))));
  }

  // The keys for the selected thread's state, as ksesh's footer shows them.
  footer(t) {
    const th = this.current();
    const hint = (k, d) => h_flex().gap(4).child(div().font_bold().text_color(t.accent).child(k)).child(div().text_color(t.muted).child(d));
    const dot = () => div().text_color(t.dim).child("·");
    let hints;
    if (this.mode === "reply") hints = [["↵", "send"], ["esc", "cancel"]];
    else if (this.mode === "filter") hints = [["↵", "keep"], ["esc", "clear"]];
    else if (!th) hints = [["n", "new"], ["/", "filter"], ["m", "more"], ["?", "keys"]];
    else if (th.closed) hints = [["↵", "resume"], ["X", "forget"], ["m", "more"]];
    else if (th.status === "blocked") hints = [["a", "approve"], ["x", "deny"], ["r", "reply"], ["m", "more"]];
    else if (th.status === "working") hints = [["x", "interrupt"], ["r", "reply"], ["z", "snooze"], ["m", "more"]];
    else hints = [["d", "review"], ["s", th.settled ? "restore" : "settle"], ["r", "reply"], ["m", "more"]];
    const row = h_flex().flex_none().h(34).mx(12).gap(6).items_center().border_t(1).border_color(t.border).overflow_hidden();
    hints.forEach(([k, d], n) => { if (n) row.child(dot()); row.child(hint(k, d)); });
    return row;
  }
}

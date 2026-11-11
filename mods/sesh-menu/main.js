// ksesh's actions menu for the herdr-sesh layout: everything you can do to a
// thread, by the sidebar's letters (a approves, p pins, d reviews…), opened by
// a right click on a thread, `space` / `m` in the sidebar, or alt+space / alt+m
// in the agent picker. Without a thread it's the sidebar's own options.
// Snooze and Sidebar open their own lists in place; Reply… asks for the text here.
// Props: { address, from: "sidebar" | "picker", pr: { number } | null, overlay }.

import { View, div, svg } from "gpui-kit";
import { v_flex, h_flex, InputState, Input } from "gpui-base";
import { theme, props, closeOverlay, shared } from "herdr";
import * as L from "./lib.js";

export default class Menu extends View {
  init(_props, cx) {
    this.handle = cx.focus_handle();
    this.view = props()?.view === "snooze" ? "snooze" : "main"; // main | snooze | reply | more | when
    this.idx = 0;
    this.error = "";
    this.inputs = {
      reply: InputState.new({ placeholder: "reply…" }),
      when: InputState.new({ placeholder: "90m, 3h, 15:04, evening, tomorrow, next-week" }),
    };
    for (const i of Object.values(this.inputs)) i.on("submit", (_e, cx) => this.submit(cx));
    this.handle.focus();
    cx.timer.after(60, () => { if (this.view === "main") this.handle.focus(); });
  }

  close() { closeOverlay(props()?.overlay); }
  thread() {
    const a = props()?.address;
    return a ? L.threads().all.find((t) => t.address === a) ?? null : null;
  }
  opts() { return shared("sesh.view") ?? {}; }

  items() {
    const th = this.thread();
    if (this.view === "more" || !th) return L.viewMenu(this.opts()).filter((x) => props()?.from !== "picker" || !/^view:(filter|narrow|wide|hide)$/.test(x[3] ?? ""));
    if (this.view === "snooze") {
      return [
        ...L.snoozeOptions().map((o, n) => ["clock", o.label, String(n + 1), `until:${o.until}`, false, L.fmtTime(o.until)]),
        ["pencil", "Other time…", "c", "when"],
      ];
    }
    return L.threadMenu(th, { pr: props()?.pr ?? null, pinnedCount: L.threads().pinned.length });
  }

  choose(item, cx) {
    if (!item || item[0] === "sep") return;
    const action = item[3];
    const th = this.thread();
    if (action === "snooze") { this.view = "snooze"; this.idx = 0; return cx.notify(); }
    if (action === "more") { this.view = "more"; this.idx = 0; return cx.notify(); }
    if (action === "reply" || action === "when") {
      this.view = action === "reply" ? "reply" : "when";
      this.error = "";
      this.inputs[this.view].set_value("");
      this.inputs[this.view].focus();
      return cx.notify();
    }
    if (action.startsWith("until:")) { L.snooze(th, Number(action.slice(6))); return this.close(); }
    this.close();
    if (action === "approve" || action === "deny" || action === "markSeen") cx.spawn(async () => L.runAction(action, th));
    else L.runAction(action, th);
  }

  submit(cx) {
    const th = this.thread();
    const text = this.inputs[this.view]?.value() ?? "";
    if (this.view === "reply") {
      this.close();
      if (th) cx.spawn(async () => L.reply(th, text));
      return;
    }
    if (this.view === "when") {
      const until = L.parseWhen(text);
      if (!until) { this.error = "try 90m, 3h, 15:04, evening, tomorrow or next-week"; return cx.notify(); }
      L.snooze(th, until);
      this.close();
    }
  }

  key(e, cx) {
    const k = e.key;
    const ch = e.key_char ?? "";
    if (this.view === "reply" || this.view === "when") {
      if (k === "escape") { this.view = this.view === "when" ? "snooze" : "main"; this.handle.focus(); cx.stop_propagation(); cx.notify(); }
      return;
    }
    if (e.modifiers.platform || e.modifiers.control) return;
    const items = this.items();
    const real = items.map((x, i) => (x[0] === "sep" ? -1 : i)).filter((i) => i >= 0);
    const at = Math.max(0, real.indexOf(this.idx));
    if (k === "escape") {
      if (this.view === "main" || !this.thread() || props()?.view === this.view) this.close();
      else { this.view = "main"; this.idx = 0; }
    }
    else if (k === "down" || ch === "j") this.idx = real[(at + 1) % real.length];
    else if (k === "up" || ch === "k") this.idx = real[(at + real.length - 1) % real.length];
    else if (k === "enter" || k === "right" || k === "space") return this.choose(items[this.idx] ?? items[real[0]], cx);
    else if (k === "left" && this.view !== "main") { this.view = "main"; this.idx = 0; }
    else {
      const byKey = items.find((x) => x[0] !== "sep" && x[2] && (x[2] === ch || (x[2] === "›" && false)));
      if (byKey) return this.choose(byKey, cx);
      // Snooze and Sidebar open with their own letters too.
      if (ch === "z" && this.view === "main") { const s = items.find((x) => x[3] === "snooze"); if (s) return this.choose(s, cx); }
      if (ch === "m" && this.view === "main") { const s = items.find((x) => x[3] === "more"); if (s) return this.choose(s, cx); }
      return;
    }
    cx.stop_propagation();
    cx.notify();
  }

  render() {
    const t = theme();
    const th = this.thread();
    const items = this.items();
    if (items[this.idx]?.[0] === "sep" || !items[this.idx]) this.idx = items.findIndex((x) => x[0] !== "sep");
    const title = this.view === "snooze" ? "Snooze until" : this.view === "more" || !th ? "Sidebar" : this.view === "when" ? "Snooze until" : this.view === "reply" ? `Reply to ${th?.title ?? ""}` : th.title;

    const body = this.view === "reply" || this.view === "when"
      ? v_flex().px(10).py(8).gap(6)
        .child(h_flex().h(28).px(8).gap(6).items_center().rounded(4).border_1().border_color(t.accent).bg(t.background)
          .child(div().text_color(t.accent).font_bold().child("›"))
          .child(Input.new(this.inputs[this.view]).flex_1().text_size(13).text_color(t.foreground)))
        .when(!!this.error, (el) => el.child(div().text_color(t.blocked).text_size(12).child(this.error)))
        .child(div().text_color(t.dim).text_size(11).child("↵ send · esc back"))
      : v_flex().py(4).children(items.map((x, i) => {
        if (x[0] === "sep") return div().id(`sep-${i}`).h(1).my(4).mx(8).bg(t.border);
        const [icon, label, key, , danger, hint] = x;
        const on = i === this.idx;
        return h_flex().id(`mi-${i}`).h(26).px(10).gap(8).items_center().cursor_pointer()
          .when(on, (el) => el.bg(t.selected))
          .hover((s) => s.bg(t.hover))
          .on_mouse_move((_e, cx) => { if (this.idx !== i) { this.idx = i; cx.notify(); } })
          .on_click((_e, cx) => this.choose(x, cx))
          .child(svg(L.ICON(icon)).size(13).flex_none().text_color(danger ? t.blocked : on ? t.accent : t.muted))
          .child(div().flex_1().truncate().text_color(danger ? t.blocked : t.foreground).child(label))
          .when(!!hint, (el) => el.child(div().flex_none().text_color(t.dim).child(hint)))
          .child(div().w(14).flex_none().text_color(t.accent).font_bold().child(key ?? ""));
      }));

    return v_flex()
      .id("sesh-menu")
      .track_focus(this.handle)
      .on_key_down((e, cx) => this.key(e, cx))
      .w_full()
      .bg(t.popover)
      .text_size(13)
      .text_color(t.foreground)
      .child(h_flex().px(10).pt(8).pb(4).gap(6).items_center()
        .child(div().flex_1().truncate().font_bold().text_color(t.accent).child(title))
        .when(!!th && this.view === "main", (el) => el.child(L.glyph(t, th.closed ? "idle" : th.status))))
      .child(div().h(1).bg(t.border))
      .child(body);
  }
}

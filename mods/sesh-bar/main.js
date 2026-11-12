// The right end of the herdr-sesh tab bar: the attention strip (the other
// spaces with an agent waiting on you, or finished), and the receiver for the
// layout's global keys (cmd-j, cmd-shift-p, cmd-ctrl-…), which keep working
// with the sidebar hidden. It's also the one place that tracks thread state.

import { View, div, svg } from "gpui-kit";
import { h_flex } from "gpui-base";
import { focused, run, theme, nextCommand, spaces, agents, panes, layout, home, readTextFile, appHome, exec, notify, shared, setShared } from "herdr";
import * as L from "./lib.js";

function find(node, id) {
  if (node?.id === id) return node;
  for (const c of node?.children ?? []) { const f = find(c, id); if (f) return f; }
  return null;
}

export default class Bar extends View {
  init(_props, cx) {
    this.needAt = -1;
    this.tick(cx);
    cx.timer.every(1000, (cx) => this.tick(cx));
    // The strip's waiting times ("12m") move on: redraw now and then, not every second.
    cx.timer.every(30000, (cx) => cx.notify());
    // Which conversation each Claude agent runs, so a closed one can be resumed; and
    // .worktreeinclude for new worktrees. One round at a time: a slow one (a big copy, a
    // far machine) mustn't have the next ones pile up behind it.
    this.busy = false;
    const round = async (worktrees) => {
      if (this.busy) return;
      this.busy = true;
      try {
        await this.readConfig();
        await L.captureSessions(home(), readTextFile).catch(() => {});
        if (worktrees) await this.worktrees();
      } finally { this.busy = false; }
    };
    cx.spawn(async () => round(false));
    cx.timer.every(10000, (cx) => cx.spawn(async () => round(true)));
    // Folders looked at for .worktreeinclude (the script itself only acts on fresh
    // worktrees), and how often a folder failed (it's tried again, up to 3 times).
    this.looked = new Set();
    this.failed = {};
    this.tops = new Set();
    cx.spawn(async (cx) => {
      for (;;) {
        const c = await nextCommand();
        this.command(c, cx);
        cx.notify();
      }
    });
  }

  // Status stamps, snooze expiry, and which space you were in before this one.
  tick() {
    this.fit();
    L.track();
    const here = focused()?.space;
    const s = L.state();
    if (here && here !== s.cur) L.patch((x) => { x.prev = x.cur && spaces().some((p) => p.id === x.cur) ? x.cur : x.prev; x.cur = here; return x; });
  }

  thread() { return L.currentThread(); }

  // herdr-sesh's config, for every view (shared "sesh.config"): read every 10 s.
  async readConfig() {
    const text = await readTextFile(`${home()}/${L.CONFIG_PATH}`).catch(() => "");
    const cfg = L.parseConfig(text);
    if (JSON.stringify(cfg) !== JSON.stringify(shared("sesh.config") ?? {})) setShared("sesh.config", cfg);
  }

  // Removes a merged thread's worktree and deletes its branch: its space closes first (its
  // agent stops), then git runs from the repo's own checkout, on the thread's machine.
  async cleanup(address) {
    const th = L.threads().all.find((t) => t.address === address && !t.closed);
    if (!th?.cwd) return;
    const { cwd, machine, space, title } = th;
    // Unsaved work stops it before anything closes.
    const st = await exec("git", ["status", "--porcelain"], { cwd, on: machine, timeout: 8000 }).catch(() => null);
    if (st?.code !== 0 || st.stdout.trim()) {
      notify({ title: `Kept "${title}"`, body: st?.code === 0 ? "Its worktree has uncommitted changes." : "Couldn't read its worktree.", level: "warning" });
      return;
    }
    if (space && spaces().some((p) => p.id === space)) run("space.close", { id: space, confirmed: true });
    const r = await exec("sh", ["-c", `sleep 1; ${L.CLEANUP}`, "sh", cwd], { on: machine, timeout: 120000 }).catch((e) => ({ code: 1, stderr: String(e) }));
    if (r.code === 0) notify({ title: `Cleaned up ${r.stdout.trim() || "the branch"}`, body: "Worktree removed, branch deleted.", level: "success" });
    else notify({ title: `Couldn't clean up "${title}"`, body: (r.stderr || r.stdout || "git failed").trim().split("\n").pop(), level: "error" });
  }

  // Copies what .worktreeinclude lists into a new worktree: Herdr's worktree dialog
  // doesn't, so a terminal showing up in a fresh worktree gets it done here.
  async worktrees() {
    this.script ??= await readTextFile(`${appHome()}/mods/sesh-bar/worktree-include.sh`).catch(() => null);
    if (!this.script) return;
    const machineOf = new Map();
    for (const sp of spaces()) for (const tb of sp.tabs) machineOf.set(tb.id, sp.machine);
    for (const p of panes()) {
      if (!p.cwd || !p.branch) continue;
      const on = machineOf.get(p.tab) ?? null;
      const key = `${on ?? ""}:${p.cwd}`;
      if (this.looked.has(key)) continue;
      this.looked.add(key);
      // Unreachable, timed out or failed: look again next round, up to 3 times.
      const retry = () => { if ((this.failed[key] = (this.failed[key] ?? 0) + 1) < 3) this.looked.delete(key); };
      try {
        const r = await exec("sh", ["-c", this.script], { cwd: p.cwd, on, timeout: 120000 });
        if (r.code !== 0) { retry(); continue; }
        const lines = r.stdout.split("\n").filter(Boolean);
        const top = lines.find((l) => l.startsWith("@"))?.slice(1);
        if (!top || this.tops.has(`${on ?? ""}:${top}`)) continue;
        this.tops.add(`${on ?? ""}:${top}`);
        const copied = lines.filter((l) => l.startsWith("copied ")).map((l) => l.slice(7));
        if (copied.length) notify({ title: "Copied into the new worktree", body: copied.join(", "), level: "info" });
      } catch { retry(); }
    }
  }

  // Agents that need you, the one waiting longest first. Not a snoozed one: snoozing is
  // "not now" (one that starts waiting on you wakes up by itself).
  needing() {
    const m = L.threads();
    const rank = { blocked: 0, done: 1 };
    return m.all.filter((t) => !t.closed && !t.snoozed && t.status in rank).sort((a, b) => rank[a.status] - rank[b.status] || a.at - b.at);
  }

  command(c, cx) {
    const [name, arg] = c.split(":");
    const th = this.thread();
    if (name === "last") {
      const prev = L.state().prev;
      if (prev && spaces().some((p) => p.id === prev)) run("space.select", { id: prev });
    } else if (name === "agent-last") {
      // Back to the agent you were with before this one.
      const here = focused()?.terminal;
      const live = new Set(agents().map((a) => a.address));
      const prev = L.state().history.find((x) => x !== here && live.has(x));
      if (prev) run("agent.focus", { terminal: prev });
    } else if (name === "pane-next" || name === "pane-prev") {
      // Next / previous pane: through the tab's panes in order.
      const f = focused();
      const list = panes().filter((p) => p.tab === f?.tab);
      if (list.length < 2) return;
      const i = list.findIndex((p) => p.pane === f?.pane);
      // Not on one of them (a sidebar has the keyboard): start at the first, or the last.
      const j = i < 0 ? (name === "pane-next" ? 0 : list.length - 1) : (i + (name === "pane-next" ? 1 : -1) + list.length) % list.length;
      run("pane.focus", { pane: list[j].pane });
    } else if (name === "need-next" || name === "need-prev") {
      const list = this.needing();
      if (!list.length) return;
      const cur = list.findIndex((t) => t.address === focused()?.terminal);
      const i = cur < 0 ? (name === "need-next" ? 0 : list.length - 1) : (cur + (name === "need-next" ? 1 : -1) + list.length) % list.length;
      L.go(list[i]);
    } else if (name === "pin" && th) L.togglePin(th);
    else if (name === "settle" && th) L.toggleSettle(th);
    else if (name === "snooze" && th) L.snooze(th, Date.now() + 3600e3);
    else if (name === "wake" && th) L.wake(th);
    else if (name === "thread-next" || name === "thread-prev") {
      const order = L.shownOrder();
      if (!order.length) return;
      const cur = order.findIndex((t) => t.address === th?.address);
      const i = cur < 0 ? (name === "thread-next" ? 0 : order.length - 1) : (cur + (name === "thread-next" ? 1 : -1) + order.length) % order.length;
      L.go(order[i]);
    } else if (name === "thread") {
      const t = L.shownOrder()[Number(arg) - 1];
      if (t) L.go(t);
    } else if (name === "toggle-sidebar") {
      // Hiding it hands the keyboard back first (a hidden sidebar would keep it).
      if (find(layout(), "sidebar")?.hidden) run("layout.show", { id: "sidebar" });
      else run("sesh-sidebar.hide");
    } else if (name === "cleanup" && arg) {
      cx?.spawn(async () => this.cleanup(arg));
    } else if (name === "ask-cleanup" && arg) {
      // From the "PR merged" notification: the actions menu asks before anything goes.
      const t = L.threads().all.find((x) => x.address === arg && !x.closed);
      if (t) L.openMenu(t, { from: "notification", view: "cleanup", at: "pointer" });
    } else if (name === "view" && arg) {
      // A sidebar command from the actions menu, sent once the just-shown sidebar is listening.
      cx?.timer.after(150, () => run(`sesh-sidebar.${arg}`));
    } else if (name === "sidebar") {
      // A hidden sidebar hears commands only once it's shown: give it a moment.
      if (find(layout(), "sidebar")?.hidden) { run("layout.show", { id: "sidebar" }); cx?.timer.after(150, () => run("sesh-sidebar.enter")); }
      else run("sesh-sidebar.toggle-focus");
    }
  }

  // The other spaces with an agent waiting on you (first) or finished.
  attention() {
    const here = focused()?.space;
    const others = spaces().filter((p) => p.id !== here && (p.status === "blocked" || p.status === "done"));
    return others.sort((a, b) => (b.status === "blocked") - (a.status === "blocked") || this.since(a) - this.since(b));
  }
  // Since when a space's agent in its status has been so (the longest waiting one).
  since(p) {
    const ch = L.state().changed;
    const ats = agents().filter((a) => a.space === p.id && a.status === p.status).map((a) => ch[a.address]?.at).filter(Boolean);
    return ats.length ? Math.min(...ats) : Date.now();
  }

  // The strip's block is as wide as what it shows, so the tab bar's own buttons
  // (+, Files, Changes) stay at the window's right edge when nothing needs you.
  widthFor(others) {
    const shown = others.slice(0, 4);
    return shown.length
      ? Math.min(520, Math.ceil(20 + shown.reduce((w, p) => w + 15 + Math.min(110, [...p.name].length * 7.3) + 4 + this.ageText(p).length * 7.3, 0) + 10 * (shown.length - 1) + (others.length > 4 ? 34 : 0)))
      : 12;
  }
  ageText(p) { return L.age(Date.now() - this.since(p)); }
  // Asked for in the last 5 s: the resize is on its way (don't ask again every redraw).
  asked(px) { return this.askedPx === px && Date.now() - (this.askedAt ?? 0) < 5000; }
  fits(others) {
    const node = find(layout(), "attention");
    const px = this.widthFor(others);
    return !node || node.size?.px === px || this.asked(px);
  }
  fit() {
    if (!find(layout(), "attention")) return;
    const px = this.widthFor(this.attention());
    // Against the block's real width (a rewritten layout file resets it), not the last asked.
    if (px !== find(layout(), "attention")?.size?.px && !this.asked(px)) {
      this.askedPx = px;
      this.askedAt = Date.now();
      run("layout.resize", { id: "attention", px });
    }
  }

  render(cx) {
    const t = theme();
    const others = this.attention();
    // What changed is drawn now; the block's width and the focus history follow right
    // after (not on the next 1 s tick: chips were clipped, short visits went unrecorded).
    const f = focused();
    const s = L.state();
    const agentHere = f?.terminal && agents().some((a) => a.address === f.terminal);
    if ((f?.space && f.space !== s.cur) || (agentHere && s.history[0] !== f.terminal) || !this.fits(others)) cx.timer.after(0, () => this.tick());
    return h_flex()
      .id("sesh-bar")
      .size_full()
      .bg(t.chrome)
      .border_b(1)
      .border_color(t.border)
      .px(10)
      .gap(10)
      .items_center()
      .justify_end()
      .text_size(12)
      .overflow_hidden()
      .children(others.slice(0, 4).map((p) => {
        const c = p.status === "blocked" ? t.blocked : t.done;
        return h_flex().id(`att-${p.id}`).gap(4).items_center().cursor_pointer().text_color(c)
          .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
          .on_click(() => {
            const a = agents().find((x) => x.space === p.id && x.status === p.status);
            if (a) run("agent.focus", { terminal: a.address }); else run("space.select", { id: p.id });
          })
          .hover((s) => s.text_color(t.foreground))
          .child(svg(L.ICON(p.status === "blocked" ? "bell" : "check")).size(11).text_color(c))
          .child(div().max_w(110).truncate().child(p.name))
          .child(div().text_color(t.dim).child(this.ageText(p)));
      }))
      .when(others.length > 4, (el) => el.child(div().text_color(t.dim).child(`+${others.length - 4}`)));
  }
}

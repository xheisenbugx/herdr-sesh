// Shared thread model for the herdr-sesh layout (the thread sidebar, pickers and menu).
// The source of truth is mods/sesh-sidebar/lib.js; sesh-agents, sesh-sessions,
// sesh-bar and sesh-menu carry copies (mods can't import across folders).
// Keep them equal: mods/sesh-sidebar/sync-lib.sh copies it.
//
// State lives in shared("sesh") so every view agrees on sections and order:
//   changed:  { address: { s, at } }   last status and when it changed
//   pins:     [address]                 pin order (Herdr's own pin flag decides membership)
//   settled:  { address: at }           settled by hand (or its PR merged)
//   merged:   { address: true }         auto-settled once for a merged PR (a restore sticks)
//   snoozed:  { address: until }        hidden until, or until it needs you
//   prev / cur: space ids               for "back to the previous session"
//   history:  [address]                 agents you were with, latest first (cmd-shift-j)
//   sessions: { address: { id, app, cwd, title, space, spaceName } }  the conversation each agent runs
//   missing:  { address: since }        live agents that vanished, until they count as closed
//   closed:   { id: { id, app, cwd, title, space, spaceName, at } }   threads whose terminal is gone
//   undo:     { settled, snoozed, pins } one step back

import { svg, div } from "gpui-kit";
import { agents, panes, spaces, terminals, focused, shared, setShared, run, hc, exec, openOverlay } from "herdr";

export const ICON = (name) => `mods/sesh-sidebar/${name}.svg`;
export const WEEK = 7 * 86400000;
const MAIN = /^(main|master|trunk|develop)$/;

export function state() {
  const s = shared("sesh") ?? {};
  return {
    changed: {}, pins: [], settled: {}, merged: {}, snoozed: {}, prev: null, cur: null, history: [],
    sessions: {}, missing: {}, closed: {}, undo: null, ...s,
  };
}
export function patch(fn, { undo = false } = {}) {
  const s = state();
  const before = { settled: { ...s.settled }, snoozed: { ...s.snoozed }, pins: [...s.pins], closed: { ...s.closed } };
  const next = fn({
    ...s, settled: { ...s.settled }, snoozed: { ...s.snoozed }, pins: [...s.pins], changed: { ...s.changed },
    merged: { ...s.merged }, history: [...s.history], sessions: { ...s.sessions }, missing: { ...s.missing }, closed: { ...s.closed },
  }) ?? s;
  if (undo) next.undo = before;
  setShared("sesh", next);
}
export function undo() {
  const s = state();
  if (!s.undo) return false;
  const u = s.undo;
  // Pins are also Herdr's terminal label: put those back too.
  const was = new Set(u.pins);
  for (const a of agents()) if (a.pinned !== was.has(a.address)) run("terminal.pin", { terminal: a.address, pinned: was.has(a.address) });
  setShared("sesh", { ...s, ...u, closed: u.closed ?? s.closed, undo: null });
  return true;
}

// Stamps status changes and applies the thread rules: an agent that starts
// waiting wakes from a snooze; one that starts working again (you sent it a
// prompt) leaves Settled. A live agent that's gone for a while becomes a
// closed thread you can resume. Call it from one place (sesh-bar) so writes don't race.
export function track(now = Date.now()) {
  const s = state();
  let dirty = false;
  const changed = { ...s.changed };
  const settled = { ...s.settled };
  const snoozed = { ...s.snoozed };
  const merged = { ...s.merged };
  const missing = { ...s.missing };
  const sessions = { ...s.sessions };
  const closed = { ...s.closed };
  let history = [...s.history];
  const live = new Set();
  const list = agents();
  for (const a of list) {
    live.add(a.address);
    if (missing[a.address]) { delete missing[a.address]; dirty = true; }
    const st = a.status === "done" ? "idle" : a.status;
    const prev = changed[a.address];
    if (prev?.s !== st) {
      changed[a.address] = { s: st, at: now };
      dirty = true;
      if (st === "blocked" && snoozed[a.address]) delete snoozed[a.address];
      if ((st === "working" || st === "blocked") && settled[a.address] && prev) delete settled[a.address];
    }
    if (snoozed[a.address] && (snoozed[a.address] <= now || a.status === "blocked")) { delete snoozed[a.address]; dirty = true; }
  }
  // The agent you're with heads the history (cmd-shift-j goes back to the one before).
  const here = focused()?.terminal;
  if (here && live.has(here) && history[0] !== here) {
    history = [here, ...history.filter((x) => x !== here)].slice(0, 20);
    dirty = true;
  }
  // An agent whose terminal went (or whose program quit) is a closed thread after 15 s
  // (the list is empty for a moment while Herdr starts, so wait for agents to show first).
  if (list.length || now - (s.startedAt ?? now) > 30000) {
    for (const [addr, sess] of Object.entries(sessions)) {
      if (live.has(addr)) continue;
      if (!missing[addr]) { missing[addr] = now; dirty = true; continue; }
      if (now - missing[addr] < 15000) continue;
      if (sess?.id) closed[sess.id] = { ...sess, at: changed[addr]?.at ?? now };
      delete sessions[addr]; delete missing[addr];
      dirty = true;
    }
  }
  // Forget what's gone (after a grace period: the list is empty right after a start).
  for (const k of Object.keys(changed)) {
    if (!live.has(k) && !sessions[k] && now - changed[k].at > 3 * 86400000) {
      delete changed[k]; delete settled[k]; delete snoozed[k]; delete merged[k]; dirty = true;
    }
  }
  history = history.filter((x) => live.has(x));
  if (history.length !== s.history.length) dirty = true;
  // Closed threads go after two weeks.
  for (const [id, c] of Object.entries(closed)) if (now - (c.at ?? 0) > 2 * WEEK) { delete closed[id]; dirty = true; }
  const pins = s.pins.filter((p) => live.has(p) || changed[p]);
  if (pins.length !== s.pins.length) dirty = true;
  if (dirty) setShared("sesh", { ...s, changed, settled, snoozed, merged, missing, sessions, closed, history, pins, startedAt: s.startedAt ?? now });
}

// Which conversation each Claude Code agent runs: its process writes
// ~/.claude/sessions/<pid>.json with the session id (what `claude --resume` takes).
export async function captureSessions(homeDir, readTextFile) {
  const s = state();
  const found = {};
  for (const a of agents()) {
    if (a.app !== "claude-code") continue;
    try {
      const r = await hc(["get", a.address, "--json"]);
      if (r.code !== 0) continue;
      const pid = JSON.parse(r.stdout)?.foreground?.pid;
      if (!pid) continue;
      const text = await readTextFile(`${homeDir}/.claude/sessions/${pid}.json`);
      const id = text ? JSON.parse(text)?.sessionId : null;
      if (!id) continue;
      const p = panes().find((x) => x.pane === a.pane);
      found[a.address] = { id, app: a.app, cwd: p?.cwd ?? JSON.parse(text)?.cwd ?? null, title: titleOf(a), space: a.space, spaceName: a.space_name };
    } catch {}
  }
  const same = Object.keys(found).every((k) => {
    const o = s.sessions[k];
    return o && o.id === found[k].id && o.title === found[k].title && o.cwd === found[k].cwd && o.spaceName === found[k].spaceName;
  });
  const live = new Set(Object.values(found).map((x) => x.id));
  const reopened = Object.keys(s.closed).some((id) => live.has(id));
  if (same && !reopened) return;
  patch((x) => {
    for (const [addr, v] of Object.entries(found)) x.sessions[addr] = v;
    for (const id of live) delete x.closed[id];
    return x;
  });
}

// A terminal's text from `hc read`, without the note hc adds about it ("[hc] only its screen: …").
export const screenLines = (text) => (text ?? "").replace(/\s+$/, "").split("\n").filter((l) => !/^\[hc\] /.test(l));

export const clean = (x) => (x ?? "").replace(/^[^\p{L}\p{N}]+/u, "").trim();

// The thread's title: what the agent set as its terminal title, else the tab's name.
export function titleOf(a) {
  const live = clean(a.title);
  const prog = a.program?.name;
  const path = /^[~/]/.test((a.title ?? "").replace(/^[^\p{L}\p{N}~/]+/u, ""));
  if (live && live !== prog && !path && live.toLowerCase() !== a.program?.key && !/^claude code$/i.test(live)) return live;
  return a.tab_name && a.tab_name !== String(a.tab_index) ? a.tab_name : prog ?? "agent";
}

export function age(ms) {
  if (!ms) return "";
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  return h < 24 ? `${h}h` : `${Math.floor(h / 24)}d`;
}

// Every thread, in the sidebar's sections. `filter` matches title, space or branch
// fzf-style (letters in order); `space` keeps one space's threads.
export function threads({ filter = "", space = null, now = Date.now() } = {}) {
  const s = state();
  const paneOf = new Map(panes().map((p) => [p.pane, p]));
  const fz = fuzzy(filter);
  const liveIds = new Set(Object.values(s.sessions).map((x) => x?.id));
  const closed = Object.values(s.closed)
    .filter((c) => c?.id && !liveIds.has(c.id))
    .map((c) => ({
      a: null, closed: true, id: c.id, app: c.app, address: `closed:${c.id}`, title: c.title || "thread",
      space: c.space, spaceName: c.spaceName ?? "", branch: null, cwd: c.cwd ?? null, status: "idle",
      unread: false, at: c.at ?? 0, pinned: false, settled: true, snoozed: null,
    }));
  const all = agents()
    .map((a) => {
      const p = paneOf.get(a.pane);
      const ch = s.changed[a.address];
      const at = ch?.at ?? 0;
      return {
        a,
        closed: false,
        address: a.address,
        title: titleOf(a),
        space: a.space,
        spaceName: a.space_name,
        branch: p?.branch ?? null,
        cwd: p?.cwd ?? null,
        status: a.status,
        unread: a.status === "done",
        at,
        pinned: a.pinned,
        settled: !!s.settled[a.address] || (a.status === "idle" && at && now - at > WEEK),
        snoozed: s.snoozed[a.address] && s.snoozed[a.address] > now ? s.snoozed[a.address] : null,
      };
    })
    .concat(closed)
    .filter((t) => !space || t.space === space)
    .filter((t) => !filter || fz(`${t.title} ${t.spaceName} ${t.branch ?? ""}`));
  const pinOrder = (t) => { const i = s.pins.indexOf(t.address); return i < 0 ? 1e9 : i; };
  const byRecent = (x, y) => y.at - x.at;
  const pinned = all.filter((t) => t.pinned).sort((x, y) => pinOrder(x) - pinOrder(y));
  const rest = all.filter((t) => !t.pinned);
  const snoozed = rest.filter((t) => t.snoozed && t.status !== "blocked").sort((x, y) => x.snoozed - y.snoozed);
  const settled = rest.filter((t) => !snoozed.includes(t) && t.settled && t.status !== "blocked").sort(byRecent);
  const working = rest.filter((t) => !snoozed.includes(t) && !settled.includes(t) && t.status === "working").sort(byRecent);
  const inbox = rest.filter((t) => !snoozed.includes(t) && !settled.includes(t) && t.status !== "working").sort(byRecent);
  const counts = { blocked: 0, done: 0, working: 0 };
  for (const t of all) if (!t.closed && counts[t.status] !== undefined) counts[t.status]++;
  return { pinned, inbox, working, snoozed, settled, all, counts, order: [...pinned, ...inbox, ...working] };
}

export function fuzzy(q) {
  const needle = q.toLowerCase().replace(/\s+/g, "");
  return (hay) => {
    hay = hay.toLowerCase();
    let i = 0;
    for (const c of hay) if (c === needle[i]) i++;
    return i >= needle.length;
  };
}

// The thread you're in: the focused terminal's, else the focused pane's, else the tab's.
export function currentThread() {
  const f = focused();
  const m = threads();
  return m.all.find((t) => !t.closed && t.address === f?.terminal)
    ?? m.all.find((t) => !t.closed && t.a.pane === f?.pane)
    ?? m.all.find((t) => !t.closed && t.a.tab === f?.tab) ?? null;
}

// ── Acting on a thread ──────────────────────────────────────────────

export function go(t) {
  if (!t) return;
  if (t.closed) return resume(t);
  run("agent.focus", { terminal: t.address });
}
// A closed thread comes back in its space (a new one when that's gone), in its folder.
export function resume(t) {
  const argv = t.app === "codex" ? ["codex", "resume", t.id] : ["claude", "--resume", t.id];
  const folder = t.cwd ?? undefined;
  if (t.space && spaces().some((p) => p.id === t.space)) run("tab.new", { space: t.space, ...(folder ? { folder } : {}), argv });
  else run("space.new", { ...(folder ? { folder } : {}), argv });
  patch((s) => { delete s.closed[t.id]; return s; });
}
export function forget(t) {
  if (!t?.closed) return;
  patch((s) => { delete s.closed[t.id]; return s; }, { undo: true });
}
// A finished agent you've read without going to it: Herdr counts it seen once its
// herdr.seen label reaches its turn (ctrl-s in the agent picker).
export async function markSeen(t) {
  if (!t || t.closed || t.status !== "done") return;
  const turn = terminals().find((x) => x.address === t.address)?.turn;
  if (turn != null) await hc(["label", t.address, `herdr.seen=${turn}`]);
}
export async function approve(t) {
  if (t?.status === "blocked") await hc(["keys", t.address, "enter"]);
}
export async function deny(t) {
  if (!t || t.closed) return;
  if (t.status === "blocked") await hc(["keys", t.address, "esc"]);
  else if (t.status === "working") await hc(["interrupt", t.address]);
}
// One process for both steps, started at once: it keeps going even when the
// view that asked (a popup) closes right after.
export async function reply(t, text) {
  if (!t || t.closed || !text.trim()) return;
  const script = 'if [ "$3" = blocked ]; then hc keys "$1" esc; sleep 0.4; fi; exec hc send --force "$1" "$2"';
  await exec("sh", ["-c", script, "sh", t.address, text, t.status], { timeout: 120000 }).catch(() => {});
}
export function togglePin(t) {
  if (!t || t.closed) return;
  patch((s) => {
    if (t.pinned) s.pins = s.pins.filter((p) => p !== t.address);
    else s.pins = [...s.pins.filter((p) => p !== t.address), t.address];
    delete s.snoozed[t.address];
    delete s.settled[t.address];
    return s;
  }, { undo: true });
  run("terminal.pin", { terminal: t.address, pinned: !t.pinned });
}
export function movePin(t, delta) {
  patch((s) => {
    const order = threads().pinned.map((x) => x.address);
    const i = order.indexOf(t.address);
    const j = i + delta;
    if (i < 0 || j < 0 || j >= order.length) return s;
    [order[i], order[j]] = [order[j], order[i]];
    s.pins = order;
    return s;
  });
}
export function toggleSettle(t) {
  if (!t || t.closed) return;
  patch((s) => {
    if (s.settled[t.address] || t.settled) { delete s.settled[t.address]; s.changed[t.address] = { s: t.status === "done" ? "idle" : t.status, at: Date.now() }; }
    else { s.settled[t.address] = Date.now(); delete s.snoozed[t.address]; }
    return s;
  }, { undo: true });
}
export function snooze(t, until) {
  if (!t || t.closed) return;
  patch((s) => { s.snoozed[t.address] = until; delete s.settled[t.address]; return s; }, { undo: true });
}
export function wake(t) {
  if (!t || t.closed) return;
  patch((s) => { delete s.snoozed[t.address]; delete s.settled[t.address]; s.changed[t.address] = { s: t.status === "done" ? "idle" : t.status, at: Date.now() }; return s; }, { undo: true });
}
export function closeThread(t) {
  if (!t) return;
  if (t.closed) return forget(t);
  run("pane.close", { pane: t.a.pane });
}
export function review(t) {
  if (!t?.cwd) return;
  // Everything since the branch left its base, committed or not, as its pull request will show it.
  const script = 'b=$(git symbolic-ref -q --short refs/remotes/origin/HEAD 2>/dev/null || echo main); ' +
    'm=$(git merge-base HEAD "$b" 2>/dev/null || echo HEAD); git --no-pager log --oneline "$m"..HEAD; echo; ' +
    'git --no-pager diff --stat "$m"; echo; git diff "$m"; exec "${SHELL:-/bin/zsh}" -l';
  run("terminal.open", { argv: ["sh", "-c", script], folder: t.cwd, place: "right" });
}
export function openPR(t) {
  if (!t?.cwd) return;
  exec("gh", ["pr", "view", "--web"], { cwd: t.cwd }).catch(() => {});
}
export function newThread(t) {
  const space = t?.space && spaces().some((p) => p.id === t.space) ? t.space : focused()?.space;
  run("tab.new", { ...(space ? { space } : {}), ...(t?.cwd ? { folder: t.cwd } : {}), argv: ["claude"] });
}
export function newWorktreeThread(t) {
  const space = t?.space ?? focused()?.space;
  openOverlay({ mod: "worktree", kind: "dialog", props: { space, ...(t?.cwd ? { folder: t.cwd } : {}), action: "new" } });
}
export function archive(t) {
  if (!t || t.closed || !t.cwd) return;
  openOverlay({ mod: "worktree", kind: "dialog", props: { space: t.space, folder: t.cwd, action: "remove" } });
}
export function rename(t) {
  if (!t || t.closed) return;
  run("tab.rename", { id: t.a.tab });
}

// The thread menu, item by item: [icon, label, key, action, danger].
// An action is a function name in this file or a view action ("reply", "snooze", "more").
export function threadMenu(t, { pr = null, pinnedCount = 0 } = {}) {
  if (!t) return viewMenu();
  if (t.closed) return [
    ["play", "Resume", "↵", "go"],
    ["sep"],
    ["trash", "Forget…", "X", "closeThread", true],
  ];
  const section = t.snoozed ? "snoozed" : t.settled ? "settled" : null;
  const items = [["arrow", "Go to thread", "↵", "go"]];
  if (!section) {
    items.push(["pin", t.pinned ? "Unpin" : "Pin", "p", "togglePin"]);
    if (t.pinned && pinnedCount > 1) items.push(["up", "Move up", "K", "pinUp"], ["down", "Move down", "J", "pinDown"]);
  }
  if (section === "snoozed") items.push(["sun", "Wake now", "w", "wake"]);
  else if (section !== "settled") items.push(["clock", "Snooze", "›", "snooze"]);
  if (section === "settled") items.push(["undo", "Restore", "s", "toggleSettle"]);
  else if (t.status !== "working" && t.status !== "blocked") items.push(["box", "Settle", "s", "toggleSettle"]);
  items.push(["sep"]);
  if (t.status === "blocked") items.push(["check", "Approve", "a", "approve"], ["x", "Deny", "x", "deny"]);
  else if (t.status === "working") items.push(["stop", "Interrupt", "x", "deny"]);
  if (t.status === "done") items.push(["check", "Mark seen", "e", "markSeen"]);
  items.push(["reply", "Reply…", "r", "reply"], ["eye", "Review changes", "d", "review"]);
  if (pr) items.push(["pr", `Open PR #${pr.number}`, "P", "openPR"]);
  items.push(["pencil", "Rename…", "R", "rename"], ["sep"]);
  items.push(["plus", "New thread…", "n", "newThread"]);
  if (t.cwd) items.push(["branch", "New in a worktree…", "N", "newWorktreeThread"]);
  items.push(["more", "Sidebar", "›", "more"], ["sep"]);
  items.push(["x", "Close pane…", "X", "closeThread", true]);
  if (t.branch && !MAIN.test(t.branch)) items.push(["box", "Archive worktree…", "A", "archive", true]);
  return items;
}
// What isn't about one thread: the sidebar's own options.
export function viewMenu({ group = false, only = false } = {}) {
  return [
    ["plus", "New thread…", "n", "newThread"],
    ["undo", "Undo", "u", "undo"],
    ["sep"],
    ["filter", "Filter…", "/", "view:filter"],
    ["grid", group ? "Ungroup" : "Group by session", "o", "view:group"],
    ["target", only ? "All sessions" : "This session only", "f", "view:only"],
    ["width", "Narrower", "<", "view:narrow"],
    ["width", "Wider", ">", "view:wide"],
    ["sep"],
    ["keys", "Keys", "?", "view:help"],
    ["eyeoff", "Hide sidebar", "q", "view:hide", true],
  ];
}
// Runs a menu item's action on a thread. View actions go to the sidebar.
export function runAction(name, t) {
  const plain = { markSeen, go, togglePin, toggleSettle, wake, approve, deny, review, openPR, rename, newThread, newWorktreeThread, closeThread, archive, forget };
  if (name === "pinUp") return movePin(t, -1);
  if (name === "pinDown") return movePin(t, 1);
  if (name === "undo") return undo();
  if (name.startsWith("view:")) return run(`sesh-sidebar.${name.slice(5)}`);
  const f = plain[name];
  if (f) return f(t);
}
// Opens the actions menu for a thread (or the sidebar's own, with none).
export function openMenu(t, { at = "pointer", from = "sidebar", pr = null, view = "main" } = {}) {
  openOverlay({
    mod: "sesh-menu", kind: "popup", at, width: 260, height: menuHeight(t, { pr, view }),
    props: { address: t?.address ?? null, from, pr, view },
  });
}
// The popup's height for its items (sesh-menu draws rows 26 px, lines 9 px, a 36 px head).
export function menuHeight(t, { pr = null, view = "main" } = {}) {
  const items = view === "snooze" ? new Array(6).fill(["x"]) : t ? threadMenu(t, { pr, pinnedCount: threads().pinned.length }) : viewMenu();
  return 36 + 8 + items.reduce((h, x) => h + (x[0] === "sep" ? 9 : 26), 0) + 2;
}

export function snoozeOptions(now = new Date()) {
  const at = (d, h) => { const x = new Date(now); x.setDate(x.getDate() + d); x.setHours(h, 0, 0, 0); return x.getTime(); };
  const evening = now.getHours() < 18 ? at(0, 18) : at(1, 18);
  const nextMon = (() => { const x = new Date(now); const d = ((8 - x.getDay()) % 7) || 7; return at(d, 9); })();
  return [
    { label: "In an hour", until: now.getTime() + 3600e3 },
    { label: "In 3 hours", until: now.getTime() + 3 * 3600e3 },
    { label: "This evening", until: evening },
    { label: "Tomorrow", until: at(1, 9) },
    { label: "Next week", until: nextMon },
  ];
}
// Free-form snooze times: 90m, 3h, 2d, 15:04, evening, tomorrow, next-week.
export function parseWhen(text, now = new Date()) {
  const s = (text ?? "").trim().toLowerCase();
  if (!s) return null;
  let m = s.match(/^(\d+)\s*(m|min|h|d)$/);
  if (m) return now.getTime() + Number(m[1]) * { m: 60e3, min: 60e3, h: 3600e3, d: 86400e3 }[m[2]];
  m = s.match(/^(\d{1,2}):(\d{2})$/);
  if (m) {
    const x = new Date(now); x.setHours(Number(m[1]), Number(m[2]), 0, 0);
    if (x.getTime() <= now.getTime()) x.setDate(x.getDate() + 1);
    return x.getTime();
  }
  const named = { evening: 2, tonight: 2, tomorrow: 3, "next-week": 4, "next week": 4 };
  if (s in named) return snoozeOptions(now)[named[s]].until;
  return null;
}

// What a waiting agent asks for, read off its screen: Claude Code draws its
// permission prompt in a box ("Bash command" / "npm test" / "Do you want to…").
export async function needOf(address) {
  try {
    const r = await hc(["read", address]);
    if (r.code !== 0) return null;
    const lines = screenLines(r.stdout).map((l) => l.replace(/[│╭╮╰╯─┃┏┓┗┛━]/g, "").trim());
    const ask = lines.findLastIndex((l) => /^Do you want|^Would you like|\? *$/.test(l));
    if (ask < 0) return null;
    const body = lines.slice(Math.max(0, ask - 14), ask).filter(Boolean);
    const kindIdx = body.findIndex((l) => /(command|file|fetch|search|tool|edit|write|read|create)$/i.test(l) || /^[A-Z][\w ]+$/.test(l));
    if (kindIdx >= 0 && body[kindIdx + 1]) return `Allow ${body[kindIdx].replace(/ command$/i, "")}: ${body[kindIdx + 1]}`;
    return lines[ask];
  } catch {
    return null;
  }
}

// Ahead/behind its upstream, per folder.
export async function aheadBehind(cwd) {
  try {
    const r = await exec("git", ["rev-list", "--left-right", "--count", "@{upstream}...HEAD"], { cwd, timeout: 4000 });
    if (r.code !== 0) return null;
    const [behind, ahead] = r.stdout.trim().split(/\s+/).map(Number);
    return { ahead, behind };
  } catch {
    return null;
  }
}
export async function pullRequest(cwd) {
  try {
    const r = await exec("gh", ["pr", "view", "--json", "number,state"], { cwd, timeout: 8000 });
    if (r.code !== 0) return null;
    return JSON.parse(r.stdout);
  } catch {
    return null;
  }
}

// ── Look ────────────────────────────────────────────────────────────

export function glyph(t, status, size = 12) {
  if (status === "blocked") return svg(ICON("bell")).size(size).flex_none().text_color(t.blocked);
  if (status === "done") return svg(ICON("check")).size(size).flex_none().text_color(t.done);
  if (status === "working") return svg(ICON("working")).size(size).flex_none().text_color(t.working).loop("opacity", { from: 0.35, to: 1, period: 1100 });
  return svg(ICON("circle")).size(size - 2).flex_none().text_color(t.dim);
}

// "Threads ////////////// +": the hatched title rule.
export function hatchTitle(t, title, right) {
  return div()
    .flex()
    .flex_row()
    .items_center()
    .gap(8)
    .w_full()
    .child(div().flex_none().font_bold().text_color(t.accent).child(title))
    .child(div().flex_1().overflow_hidden().whitespace_nowrap().text_color(t.accent).opacity(0.45).child("/".repeat(240)))
    .when(!!right, (el) => el.child(right));
}

// "PINNED ─────────── 1"
export function sectionHead(t, icon, label, count) {
  return div()
    .flex()
    .flex_row()
    .items_center()
    .gap(6)
    .w_full()
    .pt(10)
    .pb(2)
    .text_color(t.muted)
    .font_bold()
    .child(svg(ICON(icon)).size(12).flex_none().text_color(t.muted))
    .child(div().flex_none().child(label))
    .child(div().flex_1().h(1).bg(t.border))
    .child(div().flex_none().font_normal().child(String(count)));
}

// One thread, two lines:
//   •🔔 Fix the login redirect              2m ●
//       api · Allow Bash: npm test   fix/login ↑2  #42
// `hovered` shows the pin, snooze and settle buttons; `onAct(name, cx)` runs one,
// `onMenu(event, cx)` opens the menu on a right click, `onPR` opens its PR.
export function threadRow(t, th, { selected = false, need = null, git = null, now = Date.now(), id, onClick, wide = false, hovered = false, onHover, onAct, onMenu } = {}) {
  const detail = (() => {
    if (th.closed) return [t.dim, "closed · ↵ resumes"];
    if (th.snoozed) return [t.muted, `snoozed until ${fmtTime(th.snoozed)}`];
    if (th.status === "blocked") return [t.blocked, need ?? "needs you"];
    if (th.status === "working") return [t.muted, `› working ${age(now - th.at)}`];
    if (th.status === "done") return [t.done, "finished"];
    return [t.dim, "idle"];
  })();
  const branch = th.branch && !MAIN.test(th.branch) ? th.branch : null;
  const button = (name, icon, tip, on = false) => div().id(`${id}-${name}`).flex_none().p(2).rounded(3).cursor_pointer()
    .text_color(on ? t.accent : t.muted).hover((s) => s.bg(t.raised).text_color(t.foreground))
    .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
    .on_click((_e, cx) => { cx.stop_propagation(); onAct?.(name, cx); })
    .child(svg(ICON(icon)).size(12).text_color(on ? t.accent : t.muted));
  const right = div().flex().flex_row().flex_none().items_center().gap(6).text_color(t.dim)
    .when(wide && !!branch, (el) => el.child(div().max_w(160).truncate().child(branch)))
    .when(!!git?.ab?.ahead, (el) => el.child(`↑${git.ab.ahead}`))
    .when(!!git?.ab?.behind, (el) => el.child(`↓${git.ab.behind}`))
    .when(!!git?.pr, (el) => el.child(
      div().id(`${id}-pr`).flex().flex_row().items_center().gap(2).cursor_pointer()
        .text_color(git.pr.state === "MERGED" ? t.accent : git.pr.state === "CLOSED" ? t.dim : t.done)
        .hover((s) => s.text_color(t.foreground))
        .on_mouse_down("left", (_e, cx) => cx.stop_propagation())
        .on_click((_e, cx) => { cx.stop_propagation(); onAct?.("openPR", cx); })
        .child(svg(ICON("pr")).size(11).text_color(git.pr.state === "MERGED" ? t.accent : t.done))
        .child(`#${git.pr.number}`)));
  const showButtons = hovered && !!onAct && !th.closed;
  return div()
    .id(id)
    .flex()
    .flex_col()
    .w_full()
    .pl(selected ? 7 : 10)
    .pr(10)
    .py(4)
    .cursor_pointer()
    .when(selected, (el) => el.bg(t.selected).border_l(3).border_color(t.accent))
    .hover((s) => (selected ? s : s.bg(t.hover)))
    .when(!!onHover, (el) => el.on_hover((h, cx) => onHover(h, cx)))
    .when(!!onClick, (el) => el.on_click(onClick))
    .when(!!onMenu, (el) => el.on_mouse_down("right", (e, cx) => { cx.stop_propagation(); onMenu(e, cx); }))
    .child(
      div().flex().flex_row().items_center().gap(5).w_full()
        .child(div().w(6).flex_none().text_color(t.accent).child(th.unread ? "•" : ""))
        .child(th.closed ? svg(ICON("box")).size(11).flex_none().text_color(t.dim) : glyph(t, th.status))
        .child(div().flex_1().min_w(0).truncate().text_color(th.closed ? t.muted : t.foreground).when(selected || th.unread || th.status === "blocked", (d) => d.font_bold()).child(th.title))
        .when(showButtons, (el) => el
          .child(button("togglePin", "pin", "pin", th.pinned))
          .child(button(th.snoozed ? "wake" : "snooze1h", th.snoozed ? "sun" : "clock", "snooze"))
          .child(button("toggleSettle", th.settled ? "undo" : "box", "settle")))
        .when(!showButtons, (el) => el
          .child(div().flex_none().text_color(t.dim).child(age(now - th.at)))
          .child(div().w(10).flex_none().text_color(t.accent).child(th.unread ? "●" : ""))),
    )
    .child(
      div().flex().flex_row().items_center().gap(6).w_full().pl(23)
        .child(div().flex_none().max_w(120).truncate().text_color(t.muted).child(th.spaceName))
        .child(div().flex_none().text_color(t.dim).child("·"))
        .child(div().flex_1().min_w(0).truncate().text_color(detail[0]).child(detail[1]))
        .child(right),
    );
}

export function fmtTime(ms) {
  const d = new Date(ms);
  const today = new Date();
  const hm = `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;
  if (d.toDateString() === today.toDateString()) return hm;
  return `${["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"][d.getDay()]} ${hm}`;
}

// Keeps hints (what a waiting agent asks) and git facts (ahead/behind, PR)
// fresh for the threads a view shows. Owned by a view instance. A thread
// whose PR merged is settled once (restoring it sticks).
export class Facts {
  constructor() { this.need = {}; this.git = {}; this.prAt = {}; }
  start(cx, list) {
    cx.timer.every(2500, (cx) => this.needs(cx, list()));
    cx.timer.every(20000, (cx) => this.gits(cx, list()));
    cx.spawn(async (cx) => { await this.needs(cx, list()); await this.gits(cx, list()); });
  }
  async needs(cx, ts) {
    let dirty = false;
    for (const th of ts) {
      if (th.closed) continue;
      if (th.status !== "blocked") { if (this.need[th.address]) { delete this.need[th.address]; dirty = true; } continue; }
      const n = await needOf(th.address);
      if (n !== this.need[th.address]) { this.need[th.address] = n; dirty = true; }
    }
    if (dirty) cx.notify();
  }
  async gits(cx, ts) {
    const seen = new Set();
    const now = Date.now();
    for (const th of ts) {
      if (th.closed || !th.cwd || !th.branch || seen.has(th.cwd)) continue;
      seen.add(th.cwd);
      const g = (this.git[th.cwd] ??= {});
      g.ab = await aheadBehind(th.cwd);
      if (!MAIN.test(th.branch) && (!this.prAt[th.cwd] || now - this.prAt[th.cwd] > 120000)) {
        this.prAt[th.cwd] = now;
        g.pr = await pullRequest(th.cwd);
      }
    }
    const s = state();
    const merge = ts.filter((th) => !th.closed && th.cwd && this.git[th.cwd]?.pr?.state === "MERGED"
      && !s.merged[th.address] && !th.settled && th.status !== "working" && th.status !== "blocked");
    if (merge.length) patch((x) => { for (const th of merge) { x.merged[th.address] = true; x.settled[th.address] = now; } return x; });
    cx.notify();
  }
  of(th) { return { need: this.need[th.address] ?? null, git: th.cwd ? this.git[th.cwd] ?? null : null }; }
}

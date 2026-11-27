// The herdr-sesh layout's notifications: Herdr's toast, plus a macOS notification for
// agent events (one waiting on you, finished or failed, a merged PR to clean up) while
// Herdr isn't the app in front, except in the config's [notifications] quiet_hours.
// Clicking the toast runs its action (e.g. go to the agent) and dismisses it; clicking
// the macOS one brings Herdr forward and runs the same action.
//
// The macOS one goes through, in order: "Herdr Notifier.app" (scripts/notifier.sh makes
// it: terminal-notifier with Herdr's icon), terminal-notifier, or osascript (no click
// action, shown as Script Editor's). The last 40 toasts, and whether each went to macOS,
// are in ~/.config/herdr-app/data/sesh-toast/recent.jsonl.

import { View, div, image } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { props, closeOverlay, run, theme, window, exec, shared } from "herdr";

// Inside ~/.config/herdr-sesh/config.toml's [notifications] quiet_hours ("22:00-08:00"),
// as sesh-bar last read it.
function quiet(now = new Date()) {
  const m = String(shared("sesh.config")?.notifications?.quiet_hours ?? "").match(/^\s*(\d{1,2}):(\d{2})\s*-\s*(\d{1,2}):(\d{2})\s*$/);
  if (!m) return false;
  const from = Number(m[1]) * 60 + Number(m[2]), to = Number(m[3]) * 60 + Number(m[4]);
  const t = now.getHours() * 60 + now.getMinutes();
  return from <= to ? t >= from && t < to : t >= from || t < to;
}

const DOT = { error: "blocked", warning: "working", success: "done", info: "accent" };

// Actions that go to an agent: Herdr's own "needs you" / "finished" toasts carry one.
const AGENT_ACTIONS = new Set(["agent.focus", "pane.focus", "terminal.show", "terminal.peek"]);
// Only agent events go to macOS; the rest (errors, "press again", "copied") stay in the window.
export function forAgent(p) {
  const name = String(p.action?.name ?? "");
  if (AGENT_ACTIONS.has(name) || name.startsWith("sesh-bar.ask-cleanup:")) return true;
  return /\b(needs you|needs input|is waiting|finished|failed|exited)\b/i.test(String(p.title ?? ""));
}

// Single-quoted for sh.
const sq = (x) => `'${String(x).replace(/'/g, "'\\''")}'`;

// $1 title, $2 message, $3 group, $4 click command (or empty).
const SEND = `
N="$HOME/Applications/Herdr Notifier.app/Contents/MacOS/terminal-notifier"
[ -x "$N" ] || N=$(PATH="$PATH:/opt/homebrew/bin:/usr/local/bin" command -v terminal-notifier || true)
if [ -n "$N" ]; then
  m=$2; case "$m" in "["*|"-"*) m="\\\\$m" ;; esac
  if [ -n "$4" ]; then exec "$N" -title "$1" -message "$m" -group "$3" -activate dev.herdr.app -execute "$4"
  else exec "$N" -title "$1" -message "$m" -group "$3" -activate dev.herdr.app; fi
fi
exec osascript -e 'on run a' -e 'display notification (item 2 of a) with title "Herdr" subtitle (item 1 of a)' -e 'end run' "$1" "$2"
`;

// Keeps the last 40 toasts, to see what went to macOS and what didn't.
const LOG = `d="$HOME/.config/herdr-app/data/sesh-toast"; mkdir -p "$d"; f="$d/recent.jsonl"
{ [ -f "$f" ] && tail -n 39 "$f"; printf '%s\\n' "$1"; } > "$f.tmp" && mv "$f.tmp" "$f"`;

export default class Toast extends View {
  init() {
    const p = props() ?? {};
    const w = window();
    const mac = Boolean(w && w.platform === "macos" && !w.active && p.title && !quiet() && forAgent(p));
    exec("sh", ["-c", LOG, "sh", JSON.stringify({ at: new Date().toISOString(), title: p.title, body: p.body, level: p.level, action: p.action, macos: mac })], { timeout: 5000 }).catch(() => {});
    if (!mac) return;
    const a = p.action?.name ? p.action : null;
    const click = a ? `PATH="$HOME/.local/bin:/opt/homebrew/bin:/usr/local/bin:$PATH" herdr-app do ${sq(a.name)}${a.args && Object.keys(a.args).length ? " " + sq(JSON.stringify(a.args)) : ""}` : "";
    const group = `herdr-sesh:${a ? a.name + JSON.stringify(a.args ?? {}) : p.title}`;
    exec("sh", ["-c", SEND, "sh", p.title, p.body || " ", group, click], { timeout: 5000 }).catch(() => {});
  }

  render(cx) {
    const t = theme();
    const p = props() ?? {};
    const login = /^([a-z0-9][a-z0-9_.-]{0,31}) needs login; run hc machine login \1$/.exec(p.body ?? "");
    const command = login ? `hc machine login ${login[1]}` : null;
    return h_flex()
      .id("toast")
      .size_full()
      .px(18)
      .gap(12)
      .items_center()
      .bg(t.popover)
      .cursor_pointer()
      .hover((s) => s.bg(t.hover))
      .on_click(() => {
        if (p.action?.name) run(p.action.name, p.action.args ?? {});
        closeOverlay(p.overlay);
      })
      .child(p.icon ? image(p.icon).w(18).h(18).flex_none() : div().size(8).rounded(4).flex_none().bg(t[DOT[p.level] ?? "accent"]))
      .child(
        v_flex()
          .flex_1()
          .overflow_hidden()
          .child(div().truncate().font_semibold().text_size(13).text_color(t.foreground).child(login ? `${login[1]} needs a login: run` : p.title ?? ""))
          .when(!login && Boolean(p.body), (el) => el.child(div().truncate().text_size(12).text_color(t.muted).child(p.body ?? "")))
          .when(Boolean(login), (el) => el.child(h_flex().gap(8).items_center()
            .child(div().flex_1().min_w(0).truncate().tooltip(command).text_size(12).text_color(t.muted).child(command))
            .child(div().id("toast-login-copy").flex_none().px(6).py(3).rounded(4).bg(t.raised)
              .text_size(12).text_color(t.foreground).cursor_pointer().hover(s => s.bg(t.hover))
              .on_click((_e, cx) => { cx.stop_propagation(); cx.write_to_clipboard(command); this.copied = true; cx.notify(); })
              .child(this.copied ? "Copied" : "Copy")))),
      );
  }
}

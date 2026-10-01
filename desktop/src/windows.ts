/**
 * Coordination between Yui windows (main, detached avatar, detached chat).
 *
 * Each window is an independent client of the same core session; the core
 * fans frames out to every socket. Only audio needs an owner, otherwise each
 * window would play the reply. The owner is the alive window with the highest
 * priority: the avatar window (so lip sync is local), then main, then chat.
 */

export type WindowRole = "main" | "avatar" | "chat";

const PRIORITY: Record<WindowRole, number> = { avatar: 3, main: 2, chat: 1 };
/** Whose session wins when windows disagree (main starts conversations). */
const AUTHORITY: Record<WindowRole, number> = { main: 3, chat: 2, avatar: 1 };
const HEARTBEAT_MS = 1000;
const STALE_MS = 3200;

interface Peer { role: WindowRole; session: string; at: number }
type Message =
  | { type: "hello"; id: string; role: WindowRole; session: string }
  | { type: "bye"; id: string }
  | { type: "close"; id: string; role: WindowRole }
  | { type: "user-text"; id: string; session: string; text: string };

export class WindowHub {
  readonly id = Math.random().toString(36).slice(2);
  private readonly peers = new Map<string, Peer>();
  private readonly channel?: BroadcastChannel;
  private session = "";
  private timer = 0;
  private lastSignature = "";

  /** Text typed in one window; the core does not echo typed user turns. */
  onUserText?: (session: string, text: string) => void;

  constructor(readonly role: WindowRole, private readonly onChange: () => void) {
    if (typeof BroadcastChannel === "undefined") return;
    this.channel = new BroadcastChannel("yui-windows");
    this.channel.onmessage = (event: MessageEvent<Message>) => this.receive(event.data);
    this.announce();
    this.timer = window.setInterval(() => { this.announce(); this.prune(); }, HEARTBEAT_MS);
    window.addEventListener("pagehide", () => this.close());
  }

  setSession(session: string): void {
    this.session = session;
    this.announce();
  }

  /** A session another window already uses, so a new window can join it. */
  knownSession(): string {
    let best: Peer | undefined;
    for (const peer of this.peers.values()) {
      if (peer.session && (!best || AUTHORITY[peer.role] > AUTHORITY[best.role])) best = peer;
    }
    return best?.session ?? "";
  }

  /** Waits briefly for peers to introduce themselves. */
  async discover(timeoutMs = 700): Promise<string> {
    if (!this.channel) return "";
    const found = this.knownSession();
    if (found) return found;
    this.announce();
    await new Promise(resolve => window.setTimeout(resolve, timeoutMs));
    return this.knownSession();
  }

  hasPeer(role: WindowRole): boolean {
    this.prune();
    for (const peer of this.peers.values()) if (peer.role === role) return true;
    return false;
  }

  get ownsAudio(): boolean {
    this.prune();
    for (const [id, peer] of this.peers) {
      const priority = PRIORITY[peer.role] - PRIORITY[this.role];
      if (priority > 0 || (priority === 0 && id < this.id)) return false;
    }
    return true;
  }

  shareUserText(text: string): void {
    if (!this.session) return;
    try { this.channel?.postMessage({ type: "user-text", id: this.id, session: this.session, text } satisfies Message); } catch { /* closed */ }
  }

  /** Asks every window with this role to close itself. */
  requestClose(role: WindowRole): void {
    try { this.channel?.postMessage({ type: "close", id: this.id, role } satisfies Message); } catch { /* closed */ }
  }

  close(): void {
    if (!this.channel) return;
    window.clearInterval(this.timer);
    try { this.channel.postMessage({ type: "bye", id: this.id } satisfies Message); } catch { /* closed */ }
    this.channel.close();
  }

  private announce(): void {
    try {
      this.channel?.postMessage({ type: "hello", id: this.id, role: this.role, session: this.session } satisfies Message);
    } catch { /* channel closed during unload */ }
  }

  private receive(message: Message): void {
    if (!message || message.id === this.id) return;
    if (message.type === "user-text") {
      this.onUserText?.(message.session, message.text);
      return;
    }
    if (message.type === "close") {
      if (message.role === this.role) void closeSelf();
      return;
    }
    if (message.type === "bye") this.peers.delete(message.id);
    else {
      const known = this.peers.has(message.id);
      this.peers.set(message.id, { role: message.role, session: message.session, at: Date.now() });
      // Answer newcomers right away instead of waiting for the next beat.
      if (!known) this.announce();
    }
    this.notify();
  }

  private prune(): void {
    const now = Date.now();
    for (const [id, peer] of this.peers) if (now - peer.at > STALE_MS) this.peers.delete(id);
    this.notify();
  }

  private notify(): void {
    const signature = Array.from(this.peers.entries()).map(([id, peer]) => `${id}:${peer.role}:${peer.session}`).sort().join("|");
    if (signature === this.lastSignature) return;
    this.lastSignature = signature;
    this.onChange();
  }
}

export const isTauri = (): boolean => typeof window !== "undefined" && "__TAURI_INTERNALS__" in window;

const SIZES: Record<Exclude<WindowRole, "main">, { width: number; height: number; title: string }> = {
  avatar: { width: 420, height: 640, title: "Yui — аватар" },
  chat: { width: 420, height: 620, title: "Yui — чат" },
};

/** Opens (or focuses) a detached window for the given role. */
export async function openDetached(role: Exclude<WindowRole, "main">, session: string, onTop: boolean): Promise<void> {
  const page = role === "avatar" ? "/avatar.html" : "/";
  const query = new URLSearchParams({ view: role });
  if (session) query.set("session", session);
  const url = `${page}?${query}`;
  const size = SIZES[role];
  if (isTauri()) {
    const { WebviewWindow } = await import("@tauri-apps/api/webviewWindow");
    const existing = await WebviewWindow.getByLabel(role);
    if (existing) { await existing.setFocus(); return; }
    const created = new WebviewWindow(role, {
      url, title: size.title, width: size.width, height: size.height,
      minWidth: 240, minHeight: 300, resizable: true,
      transparent: role === "avatar", decorations: role !== "avatar", shadow: role !== "avatar",
      alwaysOnTop: role === "avatar" ? onTop : false,
    });
    await new Promise<void>((resolve, reject) => {
      void created.once("tauri://created", () => resolve());
      void created.once("tauri://error", event => reject(new Error(String(event.payload))));
    });
    return;
  }
  const opened = window.open(url, `yui-${role}`, `popup,width=${size.width},height=${size.height}`);
  if (!opened) throw new Error("Браузер заблокировал окно. Разрешите всплывающие окна для Yui.");
  opened.focus();
}

/** Tauri-only window controls for the avatar window; no-ops in a browser. */
export async function currentWindow() {
  if (!isTauri()) return undefined;
  const { getCurrentWindow } = await import("@tauri-apps/api/window");
  return getCurrentWindow();
}

async function closeSelf(): Promise<void> {
  const tauriWindow = await currentWindow();
  if (tauriWindow) await tauriWindow.close();
  else window.close();
}

/** Closes a detached window (Tauri by label, browsers by message). */
export async function closeDetached(hub: WindowHub, role: Exclude<WindowRole, "main">): Promise<void> {
  if (isTauri()) {
    const { WebviewWindow } = await import("@tauri-apps/api/webviewWindow");
    const existing = await WebviewWindow.getByLabel(role);
    if (existing) { await existing.close(); return; }
  }
  hub.requestClose(role);
}

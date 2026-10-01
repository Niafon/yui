/**
 * Client for Yui Core. The stage is a viewer: it holds no personality state
 * and never persists memory of its own (CL-006, ADR-007).
 */

export interface Frame {
  type: string;
  at: string;
  session_id?: string;
  payload?: unknown;
}

export interface ProviderStatus {
  info: { id: string; kind: string; driver: string; model?: string; local: boolean };
  is_default: boolean;
  healthy: boolean;
}

export interface SessionInfo {
  id: string;
  identity_id: string;
  state: string;
  mode: string;
}


export interface InferencePreferences {
  mode: "auto" | "max_quality" | "balanced" | "gaming" | "manual";
  preferred_provider?: string;
  locked_model?: string;
  allow_auto_downgrade: boolean;
  minimum_quality: number;
}

export interface ResourceTelemetry {
  cpu_percent: number;
  gpu_percent: number;
  ram_used_mb: number;
  ram_free_mb: number;
  vram_used_mb: number;
  vram_free_mb: number;
  foreground_process?: string;
  game_active: boolean;
  fps?: number;
  baseline_fps?: number;
  frame_time_ms?: number;
  source: string;
  at: string;
}

export interface InferenceModelStatus {
  provider_id: string;
  local: boolean;
  kind: string;
  model?: string;
  model_family?: string;
  backend?: string;
  quality: number;
  estimated_ram_mb?: number;
  estimated_vram_mb?: number;
  gaming_safe: boolean;
  auto_select: boolean;
  keep_warm: boolean;
  worker_id?: string;
}

export interface InferenceDecision {
  provider_id: string;
  at?: string;
  model?: string;
  model_family?: string;
  kind: string;
  backend?: string;
  quality: number;
  mode: string;
  reason: string;
  explicit: boolean;
  snapshot: ResourceTelemetry;
}

export interface InferenceStatus {
  preferences: InferencePreferences;
  telemetry: ResourceTelemetry;
  last_decision?: InferenceDecision;
  last_llm_decision?: InferenceDecision;
  models: InferenceModelStatus[];
}

export interface CatalogModel {
  id: string;
  kind: string;
  service: string;
  driver: string;
  endpoint?: string;
  model?: string;
  model_family?: string;
  local: boolean;
  backend?: string;
  quality?: number;
  estimated_ram_mb?: number;
  estimated_vram_mb?: number;
  auto_select: boolean;
  tags?: string[];
  is_default: boolean;
  credential_ready: boolean;
  api_key_env?: string;
  user_added: boolean;
}

export interface AddModelInput {
  kind: "llm" | "vision" | "embeddings";
  source: "local" | "provider";
  service: string;
  endpoint: string;
  model: string;
  api_key_env: string;
}

export interface MemoryItem {
  id: string;
  content: string;
  category: string;
  status: string;
  recorded_at: string;
  pinned?: boolean;
}

export type ConversationMode = "normal" | "work" | "learning" | "brief" | "support" | "playful" | "public" | "night" | "silent";

export interface Identity {
  id: string;
  name: string;
  speech_style?: string;
  relationship?: string;
  traits?: Record<string, number>;
  initiative: number;
  mode: ConversationMode;
  presentation?: { voice_profile?: string; live2d_package?: string };
}

export interface IdentityPatch {
  name?: string;
  speech_style?: string;
  relationship?: string;
  traits?: Record<string, number>;
  initiative?: number;
  mode?: ConversationMode;
  voice_profile?: string;
}

export interface Turn {
  id: string;
  seq: number;
  role: string;
  text: string;
  started_at: string;
  completed_at?: string;
}

export interface DeviceInfo {
  id: string;
  name: string;
  kind: string;
  paired_at: string;
  last_seen_at: string;
  revoked_at?: string;
}

export interface Grant {
  id: string;
  subject_kind: string;
  subject_id: string;
  category: string;
  action: string;
  decision: string;
  expires_at?: string;
  created_at: string;
}

export interface AuditRecord {
  id: string;
  at: string;
  action: string;
  reason?: string;
  provider?: string;
  categories?: string[];
  result: string;
}

export interface PendingPermission {
  id: string;
  subject: string;
  category: string;
  action: string;
  provider?: string;
}

export interface PendingTool {
  invocation_id: string;
  tool: string;
  human_readable_action: string;
  required_method: string;
  expires_at: string;
}

export class CoreClient {
  private control?: WebSocket;
  private data?: WebSocket;

  constructor(
    private readonly base: string,
    private readonly token: string,
  ) {}

  private get httpBase(): string {
    return this.base.replace(/^ws/, "http");
  }

  private get wsBase(): string {
    return this.base.replace(/^http/, "ws");
  }

  /** Go encodes an empty slice as null; lists are normalised to arrays. */
  private async list<T>(path: string): Promise<T[]> {
    return (await this.request<T[] | null>(path)) ?? [];
  }

  async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const res = await fetch(`${this.httpBase}${path}`, {
      ...init,
      headers: {
        "Content-Type": "application/json",
        Authorization: `Bearer ${this.token}`,
        ...(init.headers ?? {}),
      },
    });
    if (!res.ok) {
      const body = await res.text();
      // Errors surface to the operator; the stage never hides a failure.
      throw new Error(`${res.status} ${res.statusText}: ${body}`);
    }
    return (await res.json()) as T;
  }

  status() {
    return this.request<{ providers: ProviderStatus[]; version: string }>("/v1/status");
  }

  identities() {
    return this.list<Identity>("/v1/identities");
  }

  updateIdentity(id: string, patch: IdentityPatch) {
    return this.request<Identity>(`/v1/identities/${encodeURIComponent(id)}`, {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
  }

  session(id: string) {
    return this.request<SessionInfo & { closed_at?: string }>(`/v1/sessions/${encodeURIComponent(id)}`);
  }

  turns(sessionId: string, limit = 60) {
    return this.list<Turn>(`/v1/sessions/${encodeURIComponent(sessionId)}/turns?limit=${limit}`);
  }

  closeSession(id: string) {
    return this.request<unknown>(`/v1/sessions/${encodeURIComponent(id)}/close`, { method: "POST", body: "{}" });
  }

  pinMemory(id: string, pinned: boolean) {
    return this.request<unknown>(`/v1/memory/${encodeURIComponent(id)}/pin`, { method: "POST", body: JSON.stringify({ pinned }) });
  }

  confirmMemory(id: string, correct: boolean) {
    return this.request<MemoryItem>(`/v1/memory/${encodeURIComponent(id)}/confirm`, { method: "POST", body: JSON.stringify({ correct }) });
  }

  deleteMemory(id: string) {
    return this.request<unknown>(`/v1/memory/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  addMemory(identityId: string, content: string) {
    return this.request<MemoryItem>("/v1/memory", {
      method: "POST",
      body: JSON.stringify({ identity_id: identityId, category: "preferences", content, importance: 0.7 }),
    });
  }

  grants() {
    return this.list<Grant>("/v1/permissions");
  }

  revokeGrant(id: string) {
    return this.request<unknown>(`/v1/permissions/${encodeURIComponent(id)}`, { method: "DELETE" });
  }

  devices() {
    return this.list<DeviceInfo>("/v1/devices");
  }

  revokeDevice(id: string) {
    return this.request<unknown>(`/v1/devices/${encodeURIComponent(id)}/revoke`, { method: "POST", body: "{}" });
  }

  memories(identityId: string) {
    return this.list<MemoryItem>(`/v1/memory?identity_id=${encodeURIComponent(identityId)}&active=true&limit=100`);
  }

  audit(identityId: string) {
    return this.list<AuditRecord>(`/v1/audit?identity_id=${encodeURIComponent(identityId)}&limit=100`);
  }

  startSession(identityId?: string) {
    return this.request<SessionInfo>("/v1/sessions", {
      method: "POST",
      body: JSON.stringify({ identity_id: identityId ?? "", mode: "normal" }),
    });
  }

  resolvePermission(id: string, allow: boolean, remember: boolean) {
    return this.request<unknown>(`/v1/permissions/pending/${id}`, {
      method: "POST",
      body: JSON.stringify({ allow, remember }),
    });
  }

  pendingTools() {
    return this.list<PendingTool>("/v1/tools/pending");
  }

  confirmTool(id: string, approved: boolean, sessionId: string) {
    return this.request<{ result: string; approved: boolean }>(`/v1/tools/pending/${encodeURIComponent(id)}`, {
      method: "POST",
      body: JSON.stringify({ approved, method: "button", session_id: sessionId }),
    });
  }

  setDefaultProvider(kind: string, providerId: string) {
    return this.request<unknown>("/v1/providers/default", {
      method: "POST",
      body: JSON.stringify({ kind, provider_id: providerId }),
    });
  }

  models() {
    return this.list<CatalogModel>("/v1/models");
  }

  addModel(input: AddModelInput) {
    return this.request<{ id: string; models: CatalogModel[] }>("/v1/models", {
      method: "POST",
      body: JSON.stringify(input),
    });
  }

  inferenceStatus() {
    return this.request<InferenceStatus>("/v1/inference");
  }

  updateInferencePreferences(patch: Partial<InferencePreferences>) {
    return this.request<InferenceStatus>("/v1/inference/preferences", {
      method: "PATCH",
      body: JSON.stringify(patch),
    });
  }

  reportInferenceTelemetry(telemetry: Partial<ResourceTelemetry>) {
    return this.request<{ accepted: boolean }>("/v1/inference/telemetry", {
      method: "POST",
      body: JSON.stringify(telemetry),
    });
  }

  /** Opens both planes; onFrame receives control and data frames alike. */
  connect(sessionId: string, onFrame: (f: Frame) => void, onClose: () => void): void {
    this.close();
    const url = (plane: string) =>
      `${this.wsBase}/v1/${plane}?session=${encodeURIComponent(sessionId)}&token=${encodeURIComponent(this.token)}`;

    this.control = new WebSocket(url("control"));
    this.data = new WebSocket(url("data"));
    const control = this.control;
    const data = this.data;
    const isCurrent = () => this.control === control && this.data === data;

    for (const socket of [this.control, this.data]) {
      socket.addEventListener("message", (event) => {
        if (!isCurrent()) return;
        try {
          onFrame(JSON.parse(String(event.data)) as Frame);
        } catch {
          // A malformed frame is dropped rather than breaking the stream.
        }
      });
      socket.addEventListener("close", () => {
        if (!isCurrent()) return;
        this.close();
        onClose();
      });
    }
  }

  /** Sends a typed message on the data plane. */
  send(message: Record<string, unknown>): void {
    if (this.data?.readyState !== WebSocket.OPEN) throw new Error("Соединение ещё не готово. Подключитесь повторно.");
    this.data.send(JSON.stringify(message));
  }

  sendAudio(pcm: ArrayBuffer): void {
    if (this.data?.readyState !== WebSocket.OPEN) throw new Error("Соединение потеряно");
    this.data.send(pcm);
  }

  sendText(text: string): void {
    this.send({ type: "text", text });
  }

  cancelTurn(): void {
    this.send({ type: "session.cancel" });
  }

  close(): void {
    const control = this.control;
    const data = this.data;
    this.control = undefined;
    this.data = undefined;
    control?.close();
    data?.close();
  }
}

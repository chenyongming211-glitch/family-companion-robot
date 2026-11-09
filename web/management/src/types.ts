export type Scope = "admin" | "family";
export type PageRoute = {
  page: string;
  device: string;
  chat: string;
  memory: string;
  family: string;
};
export type Device = {
  id: string;
  familyId: string;
  name: string;
  location: string;
  model: string;
  connection: string;
  activity: string;
  lastSeen: string;
  battery: number | null;
  volume: number | null;
  brightness: number | null;
  revision: number | null;
};
export type Turn = {
  id: string;
  user: string;
  played: string;
  generated: string;
  playback: "played" | "interrupted" | "unknown";
  saved: boolean;
  textSource?: string;
  boundary?: string;
  exactStopKnown?: boolean;
  recognitionMayDiffer?: boolean;
  sourceRecordId?: string;
  sourceTurnId?: string;
};
export type Conversation = {
  id: string;
  deviceId: string;
  familyId: string;
  title: string;
  date: string;
  time: string;
  status: string;
  endReason: string;
  summary: string;
  bodyAvailable: boolean;
  turns: Turn[];
  sourceKind?: string;
  recordStatus?: "saved" | "pending" | "error";
  startedAt?: string;
  lastActivityAt?: string;
  endedAt?: string;
  archiveReason?: string;
  turnCount?: number;
  connectionCount?: number;
  sources?: Array<{ recordId: string; sessionId: string; startedAt: string }>;
};
export type Memory = {
  id: string;
  deviceId: string;
  conversationId: string;
  text: string;
  date: string;
  user?: string;
  assistant?: string;
  textSource?: string;
  boundary?: string;
};
export type Member = {
  id: string;
  name: string;
  email: string;
  role: string;
  access: string;
  status: string;
};
export type Family = { id: string; name: string; members: Member[] };
export type Privacy = {
  keepRecords: boolean;
  useMemory: boolean;
  retention: "7" | "30" | "90";
};
export type Account = { email: string; role: string; scope: Scope };
export type Capability = {
  id: string;
  name: string;
  stage: string;
  description: string;
  detail: string;
};
export type AuditEvent = {
  id: string;
  time: string;
  actor: string;
  action: string;
  target: string;
  result: string;
};
export type Task = {
  id: string;
  name: string;
  stage: string;
  trigger: string;
  detail: string;
};
export type Firmware = {
  model: string;
  stage: string;
  features: string;
  acceptance: string;
};
export type Snapshot = {
  devices: Device[];
  conversations: Conversation[];
  memories: Memory[];
  families: Family[];
  privacy: Record<string, Privacy>;
  accounts: Partial<Record<Scope, Account>>;
  audit: AuditEvent[];
};

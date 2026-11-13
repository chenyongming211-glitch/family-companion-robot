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
  chatId?: string;
  turnId?: string;
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
export type Account = { email: string; role: string; scope: Scope };
export type DeviceSettings = {
  deviceId: string;
  name: string;
  location: string;
  version: string;
  canEdit: boolean;
};
export type DeviceSettingsInput = Pick<DeviceSettings, "name" | "location" | "version">;
export type RuntimeField = "volume" | "brightness";
export type ControlInput = { field: RuntimeField; value: number; boot: string };
export type DeviceRuntime = {
  deviceId: string; connection: string; seenAt: string; boot: string;
  volume: number | null; brightness: number | null; canControl: boolean;
  command: {id:string;field:RuntimeField;value:number;status:string;actual:number|null;expiresAt?:string} | null;
};

export type AuditAction = "access" | "device_control" | "memory_clear" | "settings";
export type AuditItem = {
  id: string;
  time: string;
  actor: string;
  deviceId: string;
  target: string;
  action: AuditAction;
  description: string;
  status: "recorded" | "requested" | "confirmed" | "failed" | "unknown";
  result: string;
  scope: Scope;
  canOpenDevice: boolean;
  field?: string;
  value?: number;
  actual?: number;
};
export type AuditFilter = {
  device?: string;
  from?: string;
  to?: string;
  action?: AuditAction;
  offset?: number;
  limit?: number;
};
export type AuditPage = { items: AuditItem[]; hasMore: boolean; pending: boolean };

export type LibraryTrack = {
  id: number; title: string; artist: string; durationMs: number; bytes: number;
  format: "mp3" | "flac"; bitrate: number; sampleRate: number; channels: number; addedAt: string;
};
export type MusicLibrary = {
  deviceId: string; items: LibraryTrack[]; canManage: boolean;
  maxBytes: number; usedBytes: number; maxTrackBytes: number;
};
export type MusicUpload = { title: string; artist: string; personalUse: boolean; file: File };

import type {
  Scope,
  Device,
  Snapshot,
  Privacy,
  Member,
  Account,
  DeviceRuntime,
  ControlInput,
} from "./types.ts";
import {
  initialData,
  capabilities,
  expressions,
  tasks,
  firmware,
} from "./seed.ts";
export type DemoStorage = Pick<Storage, "getItem" | "setItem" | "removeItem">;

const storageKey = "companion-management-demo-v1";
const clone = <T>(value: T): T => structuredClone(value);
export class ApiError extends Error {
  code: string;
  constructor(code: string, message: string) {
    super(message);
    this.code = code;
  }
}
function fail(code: string, message: string): never {
  throw new ApiError(code, message);
}
function validName(name: string): string {
  if (typeof name !== "string" || /[\p{Cc}\p{Cf}]/u.test(name))
    fail("invalid_name", "名称不能包含控制字符");
  const trimmed = name.trim();
  if (!trimmed || [...trimmed].length > 12)
    fail("invalid_name", "名称需为1至12个字符");
  return trimmed;
}
function validPercent(value: unknown): value is number {
  return (
    typeof value === "number" &&
    Number.isInteger(value) &&
    value >= 0 &&
    value <= 100
  );
}
function validEmail(value: string): string {
  if (
    typeof value !== "string" ||
    !/^[a-z0-9.!#$%&'*+\/=?^_`{|}~-]+@example\.test$/i.test(value.trim())
  )
    fail("invalid_email", "请使用example.test域名的演示邮箱");
  return value.trim().toLowerCase();
}
function validCode(code: string) {
  if (!/^\d{6}$/.test(code)) fail("invalid_code", "请输入6位数字演示码");
}
function textFields(value: unknown, keys: string[]): boolean {
  return (
    value !== null &&
    typeof value === "object" &&
    keys.every(
      (key) => typeof (value as Record<string, unknown>)[key] === "string",
    )
  );
}
function validSnapshot(data: unknown): data is Snapshot {
  if (!data || typeof data !== "object") return false;
  const s = data as Snapshot;
  if (
    ![s.devices, s.conversations, s.memories, s.families, s.audit].every(
      Array.isArray,
    ) ||
    !s.privacy ||
    typeof s.privacy !== "object" ||
    !s.accounts ||
    typeof s.accounts !== "object" ||
    Array.isArray(s.accounts)
  )
    return false;
  try {
    for (const d of s.devices) {
      if (
        !textFields(d, [
          "id",
          "familyId",
          "name",
          "location",
          "model",
          "connection",
          "activity",
          "lastSeen",
        ]) ||
        !d.id ||
        !s.families.some((f) => f.id === d.familyId) ||
        validName(d.name) !== d.name ||
        !validPercent(d.volume) ||
        !validPercent(d.brightness) ||
        typeof d.revision !== "number" ||
        !Number.isInteger(d.revision) ||
        d.revision < 1 ||
        !(d.battery === null || validPercent(d.battery))
      )
        return false;
    }
    for (const c of s.conversations) {
      if (
        !textFields(c, [
          "id",
          "deviceId",
          "familyId",
          "title",
          "date",
          "time",
          "status",
          "endReason",
          "summary",
        ]) ||
        !s.devices.some(
          (d) => d.id === c.deviceId && d.familyId === c.familyId,
        ) ||
        typeof c.bodyAvailable !== "boolean" ||
        !Array.isArray(c.turns)
      )
        return false;
      for (const t of c.turns)
        if (
          !t ||
          typeof t !== "object" ||
          typeof t.id !== "string" ||
          typeof t.user !== "string" ||
          typeof t.played !== "string" ||
          typeof t.generated !== "string" ||
          !["played", "interrupted", "unknown"].includes(t.playback) ||
          typeof t.saved !== "boolean"
        )
          return false;
    }
    for (const m of s.memories)
      if (
        !textFields(m, ["id", "deviceId", "conversationId", "text", "date"]) ||
        !s.conversations.some(
          (c) => c.id === m.conversationId && c.deviceId === m.deviceId,
        )
      )
        return false;
    for (const f of s.families)
      if (
        !textFields(f, ["id", "name"]) ||
        !Array.isArray(f.members) ||
        !f.members.every((m) =>
          textFields(m, ["id", "name", "email", "role", "access", "status"]),
        )
      )
        return false;
    if (
      !s.audit.every((e) =>
        textFields(e, ["id", "time", "actor", "action", "target", "result"]),
      )
    )
      return false;
    for (const scope of ["admin", "family"] as const)
      if (
        s.accounts[scope] &&
        (!textFields(s.accounts[scope], ["email", "role", "scope"]) ||
          s.accounts[scope]!.scope !== scope)
      )
        return false;
    for (const d of s.devices) {
      const p = s.privacy[d.id];
      if (
        !p ||
        typeof p.keepRecords !== "boolean" ||
        typeof p.useMemory !== "boolean" ||
        !["7", "30", "90"].includes(p.retention) ||
        (!p.keepRecords && p.useMemory)
      )
        return false;
    }
  } catch {
    return false;
  }
  return true;
}

export function createMockApi(storage?: DemoStorage) {
  let state = initialData();
  let storageMode: "memory" | "session" = storage ? "session" : "memory";
  let storageWarning = "";
  if (storage) {
    try {
      const saved = storage.getItem(storageKey);
      if (saved) {
        const envelope = JSON.parse(saved);
        if (envelope.version === 1 && validSnapshot(envelope.data))
          state = envelope.data;
        else {
          storageWarning = "旧演示数据不兼容，已恢复初始样例。";
          storage.removeItem(storageKey);
        }
      }
    } catch {
      storageMode = "memory";
      storageWarning = "浏览器存储不可用，当前使用内存样例。";
    }
  }
  function persist() {
    if (storageMode !== "session" || !storage) return;
    try {
      storage.setItem(storageKey, JSON.stringify({ version: 1, data: state }));
    } catch {
      storageMode = "memory";
      storageWarning = "浏览器无法保存演示变更；刷新可能恢复之前的样例。";
    }
  }
  function findDevice(id: string, scope: Scope = "admin"): Device {
    const d = state.devices.find((item) => item.id === id);
    if (!d) return fail("not_found", "未找到这台设备");
    if (scope === "family" && d.familyId !== "family-lin")
      return fail("forbidden", "当前家庭未授权访问这台设备");
    return d;
  }
  function audit(action: string, target: string) {
    state.audit.unshift({
      id: `audit-demo-${Date.now()}-${state.audit.length}`,
      time: "刚刚 · 演示",
      actor: "演示用户",
      action,
      target,
      result: "演示回执",
    });
  }
  function findFamily(id: string, scope: Scope = "admin") {
    const f = state.families.find((item) => item.id === id);
    if (!f) return fail("not_found", "未找到这个家庭");
    if (scope === "family" && id !== "family-lin")
      return fail("forbidden", "当前账号未授权访问这个家庭");
    return f;
  }
  function findConversation(
    id: string,
    scope: Scope = "admin",
    expectedDeviceId?: string,
  ) {
    if (expectedDeviceId !== undefined) findDevice(expectedDeviceId, scope);
    const c = state.conversations.find((item) => item.id === id);
    if (!c) return fail("not_found", "未找到这段聊天");
    findDevice(c.deviceId, scope);
    if (expectedDeviceId !== undefined && c.deviceId !== expectedDeviceId)
      fail("not_found", "这段聊天不属于所选设备");
    if (!c.bodyAvailable) return fail("forbidden", "这段聊天正文未授权查看");
    return c;
  }
  function conversationList(filter: {
    deviceId?: string;
    query?: string;
    scope?: Scope;
  }): Snapshot["conversations"] {
    if (filter.deviceId !== undefined)
      findDevice(filter.deviceId, filter.scope);
    return state.conversations
      .filter(
        (c) =>
          (filter.deviceId === undefined || c.deviceId === filter.deviceId) &&
          (filter.scope !== "family" || c.familyId === "family-lin") &&
          (!filter.query ||
            c.title.includes(filter.query) ||
            c.summary.includes(filter.query)),
      )
      .map((c) => ({ ...c, turns: [] }));
  }
  return {
    mode: "demo" as const,
    get storageMode() {
      return storageMode;
    },
    get storageWarning() {
      return storageWarning;
    },
    async listDevices(scope: Scope = "admin") {
      return clone(
        state.devices.filter(
          (d) => scope === "admin" || d.familyId === "family-lin",
        ),
      );
    },
    async getDevice(id: string, scope: Scope = "admin") {
      return clone(findDevice(id, scope));
    },
    async getDeviceRuntime(id: string, scope: Scope = "admin"): Promise<DeviceRuntime> {
      const d = findDevice(id, scope);
      return {deviceId:id, connection:d.connection, seenAt:d.lastSeen, boot:"demo", volume:d.volume,brightness:d.brightness,canControl:false,command:null};
    },
    async controlDevice(_id:string, _value:ControlInput, _scope:Scope="admin"):Promise<DeviceRuntime> {
      return fail("demo_only", "请使用演示设置，不会控制真实设备。");
    },
    async updateDevice(
      id: string,
      value: Pick<Device, "name" | "volume" | "brightness">,
      scope: Scope = "admin",
    ) {
      const d = findDevice(id, scope),
        name = validName(value.name);
      if (!validPercent(value.volume) || !validPercent(value.brightness))
        fail("invalid_value", "音量和亮度需为0至100的整数");
      Object.assign(d, {
        name,
        volume: value.volume,
        brightness: value.brightness,
        revision: (d.revision ?? 0) + 1,
      });
      audit("配置变更", d.name);
      persist();
      return clone(d);
    },
    async listConversations(
      filter: { deviceId?: string; query?: string; scope?: Scope } = {},
    ) {
      return clone(conversationList(filter));
    },
    async listConversationPage(
      filter: {
        deviceId?: string;
        query?: string;
        scope?: Scope;
        offset?: number;
        limit?: number;
      } = {},
    ) {
      const offset = filter.offset ?? 0,
        limit = filter.limit ?? 50;
      if (
        !Number.isInteger(offset) ||
        offset < 0 ||
        offset > 10000 ||
        !Number.isInteger(limit) ||
        limit < 1 ||
        limit > 100
      )
        fail("invalid_value", "分页参数无效");
      const records = conversationList(filter);
      return {
        items: clone(records.slice(offset, offset + limit)),
        hasMore: offset + limit < records.length,
      };
    },
    async getConversation(
      id: string,
      scope: Scope = "admin",
      expectedDeviceId?: string,
    ) {
      return clone(findConversation(id, scope, expectedDeviceId));
    },
    async deleteConversation(
      id: string,
      scope: Scope = "admin",
      expectedDeviceId?: string,
    ) {
      const c = findConversation(id, scope, expectedDeviceId);
      state.conversations = state.conversations.filter(
        (item) => item.id !== c.id,
      );
      state.memories = state.memories.filter(
        (item) => item.conversationId !== c.id,
      );
      audit("删除聊天演示", c.title);
      persist();
    },
    async listFamilies(scope: Scope = "admin") {
      return clone(
        state.families.filter(
          (f) => scope === "admin" || f.id === "family-lin",
        ),
      );
    },
    async getFamily(id: string, scope: Scope = "admin") {
      return clone(findFamily(id, scope));
    },
    async inviteMember(
      familyId: string,
      value: { email: string; role: string; allowHistory: boolean },
      scope: Scope = "admin",
    ) {
      const f = findFamily(familyId, scope),
        email = validEmail(value.email);
      if (!["家庭成员", "家庭管理员"].includes(value.role))
        fail("invalid_role", "请选择有效的示例角色");
      if (f.members.some((m) => m.email === email))
        fail("already_exists", "此演示成员或邀请已存在");
      const member: Member = {
        id: `member-demo-${f.members.length}-${Date.now()}`,
        name: email.split("@")[0]!,
        email,
        role: value.role,
        access: value.allowHistory ? "本家庭设备" : "未授权正文",
        status: "待加入示例",
      };
      f.members.push(member);
      audit("邀请成员演示", f.name);
      persist();
      return clone(member);
    },
    async joinFamily(code: string) {
      validCode(code);
      audit("加入家庭演示", "林的家");
      persist();
      return clone(findFamily("family-lin", "family"));
    },
    async bindDevice(
      value: { familyId: string; code: string; name: string; model: string },
      scope: Scope = "admin",
    ) {
      const f = findFamily(value.familyId, scope);
      validCode(value.code);
      const name = validName(value.name),
        id = `dev-demo-${value.code}`;
      if (!["微雪 ESP32-S3", "M5 ESP32-S3"].includes(value.model))
        fail("invalid_model", "请选择受支持的ESP32-S3示例型号");
      if (state.devices.some((d) => d.id === id))
        fail("already_bound", "此演示码已绑定，请换一个示例码");
      const d: Device = {
        id,
        familyId: f.id,
        name,
        model: value.model,
        location: "待设置",
        connection: "未上报",
        activity: "未知",
        lastSeen: "暂无上报",
        battery: null,
        volume: 50,
        brightness: 50,
        revision: 1,
      };
      state.devices.push(d);
      state.privacy[id] = {
        keepRecords: true,
        useMemory: true,
        retention: "30",
      };
      audit("绑定设备演示", name);
      persist();
      return clone(d);
    },
    async updateMemberAccess(
      familyId: string,
      memberId: string,
      access: string,
      scope: Scope = "admin",
    ) {
      const f = findFamily(familyId, scope),
        m = f.members.find((item) => item.id === memberId);
      if (!m) return fail("not_found", "未找到这个成员");
      if (!["未授权正文", "客厅机器人", "本家庭设备"].includes(access))
        fail("invalid_access", "请选择有效的示例查看范围");
      if (
        access === "客厅机器人" &&
        !state.devices.some((d) => d.familyId === f.id && d.location === "客厅")
      )
        fail("invalid_access", "该家庭没有客厅设备");
      m.access = access;
      audit("修改查看范围演示", m.name);
      persist();
      return clone(f);
    },
    async listMemories(filter: { deviceId?: string; scope?: Scope } = {}) {
      if (filter.deviceId !== undefined)
        findDevice(filter.deviceId, filter.scope);
      return clone(
        state.memories.filter(
          (m) =>
            (filter.deviceId === undefined || m.deviceId === filter.deviceId) &&
            (filter.scope !== "family" ||
              state.devices.some(
                (d) => d.id === m.deviceId && d.familyId === "family-lin",
              )),
        ),
      );
    },
    async getMemory(
      id: string,
      scope: Scope = "admin",
      expectedDeviceId?: string,
    ) {
      const m = state.memories.find((item) => item.id === id);
      if (!m) return fail("not_found", "未找到这条记忆");
      if (expectedDeviceId !== undefined && m.deviceId !== expectedDeviceId)
        return fail("device_mismatch", "这条记忆不属于所选设备。");
      findConversation(m.conversationId, scope, m.deviceId);
      return clone(m);
    },
    async clearMemories(deviceId: string, scope: Scope = "admin") {
      const d = findDevice(deviceId, scope);
      state.memories = state.memories.filter((m) => m.deviceId !== d.id);
      audit("清除记忆演示", d.name);
      persist();
    },
    async getPrivacy(deviceId: string, scope: Scope = "admin") {
      findDevice(deviceId, scope);
      return clone(state.privacy[deviceId]!);
    },
    async updatePrivacy(
      deviceId: string,
      value: Privacy,
      scope: Scope = "admin",
    ) {
      const d = findDevice(deviceId, scope);
      if (
        typeof value.keepRecords !== "boolean" ||
        typeof value.useMemory !== "boolean" ||
        !["7", "30", "90"].includes(value.retention)
      )
        fail("invalid_privacy", "请选择有效的数据偏好");
      const result = {
        keepRecords: value.keepRecords,
        useMemory: value.keepRecords && value.useMemory,
        retention: value.retention,
      };
      state.privacy[d.id] = result;
      audit("保存隐私偏好演示", d.name);
      persist();
      return clone(result);
    },
    async login(scope: Scope, email: string, secret: string) {
      const normalized = validEmail(email);
      if (scope === "family") validCode(secret);
      else if (typeof secret !== "string" || !secret.trim())
        fail("invalid_login", "请输入非空演示密码");
      const account: Account = {
        email: normalized,
        role: scope === "admin" ? "内部演示管理员" : "家庭演示管理员",
        scope,
      };
      state.accounts[scope] = account;
      audit("登录演示", normalized);
      persist();
      return clone(account);
    },
    async logout(scope: Scope) {
      delete state.accounts[scope];
      persist();
    },
    async getAccount(scope: Scope) {
      return clone(state.accounts[scope] ?? null);
    },
    async listAccounts() {
      return [
        {
          name: "陈管理员",
          email: "admin@example.test",
          role: "系统管理员",
          access: "工作权限不等于家庭正文授权",
        },
        {
          name: "产品同事",
          email: "product@example.test",
          role: "产品观察员",
          access: "仅已授权会话",
        },
        {
          name: "运维同事",
          email: "ops@example.test",
          role: "运维人员",
          access: "默认无正文权限",
        },
      ];
    },
    async getDailySummaries(scope: Scope = "admin", deviceId?: string) {
      const chats = conversationList({ scope, deviceId });
      return clone(
        [...new Set(chats.map((c) => c.date))]
          .sort()
          .reverse()
          .map((date) => ({
            date,
            conversations: chats.filter((c) => c.date === date),
          })),
      );
    },
    async listCapabilities() {
      return clone(capabilities);
    },
    async listExpressions() {
      return clone(expressions);
    },
    async listTasks() {
      return clone(tasks);
    },
    async listFirmware() {
      return clone(firmware);
    },
    async listAudit(query = "") {
      const term = query.trim().toLowerCase();
      return clone(
        state.audit.filter((e) =>
          [e.time, e.actor, e.action, e.target, e.result]
            .join(" ")
            .toLowerCase()
            .includes(term),
        ),
      );
    },
    async getOperations() {
      return {
        metrics: [
          {
            label: "设备示例",
            value: String(state.devices.length),
            description: "虚构设备数量，非实时在线数",
          },
          {
            label: "聊天示例",
            value: String(state.conversations.length),
            description: "当前浏览器示例记录",
          },
          {
            label: "正式服务连接",
            value: "未接入",
            description: "本演示不连接语音或管理服务",
          },
          {
            label: "模型用量",
            value: "未上报",
            description: "没有实际供应商usage数据",
          },
        ],
        events: state.audit
          .slice(0, 8)
          .map((e) => ({ time: e.time, label: e.action, status: e.result })),
      };
    },
    async reset() {
      state = initialData();
      persist();
    },
  };
}
export type MockApi = ReturnType<typeof createMockApi>;

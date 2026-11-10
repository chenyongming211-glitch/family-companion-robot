import { validateRuntime, runtimeView, commandOutcome, level, sliderLevel } from "./runtime.ts";
import { Api, ClientError } from "./api.ts";
import { familySession, deviceID, chatID } from "./family.ts";
type Kind = "home" | "device" | "chats" | "chat" | "settings";
function localTime(value: unknown): string {
  if (typeof value !== "string" || !value) return "未上报";
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return "未上报";
  const two = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${two(date.getMonth() + 1)}-${two(date.getDate())} ${two(date.getHours())}:${two(date.getMinutes())}`;
}
function displayStatus(value: unknown): string {
  const labels: Record<string, string> = {
    played: "已播放",
    interrupted: "播放被打断",
    unknown: "状态不确定",
    pending: "记录更新中",
    active: "进行中",
    archived: "已归档",
    closed: "已结束",
    completed: "已完成",
    stopped: "已停止",
    cancelled: "已取消",
    failed: "播放失败",
    not_played: "尚未播放",
    generated: "已生成",
  };
  if (typeof value !== "string" || !value) return "未上报";
  return (
    labels[value] ||
    (/^[\u3400-\u9fff\s，。；：、]+$/.test(value) ? value : "状态未确认")
  );
}
function displayBoundary(value: unknown): string {
  const labels: Record<string, string> = {
    complete_response: "完整回复已播放，停止位置已确认",
    no_recognition: "没有语音识别证据，停止位置不确定",
    recognition_gap: "语音识别记录有缺口，停止位置不确定",
    invalid_timing: "识别时间信息无效，停止位置不确定",
    playback_guard: "已保守保留播放尾部，停止位置不确定",
    context_limit: "识别证据达到长度限制，停止位置不确定",
    conservative_end: "依据识别证据保守截取，停止位置不确定",
    unknown_playback: "缺少可信播放证据，停止位置不确定",
    exact: "停止位置已确认",
    exact_stop: "停止位置已确认",
    unknown: "停止位置边界不确定",
    unknown_stop: "停止位置边界不确定",
    played: "已有设备播放证据",
    generated_only: "仅有生成稿，不能确认播放",
    interrupted: "播放被打断，停止位置未确认",
  };
  if (typeof value !== "string" || !value) return "";
  return (
    labels[value] ||
    (/^[\u3400-\u9fff\s，。；：、]+$/.test(value) ? value : "播放边界尚未确认")
  );
}
function chatView(c: any): any {
  return {
    ...c,
    status: displayStatus(c.status),
    turns: Array.isArray(c.turns)
      ? c.turns.map((t: any) => ({
          ...t,
          playback: displayStatus(t.playback),
          boundary: displayBoundary(t.boundary),
        }))
      : [],
  };
}
function invalid(): never {
  throw new ClientError("invalid_response", "服务响应与当前设备不匹配");
}
export function familyPage(kind: Kind): any {
  return {
    data: {
      loading: false,
      saving: false,
      error: "",
      notice: "",
      username: "",
      password: "",
      loggedIn: false,
      families: [],
      familyIndex: -1,
      familyNames: [],
      devices: [],
      device: null,
      items: [],
      chat: null,
      hasMore: false,
      pending: false,
      name: "",
      location: "",
      settings: null,
      runtime: null, volumeDraft: 50, brightnessDraft: 50, controlling: false, controlNotice: "",
      runtimeTicks: [0,10,20,30,40,50,60,70,80,90,100],
    },
    epoch: 0,
    active: false,
    loginBusy: false,
    deviceId: "",
    chatId: "",
    api: undefined as Api | undefined,
    onLoad(this: any, query: any) {
      this.api = familySession(wx);
      this.routeError = "";
      try {
        if (kind !== "home") this.deviceId = deviceID(query.device);
        if (kind === "chat") this.chatId = chatID(query.chat);
      } catch (e: any) {
        this.routeError = e.message;
      }
      this.unsubscribe = this.api.onChange(() => {
        this.clearPrivate();
        this.setData({
          loggedIn: false,
          families: [],
          devices: [],
          familyIndex: -1,
        });
      });
      this.unauthorized = this.api.onUnauthorized((e: any) =>
        this.setData({ error: e.message, loggedIn: false }),
      );
    },
    clearPrivate(this: any) {
      this.stopRuntime();
      this.epoch++;
      this.setData({
        loading: false,
        saving: false,
        runtime: null, controlling: false, controlNotice: "", devices: [], notice: "", error: "",
        device: null,
        items: [],
        chat: null,
        settings: null,
        name: "",
        location: "",
        password: "",
        hasMore: false,
        pending: false,
      });
    },
    async onShow(this: any) {
      this.active = true;
      return this.refresh();
    },
    onHide(this: any) {
      this.active = false;
      this.api?.invalidatePending();
      this.clearPrivate();
    },
    onUnload(this: any) {
      this.onHide();
      this.unsubscribe?.();
      this.unauthorized?.();
    },
    input(this: any, e: any) {
      const field = e.currentTarget.dataset.field;
      if (["username", "password", "name", "location"].includes(field))
        this.setData({ [field]: e.detail.value });
    },
    async login(this: any) {
      if (this.loginBusy || this.data.loading) return;
      this.loginBusy = true;
      const username = this.data.username;
      const password = this.data.password;
      this.clearPrivate();
      this.setData({ loading: true, error: "" });
      const epoch = this.epoch;
      try {
        await this.api.login(username, password);
        if (!this.active) return;
        this.setData({ loading: false });
        await this.refresh();
      } catch (e: any) {
        if (this.active) this.setData({ error: e.message });
      } finally {
        this.loginBusy = false;
        if (this.active) this.setData({ loading: false, password: "" });
      }
    },
    async logout(this: any) {
      const pending = this.api.request("/logout", "POST", {});
      this.api.clear();
      this.setData({ loggedIn: false, error: "", devices: [], families: [] });
      try {
        await pending;
      } catch {}
    },
    async refresh(this: any) {
      if (this.data.loading) return;
      this.clearPrivate();
      const epoch = this.epoch;
      this.setData({ loading: true, error: "", notice: "" });
      try {
        if (this.routeError) throw new Error(this.routeError);
        if (kind === "home") {
          const families: any = await this.api.families();
          if (!Array.isArray(families)) invalid();
          const devices: any = await this.api.request("/devices");
          if (!Array.isArray(devices)) invalid();
          for (const d of devices) {
            deviceID(d.id);
            if (!families.some((f: any) => f.id === d.familyId)) invalid();
          }
          if (this.active && epoch === this.epoch) {
            const selected = this.selectedFamily;
            const index = families.findIndex((f: any) => f.id === selected);
            const i = index >= 0 ? index : families.length === 1 ? 0 : -1;
            this.allDevices = devices.map((d: any) => ({
              ...d,
              lastSeen: localTime(d.lastSeen),
            }));
            this.setData({
              loggedIn: true,
              families,
              familyNames: families.map((f: any) => f.name),
              familyIndex: i,
              devices:
                i >= 0
                  ? this.allDevices.filter(
                      (d: any) => d.familyId === families[i].id,
                    )
                  : [],
            });
          }
        } else if (kind === "device") {
          const d: any = await this.api.request("/devices/" + this.deviceId);
          if (d.id !== this.deviceId) invalid();
          if (this.active && epoch === this.epoch)
            this.setData({
              device: { ...d, lastSeen: localTime(d.lastSeen) },
              loggedIn: true,
            });
        } else if (kind === "chat") {
          const c: any = await this.api.request(
            "/chats/" + this.chatId + "?device=" + this.deviceId,
          );
          if (c.id !== this.chatId || c.deviceId !== this.deviceId) invalid();
          if (this.active && epoch === this.epoch)
            this.setData({
              chat: chatView(c.bodyAvailable ? c : { ...c, turns: [] }),
              loggedIn: true,
            });
        } else if (kind === "settings") {
          await this.readSettings(epoch);
        } else {
          await this.loadChats(0, epoch);
        }
      } catch (e: any) {
        if (this.active && epoch === this.epoch)
          this.setData({
            error: e.message,
            device: null,
            chat: null,
            items: [],
            settings: null,
          });
      } finally {
        if (this.active && epoch === this.epoch) {
          this.setData({ loading: false });
          if (["home", "device", "settings"].includes(kind) && this.data.loggedIn) { await this.refreshRuntime(); this.scheduleRuntime(); }
        }
      }
    },
    stopRuntime(this:any) {
      clearTimeout(this.runtimeTimer); clearTimeout(this.commandTimer); clearTimeout(this.commandDeadlineTimer);
      this.runtimeGeneration=(this.runtimeGeneration || 0)+1;
      this.runtimeTimer = undefined; this.commandTimer = undefined; this.commandDeadlineTimer = undefined;
      this.pendingCommand = null; this.runtimeReading = false;
    },
    scheduleRuntime(this:any) {
      clearTimeout(this.runtimeTimer);
      if (this.active && this.data.loggedIn) this.runtimeTimer = setTimeout(async()=>{ await this.refreshRuntime(); this.scheduleRuntime(); },10000);
      this.runtimeTimer?.unref?.();
    },
    async refreshRuntime(this:any) {
      if (!this.active || !this.data.loggedIn || this.runtimeReading || this.data.controlling) return;
      const epoch=this.epoch, generation=this.runtimeGeneration=(this.runtimeGeneration || 0)+1; this.runtimeReading=true;
      try {
        if(kind==='home') {
          const devices=await Promise.all((this.allDevices || []).map(async(d:any)=>({...d,runtime:runtimeView(validateRuntime(await this.api.request('/device-runtime/'+d.id),d.id))})));
          if(this.active && epoch===this.epoch && generation===this.runtimeGeneration) {
            this.allDevices=devices;
            const f=this.data.families[this.data.familyIndex];
            this.setData({devices:f ? devices.filter((d:any)=>d.familyId===f.id):[]});
          }
        } else {
          const r=validateRuntime(await this.api.request('/device-runtime/'+this.deviceId),this.deviceId);
          if(this.active && epoch===this.epoch && generation===this.runtimeGeneration) this.setData({runtime:runtimeView(r)});
        }
      } catch(e:any) {
        if(this.active && epoch===this.epoch && generation===this.runtimeGeneration) {
          this.setData({runtime:null,error:e.message});
          if(kind==='home') { this.allDevices=(this.allDevices||[]).map((d:any)=>({...d,runtime:null})); const f=this.data.families[this.data.familyIndex]; this.setData({devices:f ? this.allDevices.filter((d:any)=>d.familyId===f.id):[]}); }
        }
      } finally { if(epoch===this.epoch) this.runtimeReading=false; }
    },
    canControl(this:any,field:string) {
      const r=this.data.runtime;
      return kind==='settings' && this.active && this.data.loggedIn && !this.data.loading && !this.data.controlling && ['volume','brightness'].includes(field) && r && r.canControl && r.connection==='online' && level(r[field]);
    },
    runtimeInput(this:any,e:any) {
      const field=e.currentTarget.dataset.field, value=sliderLevel(e.detail.value);
      if(this.canControl(field) && value!==null) this.setData({[field+'Draft']:value});
    },
    async runtimeCommit(this:any,e:any) {
      const field=e.currentTarget.dataset.field, value=sliderLevel(e.detail.value);
      if(!this.canControl(field) || value===null) return;
      this.setData({[field+'Draft']:value});
      await this.control(e);
    },
    async control(this:any,e:any) {
      const field=e.currentTarget.dataset.field, r=this.data.runtime, value=this.data[field+'Draft'];
      if(!this.canControl(field) || !level(value)) return;
      const label=field==='volume' ? '音量' : '亮度';
      if(r[field]===value) { this.setData({controlNotice:label+'当前已是 '+value+'，无需设置'}); return; }
      const epoch=this.epoch;
      this.runtimeGeneration=(this.runtimeGeneration || 0)+1;
      this.setData({controlling:true,controlNotice:'正在设置'+label+'为 '+value+'，等待设备回执',error:''});
      try {
        const result=validateRuntime(await this.api.request('/device-runtime/'+this.deviceId,'PUT',{field,value,boot:r.boot}),this.deviceId);
        if(!this.active || epoch!==this.epoch) return;
        const c=result.command;
        if(!c || c.field!==field || c.value!==value) throw new Error('结果未确认，请刷新状态');
        this.pendingCommand={id:c.id,field,value}; this.commandDeadline=Date.now()+50000;
        this.commandDeadlineTimer=setTimeout(()=>{
          if(!this.active || epoch!==this.epoch || !this.pendingCommand) return;
          this.pendingCommand=null;
          this.runtimeGeneration=(this.runtimeGeneration || 0)+1;
          clearTimeout(this.commandTimer); this.commandTimer=undefined; this.commandDeadlineTimer=undefined;
          this.setData({controlling:false,controlNotice:label+'：结果未确认，请刷新状态'});
          void this.refreshRuntime();
        },50000);
        this.commandDeadlineTimer?.unref?.();
        this.setData({runtime:runtimeView(result)});
        await this.observeCommand(epoch);
      } catch(e:any) {
        if(this.active && epoch===this.epoch) { this.setData({controlling:false,controlNotice:label+'：'+(e.code==='network_error' ? '提交结果未确认，请刷新状态；请勿重复提交' : e.message)}); await this.refreshRuntime(); }
      }
    },
    async observeCommand(this:any,epoch:number) {
      if(!this.active || epoch!==this.epoch || !this.pendingCommand) return;
      const p=this.pendingCommand, generation=this.runtimeGeneration=(this.runtimeGeneration || 0)+1;
      const label=p.field==='volume' ? '音量' : '亮度';
      try {
        const r=validateRuntime(await this.api.request('/device-runtime/'+this.deviceId),this.deviceId);
        if(!this.active || epoch!==this.epoch || p!==this.pendingCommand || generation!==this.runtimeGeneration) return;
        const message=Date.now() >= this.commandDeadline ? '结果未确认，请刷新状态' : commandOutcome(r.command,p);
        const waiting=message==='等待设备回执' && Date.now()<this.commandDeadline;
        this.setData({runtime:runtimeView(r),controlNotice:label+'：'+(waiting ? message : message==='等待设备回执' ? '结果未确认，请刷新状态' : message),controlling:waiting});
        if(waiting) { this.commandTimer=setTimeout(()=>this.observeCommand(epoch),2000); this.commandTimer?.unref?.(); }
        else { this.pendingCommand=null; clearTimeout(this.commandDeadlineTimer); this.commandDeadlineTimer=undefined; }
      } catch(e:any) {
        if(this.active && epoch===this.epoch && p===this.pendingCommand && generation===this.runtimeGeneration) { this.pendingCommand=null; clearTimeout(this.commandDeadlineTimer); this.commandDeadlineTimer=undefined; this.setData({controlling:false,controlNotice:label+'：结果未确认，请刷新状态',runtime:null}); }
      }
    },
    chooseFamily(this: any, e: any) {
      const i = Number(e.detail.value);
      const f = this.data.families[i];
      if (!f) return;
      this.selectedFamily = f.id;
      this.setData({
        familyIndex: i,
        devices: this.allDevices.filter((d: any) => d.familyId === f.id),
      });
    },
    async loadChats(this: any, offset: number, epoch: number) {
      const p: any = await this.api.request(
        "/chats?device=" + this.deviceId + "&offset=" + offset + "&limit=20",
      );
      if (
        !Array.isArray(p.items) ||
        p.items.length > 20 ||
        typeof p.hasMore !== "boolean"
      )
        invalid();
      for (const c of p.items) {
        chatID(c.id);
        if (c.deviceId !== this.deviceId) invalid();
      }
      if (this.active && epoch === this.epoch)
        this.setData({
          items: offset
            ? [...this.data.items, ...p.items.map(chatView)]
            : p.items.map(chatView),
          hasMore: p.hasMore,
          pending: !!p.pendingSources,
        });
    },
    async more(this: any) {
      if (this.data.loading || !this.data.hasMore) return;
      const epoch = this.epoch;
      this.setData({ loading: true, error: "" });
      try {
        await this.loadChats(this.data.items.length, epoch);
      } catch (e: any) {
        if (this.active && epoch === this.epoch)
          this.setData({ items: [], hasMore: false, error: e.message });
      } finally {
        if (this.active && epoch === this.epoch)
          this.setData({ loading: false });
      }
    },
    async readSettings(this: any, epoch: number) {
      const s: any = await this.api.request(
        "/device-settings/" + this.deviceId,
      );
      if (
        s.deviceId !== this.deviceId ||
        typeof s.name !== "string" ||
        typeof s.location !== "string" ||
        typeof s.canEdit !== "boolean" ||
        !/^[a-f0-9]{64}$/.test(s.version)
      )
        invalid();
      if (this.active && epoch === this.epoch)
        this.setData({
          settings: s,
          name: s.name,
          location: s.location,
          loggedIn: true,
        });
      return s;
    },
    async save(this: any) {
      if (this.data.saving || this.data.loading || !this.data.settings?.canEdit)
        return;
      const name = this.data.name.trim(),
        location = this.data.location.trim();
      if (
        [...name].length < 1 ||
        [...name].length > 12 ||
        [...location].length > 24 ||
        /[\x00-\x1f\x7f\u2028\u2029]/.test(name + location)
      ) {
        this.setData({ error: "昵称1至12字，位置最多24字，不得包含控制字符" });
        return;
      }
      const epoch = this.epoch;
      this.setData({ saving: true, error: "", notice: "" });
      try {
        const s: any = await this.api.request(
          "/device-settings/" + this.deviceId,
          "PUT",
          { name, location, version: this.data.settings.version },
        );
        if (
          s.deviceId !== this.deviceId ||
          s.name !== name ||
          s.location !== location ||
          !/^[a-f0-9]{64}$/.test(s.version)
        )
          throw new ClientError(
            "unconfirmed",
            "云端返回与提交不一致，保存结果未确认，请重新读取",
          );
        const confirmed = await this.readSettings(epoch);
        if (
          confirmed.name !== name ||
          confirmed.location !== location ||
          confirmed.version !== s.version
        )
          throw new ClientError(
            "unconfirmed",
            "云端设置已变化，未确认提交值生效，请核对当前内容",
          );
        if (this.active && epoch === this.epoch)
          this.setData({ notice: "已保存并重新读取云端展示设置" });
      } catch (e: any) {
        if (this.active && epoch === this.epoch) {
          this.setData({ error: e.message });
          if (e.code === "conflict" || e.code === "unconfirmed") {
            try {
              await this.readSettings(epoch);
            } catch {
              if (this.active && epoch === this.epoch)
                this.setData({ settings: null });
            }
          }
        }
      } finally {
        if (this.active && epoch === this.epoch)
          this.setData({ saving: false });
      }
    },
    openDevice(this: any, e: any) {
      try {
        wx.navigateTo({
          url:
            "/pages/device/device?device=" +
            deviceID(e.currentTarget.dataset.id),
        });
      } catch {}
    },
    openChat(this: any, e: any) {
      try {
        wx.navigateTo({
          url:
            "/pages/chat/chat?device=" +
            deviceID(this.deviceId) +
            "&chat=" +
            chatID(e.currentTarget.dataset.id),
        });
      } catch {}
    },
    chats(this: any) {
      wx.navigateTo({ url: "/pages/chats/chats?device=" + this.deviceId });
    },
    settings(this: any) {
      wx.navigateTo({
        url: "/pages/settings/settings?device=" + this.deviceId,
      });
    },
    add() {
      wx.navigateTo({ url: "/pages/index/index" });
    },
    home() {
      wx.reLaunch({ url: "/pages/home/home" });
    },
  };
}

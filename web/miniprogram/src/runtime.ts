import { ClientError } from './api.ts';
export type Field = 'volume' | 'brightness';
export type Runtime = {deviceId:string;connection:'online'|'offline'|'unknown';seenAt:string;boot:string;volume:number|null;brightness:number|null;canControl:boolean;command:any};
export const level = (v:unknown):v is number => typeof v==='number' && Number.isInteger(v) && v>=0 && v<=100;
const date = (v:unknown) => typeof v==='string' && /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)$/.test(v) && Number.isFinite(Date.parse(v));
export function validateRuntime(v:any,id:string):Runtime {
 const bad=()=>{throw new ClientError('invalid_response','设备状态响应无效，请重新读取');};
 if(!v || v.deviceId!==id || !['online','offline','unknown'].includes(v.connection) || typeof v.canControl!=='boolean' || !(v.seenAt==='' || date(v.seenAt)) || typeof v.boot!=='string' || !(v.boot==='' || /^[a-f0-9]{32}$/.test(v.boot)) || ![v.volume,v.brightness].every(n=>n===null || level(n))) bad();
 if(v.connection==='online' && (!v.boot || !v.seenAt)) bad();
 if(v.connection!=='online' && (v.volume!==null || v.brightness!==null)) bad();
 if(v.command!==null) {
 const c=v.command;
 if(!c || !/^cmd_[a-f0-9]{32}$/.test(c.id) || !['volume','brightness'].includes(c.field) || !level(c.value) || !['queued','sent','confirmed','failed','unknown'].includes(c.status) || !(c.actual===null || level(c.actual)) || !date(c.expiresAt)) bad();
 }
 return v;
}
export function commandOutcome(c:any,p:any):string {
 if(!c || c.id!==p.id || c.field!==p.field || c.value!==p.value) return '结果未确认，请刷新状态';
 if(c.status==='confirmed' && c.actual===p.value) return '已生效，实际值 '+c.actual;
 if(c.status==='queued' || c.status==='sent') return '等待设备回执';
 return c.status==='failed' ? '设备执行失败，请核对实际值' : '结果未确认，请刷新状态';
}
export function runtimeView(r:Runtime) {
 return {...r,connectionLabel:{online:'在线',offline:'离线',unknown:'尚未上报'}[r.connection],seenLabel:r.seenAt ? r.seenAt.replace('T',' ').replace(/(?:\.\d+)?Z$/,' UTC') : '未上报'};
}

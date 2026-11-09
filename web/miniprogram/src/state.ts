export type Phase='idle'|'connecting'|'wifi'|'applying'|'waiting_cloud'|'success'|'error';
export type State={phase:Phase;generation:number;deviceId?:string;error?:string};
export const initial:State={phase:'idle',generation:0};
export type Event={type:string;generation?:number;status?:string;deviceId?:string;error?:string};
export function reduce(state:State,event:Event):State {
 if(event.type==='start') return {phase:'connecting',generation:state.generation+1};
 if(event.type==='cancel') return {phase:'idle',generation:state.generation+1};
 if(event.generation!==state.generation || ['idle','success','error'].includes(state.phase)) return state;
 if(event.type==='error')return {...state,phase:'error',error:event.error||'操作失败，请重试'};
 if(event.type==='secure')return {...state,phase:'wifi'};
 if(event.type==='apply')return {...state,phase:'applying'};
 if(event.type==='ble_applied')return {...state,phase:'waiting_cloud'};
 if(event.type==='cloud_status') {
  if(event.status==='confirmed' && /^dev_[a-f0-9]{32}$/.test(event.deviceId||''))return {...state,phase:'success',deviceId:event.deviceId};
  if(event.status==='expired'||event.status==='cancelled')return {...state,phase:'error',error:event.status==='expired'?'认领已过期，请重新扫码':'添加已取消'};
 }
 return state;
}

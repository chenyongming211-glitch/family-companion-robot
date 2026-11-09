import {ClientError} from '../api.ts';
// Official network_provisioning proto3 wifi_config.proto / network_scan.proto wire format.
export const utf8=(s:string)=>new Uint8Array([...unescape(encodeURIComponent(s))].map(c=>c.charCodeAt(0)));
export const text=(b:Uint8Array)=>decodeURIComponent(escape(String.fromCharCode(...b)));
function integer(n:number){const a=[];do{a.push((n&127)|(n>127?128:0));n=Math.floor(n/128);}while(n);return a;}
export function field(n:number,v:number|Uint8Array):number[]{return typeof v==='number'?[...integer(n*8),...integer(v)]:[...integer(n*8+2),...integer(v.length),...v];}
export function message(...fields:number[][]):Uint8Array{return new Uint8Array(fields.flat());}
export function parse(data:Uint8Array):Map<number,Array<number|Uint8Array>>{
 if(data.length>8192)throw new ClientError('proto','设备响应过大');const result=new Map<number,Array<number|Uint8Array>>();let at=0;
 const read=(scalar=false)=>{
  // Keep two exact uint32 halves: negative int32 uses a sign-extended uint64
  // varint. Tags/lengths stay uint32; no BigInt is needed in the initial bundle.
  let low=0,high=0;
  for(let i=0;i<(scalar?10:5);i++){
   if(at>=data.length)throw new ClientError('proto','设备响应截断');const c=data[at++],bits=c&127;
   if((!scalar&&i===4&&bits>15)||(i===9&&bits>1))throw new ClientError('proto','设备响应无效');
   if(i<4)low+=bits*2**(7*i);else if(i===4){low+=(bits&15)*2**28;high=Math.floor(bits/16);}else high+=bits*2**(7*i-32);
   if(!(c&128)){
    if(high===4294967295&&low>=2147483648)return low-4294967296;
    const value=low+high*4294967296;if(!Number.isSafeInteger(value))throw new ClientError('proto','设备响应无效');return value;
   }
  }
  throw new ClientError('proto','设备响应无效');
 };
 while(at<data.length){const key=read(),number=key>>>3,wire=key&7;if(!number)throw new ClientError('proto','设备响应无效');let value:number|Uint8Array;if(wire===0)value=read(true);else if(wire===2){const length=read();if(at+length>data.length)throw new ClientError('proto','设备响应截断');value=data.slice(at,at+length);at+=length;}else throw new ClientError('proto','设备响应格式不兼容');const values=result.get(number)||[];values.push(value);result.set(number,values);}
 return result;
}
const num=(m:Map<number,Array<number|Uint8Array>>,n:number)=>{const v=m.get(n)?.[0];if(v===undefined)return 0;if(typeof v!=='number'||v<0||v>4294967295)throw new ClientError('proto','设备响应格式无效');return v;};
const bytes=(m:Map<number,Array<number|Uint8Array>>,n:number)=>{const v=m.get(n)?.[0];if(!(v instanceof Uint8Array))throw new ClientError('proto','设备响应缺失');return v;};
export type SecureExchange=(endpoint:string,payload:Uint8Array)=>Promise<Uint8Array>;
export class WifiProvisioner {
 private exchange:SecureExchange;constructor(exchange:SecureExchange){this.exchange=exchange;}
 private async config(msg:number,nested:number,body:Uint8Array){const m=parse(await this.exchange('prov-config',message(field(1,msg),field(nested,body))));if(num(m,1)!==msg+1)throw new ClientError('proto','设备响应类型错误');const result=parse(bytes(m,nested+1));if(num(result,1)!==0)throw new ClientError('wifi_config','设备拒绝了Wi-Fi配置');return result;}
 async apply(ssid:string,password:string){const name=utf8(ssid),pass=utf8(password);if(name.length===0||name.length>32||pass.length>64)throw new ClientError('wifi_input','Wi-Fi名称或密码长度无效');await this.config(2,12,message(field(1,name),field(2,pass)));await this.config(4,14,new Uint8Array());}
 async scan():Promise<string[]>{
  const start=parse(await this.exchange('prov-scan',message(field(1,0),field(10,message(field(1,1),field(2,0),field(3,0),field(4,120))))));
  if(num(start,1)!==1||num(start,2)!==0)throw new ClientError('wifi_scan','Wi-Fi扫描失败');
  const status=parse(await this.exchange('prov-scan',message(field(1,2),field(12,new Uint8Array()))));
  if(num(status,1)!==3)throw new ClientError('proto','Wi-Fi扫描响应错误');const s=parse(bytes(status,13));if(num(status,2)!==0||num(s,1)!==1)throw new ClientError('wifi_scan','Wi-Fi扫描未完成，请重试');const count=Math.min(num(s,2),32);
  const names:string[]=[];
  for(let index=0;index<count;index+=4){
   const result=parse(await this.exchange('prov-scan',message(field(1,4),field(14,message(field(1,index),field(2,Math.min(4,count-index)))))));
   if(num(result,1)!==5||num(result,2)!==0)throw new ClientError('wifi_scan','Wi-Fi扫描失败');const payload=parse(bytes(result,15));
   for(const item of payload.get(1)||[]){if(!(item instanceof Uint8Array))throw new ClientError('proto','Wi-Fi列表无效');const name=text(bytes(parse(item),1));if(name)names.push(name);}
  }
  return [...new Set(names)];
 }
}

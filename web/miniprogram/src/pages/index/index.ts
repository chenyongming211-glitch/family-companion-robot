import {Onboarding} from '../../controller.ts';
import {setCleanup} from '../../lifecycle.ts';
function errorMessage(e:unknown){return e instanceof Error?e.message:'操作失败，请重试';}
Page({
 data:{phase:'idle',error:'',loggedIn:false,scanned:false,username:'',loginPassword:'',families:[],familyIndex:0,familyNames:[],name:'',devices:[],networks:[],ssid:'',password:'',serial:'',model:'',deviceId:''},
 controller:undefined as Onboarding|undefined,
 onLoad(this:any,options:any){
  this.controller=new Onboarding(wx,(view)=>this.setData({loggedIn:view.families.length>0,phase:view.state.phase,error:view.state.error||'',deviceId:view.state.deviceId||'',families:view.families,familyNames:view.families.map(f=>f.name),devices:view.devices,networks:view.networks,serial:view.serial,model:view.model}));
  const cleanup=()=>{void this.controller?.leave();this.setData({password:'',loginPassword:'',loggedIn:false,scanned:false,ssid:'',name:''});};setCleanup(cleanup);this.cleanup=cleanup;
  if(options.scene){try{this.controller.setCode(decodeURIComponent(options.scene));this.setData({scanned:true});}catch(e){this.setData({error:errorMessage(e)});}}
 },
 onHide(this:any){this.cleanup?.();},onUnload(this:any){this.cleanup?.();setCleanup(undefined);},
 input(this:any,e:any){const key=e.currentTarget.dataset.key;if(['username','loginPassword','name','ssid','password'].includes(key))this.setData({[key]:e.detail.value});},
 family(this:any,e:any){this.setData({familyIndex:Number(e.detail.value)});},
 network(this:any,e:any){this.setData({ssid:this.data.networks[Number(e.detail.value)]||''});},
 async login(this:any){const password=this.data.loginPassword;this.setData({loginPassword:'',error:''});try{await this.controller.login(this.data.username,password);this.setData({loggedIn:true,familyIndex:0});}catch(e){this.setData({error:errorMessage(e)});}},
 scan(this:any){wx.scanCode({onlyFromCamera:true,scanType:['qrCode'],success:(r:any)=>{try{this.controller.setCode(r.result);this.setData({scanned:true,error:''});}catch(e){this.setData({error:errorMessage(e)});}},fail:()=>this.setData({error:'扫码未完成，请重试'})});},
 async start(this:any){this.setData({error:''});try{await this.controller.start(this.data.families[this.data.familyIndex]?.id,this.data.name);}catch(e){this.setData({error:errorMessage(e)});}},
 async connect(this:any,e:any){await this.controller.connect(e.currentTarget.dataset.id);},
 async refresh(this:any){await this.controller.scanWifi();},
 async apply(this:any){const password=this.data.password;this.setData({password:''});await this.controller.apply(this.data.ssid,password);},
 async cancel(this:any){this.setData({password:'',scanned:false,error:''});await this.controller.cancel();},
});

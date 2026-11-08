(function () {
  'use strict';
  var paths = {
    home:'M3 10 12 3l9 7M5 9v11h5v-6h4v6h5V9',
    grid:'M3 3h7v7H3zM14 3h7v7h-7zM3 14h7v7H3zM14 14h7v7h-7z',
    chat:'M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-2 2V11.5A8.5 8.5 0 0 1 10.5 3h2A8.5 8.5 0 0 1 21 11.5ZM7 9h9M7 13h6',
    people:'M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2M17 4a4 4 0 0 1 0 8M22 21v-2a4 4 0 0 0-3-3.87M13 7a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
    device:'M6 2h12a2 2 0 0 1 2 2v16a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2ZM9 18h6M8 6h8v7H8z',
    brain:'M9 18v3M15 18v3M8 5a4 4 0 0 1 8 0 4 4 0 0 1 3 7 4 4 0 0 1-4 6H9a4 4 0 0 1-4-6 4 4 0 0 1 3-7ZM12 7v9M8 10l4 3 4-3',
    face:'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0ZM8 9h.01M16 9h.01M8 14s1.5 3 4 3 4-3 4-3',
    mic:'M9 5a3 3 0 0 1 6 0v7a3 3 0 0 1-6 0V5ZM5 10v2a7 7 0 0 0 14 0v-2M12 19v3M8 22h8',
    tasks:'M8 4H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2V6a2 2 0 0 0-2-2h-3M8 2h8v4H8zM7 12l2 2 3-3M14 12h3M7 18h10',
    settings:'M9 3h6l1 3 3 1 2 5-2 5-3 1-1 3H9l-1-3-3-1-2-5 2-5 3-1 1-3ZM16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0Z',
    shield:'M12 2 3 6v6c0 5 9 10 9 10s9-5 9-10V6l-9-4ZM8 12l3 3 5-6',
    activity:'M2 12h4l3-9 6 18 3-9h4',
    file:'M14 2H5v20h14V7l-5-5ZM14 2v6h5M8 12h8M8 16h8',
    search:'M21 21l-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0Z',
    bell:'M18 8a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9ZM10 21h4',
    arrow:'M5 12h14M13 6l6 6-6 6',
    back:'M19 12H5M11 6l-6 6 6 6',
    chevron:'M9 5l7 7-7 7',
    down:'m6 9 6 6 6-6',
    check:'m5 12 4 4L19 6',
    clock:'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0ZM12 7v5l3 2',
    filter:'M3 4h18l-7 8v7l-4 2v-9L3 4Z',
    plus:'M12 4v16M4 12h16',
    close:'M6 6l12 12M18 6 6 18',
    more:'M4 12h.01M12 12h.01M20 12h.01',
    download:'M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5',
    info:'M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0ZM12 11v6M12 7h.01',
    link:'M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-2 2M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l2-2',
    logout:'M9 3H4v18h5M10 12h11m-5-5 5 5-5 5',
    menu:'M4 6h16M4 12h16M4 18h16',
    sound:'m3 9 5 0 5-5v16l-5-5H3V9ZM17 8a6 6 0 0 1 0 8M20 5a10 10 0 0 1 0 14',
    sun:'M16 12a4 4 0 1 1-8 0 4 4 0 0 1 8 0ZM12 2v2M12 20v2M2 12h2M20 12h2M5 5l1 1M18 18l1 1M5 19l1-1M18 6l1-1',
    calendar:'M4 5h16v17H4V5ZM4 10h16M8 2v6M16 2v6M8 14h2M14 14h2M8 18h2',
    lock:'M5 10h14v12H5V10ZM8 10V6a4 4 0 0 1 8 0v4M12 15v3',
    refresh:'M20 7V2m0 5h-5M4 17v5m0-5h5M20 7a9 9 0 0 0-16 2M4 17a9 9 0 0 0 16-2',
    chick:'M5 13c0-5 3-9 7-9s7 4 7 9c0 4-3 7-7 7s-7-3-7-7ZM8 10h.01M16 10h.01M10 14l2 2 2-2M10 4l1-2 2 2M5 12l-3 4M19 12l3 4',
    upload:'M12 16V3m-5 5 5-5 5 5M4 16v5h16v-5'
  };
  function esc(v) { return String(v == null ? '' : v).replace(/[&<>"']/g, function(c){return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];}); }
  function icon(name, extra) {return '<svg aria-hidden="true" class="icon '+(extra||'')+'" viewBox="0 0 24 24"><path d="'+(paths[name]||paths.grid)+'"/></svg>';}
  function badge(text, type, dot) {return '<span class="badge '+(type||'')+'">'+(dot?'<i class="dot"></i>':'')+esc(text)+'</span>';}
  function button(text, action, type, ico) {return '<button class="btn '+(type||'')+'" data-action="'+esc(action)+'">'+(ico?icon(ico):'')+esc(text)+'</button>';}
  function link(text, href, type, ico){return '<a class="btn '+(type||'')+'" href="'+esc(href)+'">'+(ico?icon(ico):'')+esc(text)+'</a>';}
  function notice(text,type){return '<div class="notice '+(type||'')+'">'+icon('info')+'<span>'+text+'</span></div>';}
  function empty(title, description, action){return '<div class="empty"><div class="mini-icon">'+icon('search')+'</div><h3>'+esc(title)+'</h3><p>'+esc(description)+'</p>'+(action||'')+'</div>';}
  function face(emotion, speaking){return '<svg class="chick '+esc(emotion||'neutral')+(speaking?' speaking':'')+'" aria-label="机器人表情示意" role="img" viewBox="0 0 120 120"><ellipse cx="60" cy="108" rx="33" ry="4" fill="#d2c1a6" opacity=".35"/><path d="M41 29c-2-10 4-14 11-10 4-13 12-13 16-3 9-6 15 1 11 13" fill="#df9e49"/><path d="M25 61C20 82 12 83 13 72s6-22 14-25M95 61c5 21 13 22 12 11s-6-22-14-25" fill="#e8ae52" stroke="#dab062" stroke-width="1.2"/><path d="M60 21c23 0 37 25 37 49 0 24-15 36-37 36S23 94 23 70c0-24 14-49 37-49Z" fill="#f8df9f" stroke="#e4c17b" stroke-width="1.3"/><rect x="29" y="39" width="62" height="48" rx="21" fill="#393b35"/><g stroke="#fff6d8" stroke-width="4.3" fill="none" stroke-linecap="round" class="blink"><path class="eyes-normal" d="M45 55v8M75 55v8"/><path class="eyes-happy" d="M40 60q5-10 10 0M70 60q5-10 10 0"/></g><g class="cheeks" fill="#f4b287"><ellipse cx="38" cy="69" rx="4" ry="2"/><ellipse cx="82" cy="69" rx="4" ry="2"/></g><ellipse class="mouth" cx="60" cy="71" rx="5" ry="2.8" fill="#f7cf7d"/><path d="M43 105v6h-8M77 105v6h8" fill="none" stroke="#b88743" stroke-width="3" stroke-linecap="round"/></svg>';}
  var saved={};try{saved=JSON.parse(sessionStorage.getItem('companion-prototype')||'{}');}catch(e){}
  var state=Object.assign({name:'团团',volume:55,brightness:65,family:'林的家',memory:true},saved);
  function save(){try{sessionStorage.setItem('companion-prototype',JSON.stringify(state));}catch(e){}}
  var adminPages=[['overview','工作台','home'],['families','家庭列表','people'],['family','家庭详情','people'],['devices','设备列表','device'],['device','设备详情','device'],['conversations','对话中心','chat'],['daily','每日摘要','calendar'],['memories','记忆管理','brain'],['capabilities','能力总览','grid'],['expressions','表情预览','face'],['voiceprints','声纹管理','mic'],['tasks','任务中心','tasks'],['configuration','产品配置','settings'],['policies','会话与数据策略','shield'],['firmware','固件版本','upload'],['operations','运行状态','activity'],['audit','操作审计','file'],['accounts','账号与角色','lock'],['login','内部登录','logout']];
  var familyPages=[['home','家庭首页','home'],['device','机器人详情','device'],['conversations','聊天记录','chat'],['conversation','聊天详情','chat'],['memories','设备记忆','brain'],['memory','记忆详情','brain'],['family','我的家庭','people'],['invite','邀请家人','people'],['bind','绑定机器人','link'],['settings','机器人设置','settings'],['privacy','数据与隐私','shield'],['login','家庭登录','logout']];
  var sessions=[
    {id:'run',title:'第一次跑完一公里',time:'14:32',date:'今天',duration:'2分钟',rounds:2,status:'已归档',type:'green',device:'团团',summary:'分享运动进步，讨论热身与拉伸。',end:'空闲30分钟',emotion:'开心',source:'语音结束后已保存'},
    {id:'story',title:'小狐狸和月亮的故事',time:'11:06',date:'今天',duration:'1分钟',rounds:2,status:'已结束',type:'',device:'团团',summary:'故事途中插问月亮的形状。',end:'用户主动结束',emotion:'放松',source:'有一轮被打断'},
    {id:'weather',title:'周末适合去公园吗',time:'09:18',date:'今天',duration:'1分钟',rounds:1,status:'已归档',type:'green',device:'圆圆',summary:'查询天气，讨论周末户外活动。',end:'空闲30分钟',emotion:'平静',source:'已完成天气查询'},
    {id:'dinner',title:'晚饭后散步计划',time:'19:42',date:'昨天',duration:'1分钟',rounds:1,status:'已归档',type:'green',device:'团团',summary:'讨论饭后散步与补水。',end:'空闲30分钟',emotion:'关心',source:'逐轮记录已保存'}
  ];
  var capabilities=[
    {name:'实时语音',icon:'mic',stage:'已接入',type:'green',desc:'连续问答、自然追问与语音回应。',detail:'微雪 / M5 · ESP32-S3',route:'conversations'},
    {name:'自然插话',icon:'sound',stage:'已接入',type:'green',desc:'说话时插话，停止旧回复并处理新输入。',detail:'真人反馈与播放证据分别留存',route:'conversations'},
    {name:'联网查询',icon:'search',stage:'已接入',type:'green',desc:'按需查询天气及实时信息，保留来源。',detail:'实际工具结果决定回答',route:'conversations'},
    {name:'设备控制',icon:'settings',stage:'已接入',type:'green',desc:'音量、屏幕亮度与设备状态，设置后回读。',detail:'按当前设备能力开放',route:'device'},
    {name:'屏幕表情',icon:'face',stage:'已接入',type:'green',desc:'系统状态、15种情绪与随实际声音变化的嘴型。',detail:'CHICK-01 · L0 / L1 / L2',route:'expressions'},
    {name:'设备近期记忆',icon:'brain',stage:'已接入',type:'green',desc:'同一设备近期聊天片段，可明确清除。',detail:'最近7天 · 至多24轮',route:'memories'},
    {name:'成员声纹',icon:'people',stage:'验证中',type:'amber',desc:'识别说话成员，拒绝把陌生人认成家人。',detail:'家庭现场验收尚未完成',route:'voiceprints'},
    {name:'聊天归档与查档',icon:'file',stage:'待开发',type:'',desc:'30分钟分段、归档摘要与按需历史查询。',detail:'会话规则已确认 · Eino尚未接入',route:'tasks'},
    {name:'拍照判题',icon:'grid',stage:'未接入',type:'',desc:'关联题目、照片与分析结果，支持后续追问。',detail:'完成独立验收后开放',route:'tasks'},
    {name:'提醒与日程',icon:'bell',stage:'未接入',type:'',desc:'持久化提醒计划、定时触发和实际投递。',detail:'完成调度与投递闭环后开放',route:'tasks'}
  ];
  function toast(message){var t=document.getElementById('toast');t.textContent=message;t.classList.add('show');clearTimeout(toast.timer);toast.timer=setTimeout(function(){t.classList.remove('show');},3200);}
  function modal(title,body,footer,drawer){var d=document.getElementById('dialog');if(d.open)d.close();d.className=drawer?'drawer':'';d.innerHTML='<div class="dialog-head"><h2>'+esc(title)+'</h2><button class="btn flat icon-only" data-ui="close" aria-label="关闭对话框">'+icon('close')+'</button></div><div class="dialog-body">'+body+'</div>'+(footer?'<div class="dialog-foot">'+footer+'</div>':'');d.showModal();}
  function confirmAction(title,description,callback,danger){modal(title,'<p class="small">'+esc(description)+'</p><p class="muted small mt">此操作仅影响当前原型示例，不会发送请求或操作真实设备。</p>','<button class="btn" data-ui="close">取消</button><button id="confirm-dialog" class="btn '+(danger?'danger':'primary')+'">确认演示</button>');document.getElementById('confirm-dialog').onclick=function(){document.getElementById('dialog').close();if(callback)callback();};}
  function definition(rows){return '<dl class="definition">'+rows.map(function(r){return '<div><dt>'+esc(r[0])+'</dt><dd>'+r[1]+'</dd></div>';}).join('')+'</dl>';}
  function pageDirectory(scope){var list=scope==='family'?familyPages:adminPages;modal('全部页面 · '+(scope==='family'?'家庭端':'内部后台'),'<p class="muted small mb">所有页面均为可交互的视觉原型。</p><div class="route-grid">'+list.map(function(p){return '<a data-ui="close" href="#/'+p[0]+'">'+icon(p[2])+esc(p[1])+icon('chevron')+'</a>';}).join('')+'</div>');}
  document.addEventListener('click',function(e){var el=e.target.closest('[data-ui]');if(!el)return;if(el.dataset.ui==='close')document.getElementById('dialog').close();if(el.dataset.ui==='demo')toast(el.dataset.message||'演示操作完成，实际功能将在接入后开放。');});
  document.getElementById('dialog').addEventListener('click',function(e){if(e.target===this){var b=this.getBoundingClientRect();if(e.clientX<b.left||e.clientX>b.right||e.clientY<b.top||e.clientY>b.bottom)this.close();}});
  window.UI={esc:esc,icon:icon,badge:badge,button:button,link:link,notice:notice,empty:empty,face:face,state:state,save:save,sessions:sessions,capabilities:capabilities,adminPages:adminPages,familyPages:familyPages,toast:toast,modal:modal,confirm:confirmAction,definition:definition,directory:pageDirectory};
}());

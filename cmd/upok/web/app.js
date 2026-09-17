'use strict';
const token=location.hash.slice(1)||sessionStorage.getItem('upok-session')||'';
if(location.hash){sessionStorage.setItem('upok-session',token);history.replaceState(null,'',location.pathname);}
const $=id=>document.getElementById(id);
let busy=false,dialogOpener=null;
const terminal=new Set(['deployed','failed','blocked']);
function message(text,error=false){$('message').textContent=text;$('message').className=error?'error':'';}
async function api(path,method='GET'){
 const headers={Authorization:'Bearer '+token};const options={method,headers};
 if(method==='POST'){headers['Content-Type']='application/json';options.body='{}';}
 const r=await fetch('/api/'+path,options);
 if(r.status===401)throw Error('本地会话已过期。请使用这次启动命令输出的完整地址重新打开。');
 const data=await r.json();if(!r.ok)throw Error(data.error||'操作未完成，请查看本地记录');return data;
}
function time(value){return value?new Date(value).toLocaleString():'尚未检查';}
function showDialog(title,text,opener=document.activeElement){dialogOpener=opener;$('dialog-title').textContent=title;const pre=document.createElement('pre');pre.textContent=text;$('dialog-body').replaceChildren(pre);$('dialog').showModal();}
$('close').addEventListener('click',()=>$('dialog').close());
$('dialog').addEventListener('close',()=>{if(dialogOpener?.isConnected)dialogOpener.focus();load().catch(e=>message(e.message,true));});
$('add').addEventListener('click',()=>showDialog('关联已有应用','内部版先关联已有 Railway + Neon 资源。\n\n在已有 Agent 中选择项目，让它使用随源码提供的 upok Skill，核对资源归属后运行 register。\n\n首次创建云资源、通用资源接管和数据库迁移尚未接入这一版。'));
async function load(){
 const views=await api('projects');const fragment=document.createDocumentFragment();
 for(const view of views){
  const p=view.project,o=view.observation,op=view.operations[0];const pending=op&&!terminal.has(op.state);
  const card=$('card').content.cloneNode(true),article=card.querySelector('article');article.dataset.name=p.name;
  card.querySelector('.name').textContent=p.name;
  card.querySelector('.plan').textContent=o?'Railway '+o.railway_plan+' · Neon '+o.neon_plan:'尚未读取资源状态';
  const status=card.querySelector('.status');
  const newerOperation=op?.provider_state&&(!o||new Date(op.updated_at)>new Date(o.at));
  const cloudState=newerOperation?op.provider_state:o?.service_state;
  status.textContent=pending?(op.state==='unknown'?(view.running?'正在确认发布':'结果待核实'):'正在发布'):op?.state==='blocked'?'发布未执行':op?.state==='failed'?'最近发布失败':cloudState==='SLEEPING'?'最近状态：休眠':cloudState==='SUCCESS'?'最近部署成功':'状态待刷新';
  if(pending||!o||op?.state==='blocked')status.classList.add('warn');if(op?.state==='failed')status.classList.add('bad');
  card.querySelector('.address').textContent=p.url;card.querySelector('.visit').href=p.url;
  card.querySelector('[data-action="publish"]').disabled=!!pending||!p.allow_publish;
  card.querySelector('.operation').textContent=op?op.message+(op.deployment_id?' · '+op.deployment_id.slice(0,8):''):o?.reason||'尚无本地发布记录';
  card.querySelector('.recovery').hidden=!pending||view.running;
  card.querySelector('.observed').textContent=time(newerOperation?op.updated_at:o?.at);
  card.querySelector('.database').textContent=o?.database_state||'尚未读取';
  const checks=view.checks;
  card.querySelector('.checked').textContent=checks?(checks.results.every(c=>c.ok)?'通过':'未通过或待确认')+' · '+time(checks.at):'尚未检查（刷新资源状态不会唤醒应用）';
  card.querySelector('.authorization').textContent=p.allow_publish?'允许向此服务发布当前源码'+(p.allow_trial?'，已接受 Trial 试用条件':''):'仅查看';
  if(checks&&!checks.results.every(c=>c.ok)&&!pending){status.textContent='访问待确认';status.classList.add('warn');}
  fragment.append(card);
 }
 if(!views.length){const empty=document.createElement('p');empty.className='empty';empty.textContent='还没有关联的应用。通过已有 Agent 或 CLI 添加一个项目后，它会显示在这里。';fragment.append(empty);}
 const focus=document.activeElement;const name=focus?.closest('article')?.dataset.name;const action=focus?.dataset.action;
 const focusSelector=action?'[data-action="'+action+'"]':focus?.matches('.visit')?'.visit':focus?.tagName==='SUMMARY'?'summary':null;
 const expanded=new Set([...document.querySelectorAll('article details[open]')].map(d=>d.closest('article').dataset.name));
 $('projects').replaceChildren(fragment);
 for(const article of document.querySelectorAll('article')){if(expanded.has(article.dataset.name))article.querySelector('details').open=true;if(name===article.dataset.name&&focusSelector){const next=article.querySelector(focusSelector);if(next&&!next.disabled)next.focus({preventScroll:true});}}
}
$('projects').addEventListener('click',async event=>{
 const button=event.target.closest('[data-action]');if(!button||busy)return;
 const name=button.closest('article').dataset.name,action=button.dataset.action;busy=true;button.disabled=true;message('正在处理…');
 try{
  const data=await api('projects/'+encodeURIComponent(name)+'/'+action,action==='logs'?'GET':'POST');
  if(action==='logs'){showDialog('部署日志 · 已处理已知凭证',data.lines.join('\n')||'暂无日志',button);message('日志已载入');return;}
  if(action==='publish')message('发布已开始。可以关闭页面，本地进程仍会继续处理。');
  else if(action==='check'&&data.results.some(c=>!c.ok))message('应用访问尚未确认，请查看检查结果。',true);
  else if(action==='reconcile'&&['unknown','failed','blocked'].includes(data.state))message(data.message,true);
  else message('已完成');await load();
 }catch(error){message(error.message,true);}finally{busy=false;if(button.isConnected)button.disabled=false;}
});
load().then(async()=>{const views=await api('projects');for(const v of views){if(!v.observation){try{await api('projects/'+v.project.name+'/refresh','POST');}catch(e){message(e.message,true);}}}await load();}).catch(e=>message(e.message,true));
// Poll local records only, including operations started by an external Agent or CLI.
setInterval(()=>{if(!busy&&!$('dialog').open)load().catch(e=>message(e.message,true));},5000);

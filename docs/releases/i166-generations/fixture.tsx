import React from 'react';
import { createRoot } from 'react-dom/client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import './src/index.css';
import '@xyflow/react/dist/style.css';
import './src/i18n';
import PlanDetail from './src/pages/PlanDetail';
const tasks = [
 {task_id:'t2215',org_ref:'T2215',title:'I166 原集成节点（已撤销）',assignee_ref:'agent:integration-dev',task_status:'discarded',node_status:'done',depends_on:[],effective:false},
 {task_id:'t2213',org_ref:'T2213',title:'I166 实现 React Flow + ELK Plan DAG 迁移并交付候选',assignee_ref:'agent:agent-center-dev1',task_status:'completed',node_status:'done',depends_on:[],effective:true},
 {task_id:'t2214',org_ref:'T2214',title:'I166 独立验收唯一 DAG 迁移候选',assignee_ref:'agent:agent-center-tester1',task_status:'failed',node_status:'failed',depends_on:['t2213'],effective:false},
 {task_id:'t2217',follows_task_id:'t2214',org_ref:'T2217',title:'I166 整改 graph-backed 依赖线缺失及旧画布残留',assignee_ref:'agent:agent-center-dev1',task_status:'completed',node_status:'done',depends_on:['t2213'],effective:true},
 {task_id:'t2223',org_ref:'T2223',title:'I166 集成已验收候选及证据到 main',assignee_ref:'agent:integration-dev',task_status:'completed',node_status:'done',depends_on:['t2217'],effective:true},
];
const generationOf = {t2215:0,t2213:0,t2214:0,t2217:1,t2223:2};
const snapshots = [0,1,2].map(revision=>({id:`gen-${revision}`,revision,reason:`第 ${revision+1} 代：交付与整改`,evidence:'Fixture',created_at:`2026-09-${15+revision}T01:00:00Z`,snapshot:{tasks:tasks.filter(t=>generationOf[t.task_id]<=revision).map(t=>({...t,status:t.task_status})),edges:tasks.flatMap(t=>t.depends_on.map(to_task_id=>({from_task_id:t.task_id,to_task_id,kind:'seq'}))),dispatch_records:tasks.map(t=>({task_id:t.task_id}))},snapshot_progress:{done:2,total:4}}));
const plan = {id:'review',project_id:'review',name:'I166 Plan DAG — 前端回归样例',description:'',status:'done',version:1,creator_ref:'user:owner',created_at:'2026-09-17T00:00:00Z',nodes:tasks,progress:{done:3,total:3}};
const graph={has_graph:true,nodes:[{id:'start',category:'control',control_kind:'start',title:'Start',status:'completed'},...tasks.map(t=>({...t,id:t.task_id,category:'business',status:'completed',follows_task_id:t.task_id==='t2217'?'t2214':undefined})),{id:'end',category:'control',control_kind:'end',title:'End',status:'completed'}],edges:[{from:'start',to:'t2213',kind:'seq'},...tasks.flatMap(t=>t.depends_on.map(from=>({from,to:t.task_id,kind:'seq'})))]};
const nativeFetch=window.fetch;
(window as any).__i166Requests=[];
window.fetch=async (input,init)=>{
 (window as any).__i166Requests.push(String(input));
 const url=String(input); if(!url.startsWith('/api'))return nativeFetch(input,init);
 let data:any=[];
 if(url==='/api/projects/review') data={id:'review',name:'UI regression fixture'};
 if(url==='/api/projects/review/plans/review') data=plan;
 if(url.endsWith('/graph'))data=graph;
 if(url.endsWith('/generations'))data={plan_id:'review',active_generation_id:'gen-2',generations:snapshots,nodes:tasks.map(t=>({task_id:t.task_id,revision:generationOf[t.task_id]}))};
 if(url.includes('/members'))data=tasks.map(t=>({id:t.assignee_ref,identity_id:t.assignee_ref,display_name:t.assignee_ref.slice(6),kind:'agent',status:'joined',role:'member'}));
 if(/^\/api\/agents\/[^/]+\/tasks$/.test(url))data={tasks:[]};
 if(/^\/api\/agents\/[^/]+$/.test(url))data={id:url.split('/').pop(),name:'agent-center-dev1',description:'DAG click regression fixture',lifecycle:'running',availability:'available',model:'fixture',cli:'fixture',version:1};
 if(url.includes('/activity'))data={activity:[],has_more:false};
 if(url.endsWith('/concurrency'))data={agent_id:'agent-center-dev1',cap:1,active:0,queued:0,slots:[],executors:[],reachable:true,has_snapshot:true};
 return new Response(JSON.stringify(data),{status:200,headers:{'Content-Type':'application/json'}});
};
// No center connection: this entry is an isolated frontend fixture.
window.EventSource=class {addEventListener(){}removeEventListener(){}close(){}} as any;
const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
createRoot(document.getElementById('root')!).render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/projects/review/plans/review']}><Routes><Route path="/projects/:id/plans/:planId" element={<PlanDetail/>}/></Routes></MemoryRouter></QueryClientProvider>);

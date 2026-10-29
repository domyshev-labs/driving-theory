import { useEffect, useRef, useState } from 'react';

export type SearchScope = { topics:boolean; subtopics:boolean; questions:boolean };
export const defaultSearchScope:SearchScope={topics:true,subtopics:true,questions:false};
export function SearchText({text,query,enabled=true}:{text:string;query:string;enabled?:boolean}) {
 const start=enabled&&query?text.toLocaleLowerCase().indexOf(query):-1;
 if(start<0)return <>{text}</>;
 const parts=[];let position=0,index=start;
 while(index>=0){parts.push(text.slice(position,index),<mark key={index}>{text.slice(index,index+query.length)}</mark>);position=index+query.length;index=text.toLocaleLowerCase().indexOf(query,position);}
 parts.push(text.slice(position));return <span data-search-match>{parts}</span>;
}
export function LibrarySearch({value,onChange,scope,onScope,container,resultKey}:{value:string;onChange:(value:string)=>void;scope:SearchScope;onScope:(scope:SearchScope)=>void;container:React.RefObject<HTMLDivElement|null>;resultKey:string}) {
 const [open,setOpen]=useState(false);
 const root=useRef<HTMLDivElement>(null);
 const hits=useRef<HTMLElement[]>([]);
 const active=useRef(0);
 const [counter,setCounter]=useState({index:0,total:0});
 useEffect(()=>{if(!open)return;const close=(event:PointerEvent)=>{if(!root.current?.contains(event.target as Node))setOpen(false);};const escape=(event:KeyboardEvent)=>{if(event.key==='Escape')setOpen(false);};document.addEventListener('pointerdown',close);document.addEventListener('keydown',escape);return()=>{document.removeEventListener('pointerdown',close);document.removeEventListener('keydown',escape);};},[open]);
 useEffect(()=>{
  const frame=requestAnimationFrame(()=>{
   hits.current.forEach(hit=>hit.classList.remove('search-current'));
   hits.current=value.trim()?Array.from(container.current?.querySelectorAll<HTMLElement>('[data-search-match]')??[]):[];
   active.current=0;hits.current[0]?.classList.add('search-current');setCounter({index:hits.current.length?1:0,total:hits.current.length});
  });return()=>cancelAnimationFrame(frame);
 },[value,scope,container,resultKey]);
 function move(delta:number){const list=hits.current;if(!list.length)return;list[active.current]?.classList.remove('search-current');active.current=(active.current+delta+list.length)%list.length;const hit=list[active.current];for(let parent=hit.parentElement;parent;parent=parent.parentElement){if(parent instanceof HTMLDetailsElement)parent.setAttribute('open','');}hit.classList.add('search-current');hit.scrollIntoView({block:'center',behavior:'smooth'});setCounter({index:active.current+1,total:list.length});}
 const option=(key:keyof SearchScope,label:string)=><label key={key}><input type="checkbox" checked={scope[key]} onChange={e=>onScope({...scope,[key]:e.target.checked})}/>{label}</label>;
 return <div className="library-search" ref={root}><div className="library-search-input"><input aria-label="Поиск по панели обучения" placeholder="Поиск по темам и вопросам…" value={value} onChange={e=>onChange(e.target.value)} onKeyDown={e=>{if(e.key==='Enter'){e.preventDefault();move(e.shiftKey?-1:1);}}}/><button className="search-options-trigger" type="button" aria-label="Настройки поиска" aria-expanded={open} aria-haspopup="dialog" onClick={()=>setOpen(!open)}><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M3 6h18M3 12h18M3 18h18"/><path d="M8 3v6M16 9v6M9 15v6"/></svg></button></div>{open&&<div className="search-options-popover" role="dialog" aria-label="Где искать"><strong>Где искать</strong><h3>Тематические</h3>{option('topics','Темы')}{option('subtopics','Подтемы')}{option('questions','Вопросы')}</div>}{value.trim()&&<div className="search-navigation" role="navigation" aria-label="Найденные элементы"><span aria-live="polite">{counter.total?`${counter.index} из ${counter.total}`:'Ничего не найдено'}</span><button type="button" aria-label="Предыдущее совпадение" disabled={!counter.total} onClick={()=>move(-1)}>↑</button><button type="button" aria-label="Следующее совпадение" disabled={!counter.total} onClick={()=>move(1)}>↓</button><button type="button" aria-label="Очистить поиск" onClick={()=>onChange('')}>×</button></div>}</div>;
}

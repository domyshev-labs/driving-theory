import { useCallback, useEffect, useLayoutEffect, useRef, useState, useTransition } from 'react';
import { createPortal } from 'react-dom';
import { LinearStatistics, ReleaseDetails } from './LinearStatistics';
import { LibrarySearch, SearchText, defaultSearchScope, type SearchScope } from './LibrarySearch';
import { readRoute, useTrainingNavigation } from './navigation';
import { topicReviewState, topicErrorLabel, topicContinuation, topicReturnState } from './topicActions';
import { PreferencesProvider, usePreferences, BilingualText, TrainingFrame } from './Preferences';

type Answer = { letter: string; text: string; correct?: boolean };
type AuthUser = { id: string; email: string };
type HelpLanguage = 'ru' | 'es' | 'en';
async function api<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(path, {
    ...init,
    headers: init?.body ? { 'Content-Type': 'application/json', ...init.headers } : init?.headers,
    credentials: 'same-origin',
    cache: 'no-store',
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => ({})) as { error?: string };
    throw new Error(payload.error ?? `HTTP ${response.status}`);
  }
  if (response.status === 204) return undefined as T;
  return response.json() as Promise<T>;
}

function HelpContent({ text }: { text: string }) {
  const blocks = text
    .replace(/\u00a0/g, ' ')
    .replace(/\s{2,}/g, '\n')
    .replace(/(?<!^)(?=\d+(?:\.\d+){1,2}\s+[A-Z])/g, '\n')
    .replace(/(?<=[a-z)])(?=S-\d)/g, '\n')
    .replace(
      /(?<=[a-z):])(?=(?:Indicates|It indicates|These signs|On roads|When the|A continuous|A broken|Advance warning|Announces|Stopping, parking))/g,
      '\n',
    )
    .replace(/(?<=[.!?])(?=[A-Z])/g, '\n')
    .replace(/(\(Página[^)]+\))/g, '\n$1\n')
    .split('\n')
    .map((block) => block.trim())
    .filter(Boolean);

  return (
    <div className="help-content">
      {blocks.map((block, index) => {
        const isPage = /^\((?:Página|Страница)/.test(block);
        const isHeading =
          /^\d+(?:\.\d+){0,2}\s+[A-Z]/.test(block) ||
          /^S-\d/.test(block) ||
          (!/[.!?]$/.test(block) && block.length < 105);

        if (isPage) return <p className="help-page" key={index}>{block}</p>;
        if (isHeading) return <h3 className="help-heading" key={index}>{block}</h3>;
        return <p className="help-paragraph" key={index}>{block}</p>;
      })}
    </div>
  );
}

function Modal({
  title,
  eyebrow,
  className = '',
  onClose,
  children,
}: {
  title: string;
  eyebrow?: string;
  className?: string;
  onClose: () => void;
  children: React.ReactNode;
}) {
  useEffect(() => {
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key === 'Escape') onClose();
    }
    document.addEventListener('keydown', closeOnEscape);
    return () => document.removeEventListener('keydown', closeOnEscape);
  }, [onClose]);

  return (
    <div className="modal-overlay" role="presentation" onMouseDown={(event) => {
      if (event.target === event.currentTarget) onClose();
    }}>
      <section className={`modal-card ${className}`} role="dialog" aria-modal="true" aria-label={title}>
        <header className="modal-header">
          <div>
            {eyebrow && <p>{eyebrow}</p>}
            <h2>{title}</h2>
          </div>
          <button type="button" className="modal-close-icon" aria-label="Закрыть" onClick={onClose}>×</button>
        </header>
        <div className="modal-body">{children}</div>
        <footer className="modal-footer">
          <button type="button" className="secondary" onClick={onClose}>Закрыть</button>
        </footer>
      </section>
    </div>
  );
}

function AccountMenu({ user, onLogout }: { user: AuthUser; onLogout: () => Promise<void> }) {
  const { preferences, update } = usePreferences();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const menu = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    if (!open) return;
    function outside(event: PointerEvent) {
      if (event.target instanceof Node && !menu.current?.contains(event.target)) setOpen(false);
    }
    function escape(event: KeyboardEvent) {
      if (event.key === 'Escape') { setOpen(false); trigger.current?.focus(); }
    }
    document.addEventListener('pointerdown', outside);
    document.addEventListener('keydown', escape);
    return () => { document.removeEventListener('pointerdown', outside); document.removeEventListener('keydown', escape); };
  }, [open]);
  async function signOut() {
    if (busy) return;
    setBusy(true);
    try { await onLogout(); } finally { setBusy(false); setOpen(false); }
  }
  return <><div className="account-menu-shell" ref={menu}>
    <ReleaseDetails />
    <button ref={trigger} type="button" className="account-menu-trigger" aria-label="Меню пользователя" aria-expanded={open} aria-controls="account-menu" onClick={() => setOpen(!open)}>
      <svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 6h16M4 12h16M4 18h16" /></svg>
    </button>
    {open && <section id="account-menu" className="account-menu-panel" aria-label="Аккаунт"><p>{user.email}</p><hr /><button type="button" onClick={() => { setOpen(false); if (readRoute().screen !== 'statistics') { window.history.pushState(null, '', '?screen=statistics'); window.dispatchEvent(new PopStateEvent('popstate')); } }}>Статистика</button><button type="button" onClick={() => { setOpen(false); setSettingsOpen(true); }}>Настройки</button><button type="button" disabled={busy} onClick={signOut}>{busy ? 'Выходим…' : 'Выйти'}</button></section>}
  </div>{settingsOpen && <Modal eyebrow="Предпочтения" title="Настройки" className="settings-modal" onClose={() => setSettingsOpen(false)}>
    <div className="settings-options">
      <label><span><strong>Сворачивать верхнюю панель</strong><small>По умолчанию при открытии вопроса на мобильном</small></span><input type="checkbox" role="switch" checked={preferences.collapseMobileHeader} onChange={e => update('collapseMobileHeader', e.target.checked)} /></label>
      <label><span><strong>Испанский текст вопроса</strong><small>Показывать на всех экранах</small></span><input type="checkbox" role="switch" checked={preferences.showSpanishQuestion} onChange={e => update('showSpanishQuestion', e.target.checked)} /></label>
      <label><span><strong>Испанский текст ответов</strong><small>Показывать на всех экранах</small></span><input type="checkbox" role="switch" checked={preferences.showSpanishAnswers} onChange={e => update('showSpanishAnswers', e.target.checked)} /></label>
    </div><p className="settings-note">Настройки сохраняются в этом браузере.</p>
  </Modal>}</>;
}

function LoginScreen({ onAuthenticated }: { onAuthenticated: (user: AuthUser) => void }) {
  const [email, setEmail] = useState('');
  const [code, setCode] = useState('');
  const [step, setStep] = useState<'email' | 'code'>('email');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  async function requestCode(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError('');
    try {
      await api('/api/auth/request-code', { method: 'POST', body: JSON.stringify({ email }) });
      setStep('code');
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Не удалось отправить код');
    } finally {
      setBusy(false);
    }
  }

  async function verifyCode(event: React.FormEvent) {
    event.preventDefault();
    setBusy(true);
    setError('');
    try {
      const user = await api<AuthUser>('/api/auth/verify-code', { method: 'POST', body: JSON.stringify({ email, code }) });
      onAuthenticated(user);
    } catch (requestError) {
      setError(requestError instanceof Error ? requestError.message : 'Не удалось войти');
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="auth-page">
      <section className="auth-card">
        <p className="eyebrow">Driving Theory</p>
        <h1>{step === 'email' ? 'Войти или зарегистрироваться' : 'Введите код из письма'}</h1>
        <p>{step === 'email' ? 'Пароль не нужен — пришлём одноразовый код на email.' : `Мы отправили шестизначный код на ${email}`}</p>
        <form onSubmit={step === 'email' ? requestCode : verifyCode}>
          {step === 'email' ? (
            <label>Email<input type="email" autoComplete="email" required value={email} onChange={(event) => setEmail(event.target.value)} placeholder="you@example.com" /></label>
          ) : (
            <label>Код<input className="code-input" inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required value={code} onChange={(event) => setCode(event.target.value.replace(/\D/g, ''))} placeholder="000000" autoFocus /></label>
          )}
          {error && <p className="auth-error" role="alert">{error}</p>}
          <button className="primary" disabled={busy}>{busy ? 'Подождите…' : step === 'email' ? 'Получить код' : 'Войти'}</button>
          {step === 'code' && <button type="button" className="auth-back" onClick={() => { setStep('email'); setCode(''); setError(''); }}>← Изменить email</button>}
        </form>
      </section>
    </main>
  );
}

type LibraryQuestion = { canonical_id: string; question: string; question_ru?: string; topic_ids: string[]; verified: boolean };
type TopicNode = { id: string; label_ru: string; parent_id: string | null };
type LibraryData = { nodes: TopicNode[]; questions: LibraryQuestion[] };

type TopicRun = { review_pending?: number; inherited?: number; kind: 'full' | 'new'; id: number; total: number; answered: number; correct: number; wrong: number; started_at: string; completed_at: string | null; review?: TopicRun; error_reviews?: TopicRun[] };
type TopicLaunch = { reviewParent?: number; newQuestions?: boolean; runId?: number };
type TopicSummary = { remaining: number; repeat_in_progress?: boolean; new_count: number; new_question_ids: string[]; question_runs: Record<string, number>; completed: boolean; runs: TopicRun[]; total: number; answered: number; correct: number; wrong: number; next_question: number; results: Record<string, boolean> };

function TopicStatistics({ stats, total, onReview }: { stats?: TopicSummary; total: number; onReview: (parent: number) => void }) {
  const [anchor, setAnchor] = useState<{ top: number; right: number; bottom?: number } | null>(null);
  const trigger = useRef<HTMLSpanElement>(null);
  const popup = useRef<HTMLDivElement>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  function keepOpen() { if (closeTimer.current) clearTimeout(closeTimer.current); }
  function closeSoon() { keepOpen(); closeTimer.current = setTimeout(() => setAnchor(null), 180); }
  function open() {
    keepOpen();
    const rect = trigger.current?.getBoundingClientRect();
    if (rect) setAnchor(rect.top > window.innerHeight / 2
      ? { top: 0, bottom: window.innerHeight - rect.top + 6, right: Math.max(12, window.innerWidth - rect.right) }
      : { top: rect.bottom + 6, right: Math.max(12, window.innerWidth - rect.right) });
  }
  useEffect(() => () => { if (closeTimer.current) clearTimeout(closeTimer.current); }, []);
  useEffect(() => {
    if (!anchor) return;
    function close(event: KeyboardEvent) { if (event.key === 'Escape') { setAnchor(null); trigger.current?.focus(); } }
    function outside(event: PointerEvent) { if (!trigger.current?.contains(event.target as Node) && !popup.current?.contains(event.target as Node)) setAnchor(null); }
    function scrolled(event: Event) { if (!popup.current?.contains(event.target as Node)) setAnchor(null); }
    document.addEventListener('keydown', close); document.addEventListener('pointerdown', outside); window.addEventListener('scroll', scrolled, true);
    return () => { document.removeEventListener('keydown', close); document.removeEventListener('pointerdown', outside); window.removeEventListener('scroll', scrolled, true); };
  }, [anchor]);
  return <><span className="topic-counts topic-statistics" ref={trigger} tabIndex={0} role="button" aria-label="История прохождений темы" aria-expanded={!!anchor}
    onMouseEnter={open} onMouseLeave={closeSoon} onFocus={open} onBlur={(event) => { if (!popup.current?.contains(event.relatedTarget as Node)) closeSoon(); }}
    onClick={(event) => { event.preventDefault(); event.stopPropagation(); if (!anchor) open(); }}
    onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); event.stopPropagation(); open(); } }}>
    <span>Всего: {total}</span><span>Выполнено: {stats?.answered ?? '…'}</span><span>✓ {stats?.correct ?? '…'}</span><span className="wrong-mark">× {stats?.wrong ?? '…'}</span>
  </span>{anchor && createPortal(<div className="topic-history-popover" ref={popup} role="dialog" aria-label="История прохождений темы"
    style={{ right: anchor.right, top: anchor.bottom === undefined ? anchor.top : undefined, bottom: anchor.bottom }}
    onMouseEnter={keepOpen} onMouseLeave={closeSoon} onFocus={keepOpen} onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node) && !trigger.current?.contains(event.relatedTarget as Node)) closeSoon(); }}>
    <strong>История прохождений</strong>
    {!stats?.runs.length && <p>Прохождений пока нет.</p>}
    {stats?.runs.map((run, index) => <section key={run.id}><b>{run.kind === 'new' ? 'Новые вопросы' : `Прохождение ${stats.runs.slice(index).filter((item) => item.kind === 'full').length}`}</b><time>{new Date(run.started_at).toLocaleString('ru-RU')}</time>
      <p>{run.completed_at ? 'Завершено' : 'В процессе'} · {run.answered}/{run.total} · ✓ {run.correct} · × {run.wrong}</p>{!!run.inherited && <p>Ранее отвечено в других темах: {run.inherited}</p>}
      {run.error_reviews?.map(review=><div key={review.id}><p>Работа над ошибками темы: {review.completed_at?'завершена':'в процессе'} · {review.answered}/{review.total} · ✓ {review.correct} · × {review.wrong}</p><time>{new Date(review.started_at).toLocaleString('ru-RU')}</time><button type="button" onClick={()=>{setAnchor(null);onReview(-review.id);}}>Посмотреть работу над ошибками</button></div>)}
      {run.review ? <><p>Работа над ошибками: {run.review.completed_at ? 'выполнена' : 'в процессе'} · {run.review.answered}/{run.review.total} · ✓ {run.review.correct} · × {run.review.wrong}</p>
        {run.review.completed_at && <time>Завершена: {new Date(run.review.completed_at).toLocaleString('ru-RU')}</time>}<button type="button" onClick={() => { setAnchor(null); onReview(run.id); }}>{run.review.completed_at ? 'Посмотреть работу над ошибками' : 'Продолжить работу над ошибками'}</button></>
        : run.completed_at && run.wrong > 0 ? <button type="button" onClick={() => { setAnchor(null); onReview(run.id); }}>Сделать работу над ошибками</button> : <p>{run.completed_at ? 'Ошибок нет' : 'Работа над ошибками доступна после завершения'}</p>}
    </section>)}
  </div>, document.body)}</>;
}

function PageLoader() {
  return <main className="page-loading" role="status" aria-label="Загрузка"><span className="page-spinner" aria-hidden="true" /></main>;
}

function SectionNavigator({ library, sectionId, onOpenTopic }: {
  library: LibraryData;
  sectionId: string; onOpenTopic: (id: string, question?: string, options?: TopicLaunch) => void;
}) {
  const [anchor, setAnchor] = useState<{ left: number; top: number } | null>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const popup = useRef<HTMLDivElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const section = library.nodes.find((node) => node.id === sectionId);
  function keepOpen() { if (timer.current) clearTimeout(timer.current); }
  function closeSoon() { keepOpen(); timer.current = setTimeout(() => { if (!document.querySelector('.topic-history-popover:hover')) setAnchor(null); }, 180); }
  function open() {
    keepOpen();
    const rect = trigger.current?.getBoundingClientRect();
    if (rect) setAnchor({ left: Math.max(12, Math.min(rect.left, window.innerWidth - 652)), top: rect.bottom + 8 });
  }
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);
  useEffect(() => {
    if (!anchor) return;
    function close(event: KeyboardEvent) { if (event.key === 'Escape') { setAnchor(null); trigger.current?.focus(); } }
    function outside(event: PointerEvent) { if (!(event.target as Element).closest('.topic-history-popover') && !trigger.current?.contains(event.target as Node) && !popup.current?.contains(event.target as Node)) setAnchor(null); }
    document.addEventListener('keydown', close); document.addEventListener('pointerdown', outside);
    return () => { document.removeEventListener('keydown', close); document.removeEventListener('pointerdown', outside); };
  }, [anchor]);
  return <><button className="section-navigation-trigger" type="button" ref={trigger} aria-haspopup="dialog" aria-expanded={!!anchor}
    onMouseEnter={open} onMouseLeave={closeSoon} onFocus={(event) => { if (!popup.current?.contains(event.relatedTarget as Node)) open(); }} onClick={open}
    onBlur={(event) => { if (!popup.current?.contains(event.relatedTarget as Node)) closeSoon(); }}>{section?.label_ru}</button>
    {anchor && createPortal(<div className="section-navigation-popup" role="dialog" aria-label={section?.label_ru} ref={popup}
      style={{ left: anchor.left, top: anchor.top, maxHeight: `calc(100dvh - ${anchor.top + 16}px)` }}
      onMouseEnter={keepOpen} onMouseLeave={closeSoon} onFocus={keepOpen}
      onBlur={(event) => { if (!event.currentTarget.contains(event.relatedTarget as Node) && !trigger.current?.contains(event.relatedTarget as Node)) closeSoon(); }}>
      <header><strong>{section?.label_ru}</strong><button type="button" aria-label="Закрыть раздел" onClick={() => setAnchor(null)}>×</button></header>
      <QuestionLibrary sectionId={sectionId} library={library}
        view={{ search: '', expanded: [sectionId], scrollTop: 0 }} onRemember={() => undefined}
        onOpenTopic={(id, question, options) => { setAnchor(null); onOpenTopic(id, question, options); }} />
    </div>, document.body)}</>;
}

function TopicHeading({ section, title }: { section: React.ReactNode; title: string }) {
  return <><div className="section-heading">{section}</div><div className="theme-heading"><h1>{title}</h1></div></>;
}
type LibraryView = { recentTopic?: string; searchScope?: SearchScope; search: string; expanded: string[]; scrollTop: number; completedTopic?: { id: string; question: string } };

function TopicQuestionList({children}:{children:React.ReactNode}) {
  return <details className="topic-questions-disclosure"><summary><svg viewBox="0 0 24 24" aria-hidden="true"><path d="m6 7 6 5 6-5M6 13l6 5 6-5"/></svg><span className="question-list-closed">Раскрыть список вопросов</span><span className="question-list-open">Свернуть список вопросов</span></summary>{children}</details>;
}

function QuestionLibrary({ library, onOpenTopic, view, onRemember, sectionId, onStatistics }: {
  library: LibraryData; onOpenTopic: (id: string, question?: string, options?: TopicLaunch) => void;
  onStatistics?: () => void; sectionId?: string; view: LibraryView; onRemember: (snapshot: LibraryView) => void;
}) {
  const root = useRef<HTMLElement>(null);
  const scroll = useRef<HTMLDivElement>(null);
  const [search, setSearch] = useState(view.search);
  const [searchScope,setSearchScope]=useState<SearchScope>(view.searchScope??defaultSearchScope);
  const [expanded, setExpanded] = useState(view.expanded);
  const [summaries, setSummaries] = useState<Record<string, TopicSummary> | null>(null);
  const [progressError, setProgressError] = useState('');
  const [returnReady,setReturnReady]=useState(()=>Boolean(sectionId)||!(view.recentTopic||view.completedTopic));
  useEffect(() => {
    let active = true;
    void api<Record<string, TopicSummary>>('/api/topics/progress').then((data) => { if (active) setSummaries(data); }).catch(() => { if (active) setProgressError('Не удалось загрузить прогресс. Обновите страницу, чтобы продолжить тему.'); });
    return () => { active = false; };
  }, []);
  // Reveal the dashboard only after its final layout and target scroll are ready.
  useLayoutEffect(() => {
    const container = scroll.current;
    if (!container) return;
    container.scrollTop = view.scrollTop;
    const targetId=view.recentTopic??view.completedTopic?.id;
    if(targetId&&!summaries&&!progressError)return;
    if(targetId){
      const branch=Array.from(container.querySelectorAll<HTMLElement>('[data-library-id]')).find(item=>item.dataset.libraryId===targetId);
      if(branch)container.scrollTop+=branch.getBoundingClientRect().top-container.getBoundingClientRect().top-24;
    }
    let reveal=0;
    const layout=requestAnimationFrame(()=>{reveal=requestAnimationFrame(()=>setReturnReady(true));});
    return()=>{cancelAnimationFrame(layout);cancelAnimationFrame(reveal);};
  }, [summaries,progressError,view.scrollTop,view.completedTopic,view.recentTopic]);

  function toggleBranch(id: string, open: boolean) {
    setExpanded((current) => open ? current.includes(id) ? current : [...current, id] : current.includes(id) ? current.filter((item) => item !== id) : current);
  }
  function rememberView() {
    onRemember({
      recentTopic:view.recentTopic, completedTopic:view.completedTopic, search, searchScope, scrollTop: scroll.current?.scrollTop ?? 0,
      expanded: Array.from(root.current?.querySelectorAll<HTMLDetailsElement>('details[data-library-id][open]') ?? []).map((node) => node.dataset.libraryId!),
    });
  }
  function openTopic(id: string, question?: string, options?: TopicLaunch) { rememberView(); onOpenTopic(id, question, options); }
  async function repeatTopic(id: string) {
    try { await api(`/api/topics/${encodeURIComponent(id)}/reset`, { method: 'POST' }); openTopic(id); }
    catch (err) { setProgressError(err instanceof Error ? err.message : 'Не удалось начать новое прохождение'); }
  }
  const query = search.trim().toLocaleLowerCase();
  const textMatches=(text:string)=>text.toLocaleLowerCase().includes(query);
  const matches = (q:LibraryQuestion)=>textMatches(`${q.question} ${q.question_ru||''}`);
  const nodeMatches=(node:TopicNode)=>Boolean(query&&(node.parent_id?searchScope.subtopics:searchScope.topics)&&textMatches(node.label_ru));
  const branchMatches=(node:TopicNode):boolean=>nodeMatches(node)||(searchScope.questions&&library.questions.some(q=>q.topic_ids.includes(node.id)&&matches(q)))||library.nodes.some(child=>child.parent_id===node.id&&branchMatches(child));
  function questionList(items: LibraryQuestion[], node: TopicNode, showAll=false) {
    const sorted = [...items].sort((a,b)=>a.canonical_id.localeCompare(b.canonical_id));
    return <div className="library-questions">{sorted.map((q,index)=>({q,index})).filter(({q})=>!query||showAll||(searchScope.questions&&matches(q))).map(({q,index})=>{
      const result=summaries?.[node.id]?.results[q.canonical_id];
      return <button type="button" key={q.canonical_id} data-question-id={q.canonical_id} onClick={()=>{
        const stats=summaries?.[node.id];
        const options:TopicLaunch=stats?.new_question_ids.includes(q.canonical_id)?{newQuestions:true}:{runId:stats?.question_runs[q.canonical_id]};
        openTopic(node.id,q.canonical_id,options);
      }}><span className="library-question-id topic-question-row"><b className="topic-list-number">{index+1}</b><span className="library-question-title"><SearchText text={q.question_ru||q.question.split('\n').filter(Boolean).at(-1)||''} query={query} enabled={searchScope.questions}/></span><i className={result===false?'wrong-mark':''}>{result===undefined?'':result?'✓':'×'}</i></span></button>;
    })}</div>;
  }
  function topic(node: TopicNode, ancestorMatch=false): React.ReactNode {
    const children = library.nodes.filter((n) => n.parent_id === node.id);
    const ids = new Set([node.id, ...children.map((n) => n.id)]);
    const items = library.questions.filter((q) => q.topic_ids.some((id) => ids.has(id)));
    const own = items.filter((q) => q.topic_ids.includes(node.id));
    const count = new Set(items.filter((q) => q.verified).map((q) => q.canonical_id)).size;
    const stats = summaries?.[node.id];
    const reviewState=topicReviewState(stats?.runs??[]);
    const hasErrors=!!stats?.wrong;
    const continuation=stats?topicContinuation({...stats,total:count}):null;
    const reviewPrimary=continuation?.target==='repeat'&&hasErrors;
    const chipLabel=reviewPrimary?topicErrorLabel(stats?.wrong??0):continuation?.label;
    const chipTooltip=reviewPrimary?'Откроются все текущие вопросы с ошибками этой темы. Верные ответы уберут их из списка ошибок.':continuation?.tooltip;
    if(query&&!ancestorMatch&&!branchMatches(node))return null;
    const titleMatch=ancestorMatch||nodeMatches(node);
    const recent=!sectionId&&view.recentTopic===node.id;
    return <details className={`library-branch ${children.length ? 'library-section' : ''} ${recent?'recent-topic':''}`} key={node.id} data-library-id={node.id} open={!!query || expanded.includes(node.id)} onToggle={(event) => { if (!event.currentTarget.open) event.currentTarget.querySelectorAll<HTMLDetailsElement>('.topic-questions-disclosure').forEach(list=>{list.open=false;}); if (!query) toggleBranch(node.id, event.currentTarget.open); }}><summary>{recent&&<span className="recent-topic-label" role="note">Вы только что занимались этой подтемой</span>}<span className="library-node-title"><SearchText text={node.label_ru} query={query} enabled={node.parent_id?searchScope.subtopics:searchScope.topics}/></span>{children.length ? <span className="topic-counts"><span>Тем: {children.length}</span><span>Пройдено: {summaries ? children.filter((child) => summaries[child.id]?.completed && !summaries[child.id]?.new_count).length : '…'}</span></span> : <TopicStatistics stats={stats} total={count} onReview={(parent) => openTopic(node.id, undefined, { reviewParent: parent })} />}</summary>
      <div className="library-children">{!children.length && count > 0 && <div className="topic-start-actions"><div className="topic-start-primary"><button type="button" className="primary library-start" disabled={!stats} onClick={() => {
        if(continuation?.target==='repeat'&&hasErrors){openTopic(node.id,undefined,{reviewParent:-1});return;}
        if(continuation?.target==='repeat'){void repeatTopic(node.id);return;}
        openTopic(node.id,undefined,continuation?.target==='new'?{newQuestions:true}:undefined);
      }}>{continuation?.target==='repeat'?(hasErrors?'Работа над ошибками':'Пройти тему снова'):continuation?.target==='new'?'Пройти новые вопросы':'Продолжить тему'}</button>{continuation&&<span className="topic-continuation-chip" tabIndex={0} title={chipTooltip} aria-label={`${chipLabel}. ${chipTooltip}`}>{chipLabel}</span>}{!hasErrors&&reviewState.completed && <span className="topic-review-completed">✓ Работа над ошибками выполнена</span>}</div>
      {hasErrors&&continuation?.target!=='repeat'&&<button type="button" className="repeat-full-topic" onClick={()=>openTopic(node.id,undefined,{reviewParent:-1})}>Работа над ошибками · {stats?.wrong}</button>}
</div>}{children.map(child=>topic(child,titleMatch))}{!children.length && count>0 && <TopicQuestionList>{questionList(own, node,titleMatch)}</TopicQuestionList>}{!count && <p className="library-empty">Вопросы появятся по мере пополнения коллекции.</p>}</div>
    </details>;
  }
  return <main className={`library-page ${sectionId ? 'section-library' : ''} ${returnReady?'':'restoring-library'}`} ref={root} aria-busy={!returnReady}>
    {!returnReady&&<div className="library-restore-message" role="status"><span>Возвращаемся к теме</span><QuestionLoader /></div>}
    {!sectionId && <>
    <header className="library-header"><a href="/" className="library-brand">Теория вождения</a></header>
    <section className="library-intro"><h1>Выбери предметную тематику для тренировки сейчас</h1>
      <div className="library-totals"><span><strong>{library.questions.length}</strong> вопросов</span><span><strong>{library.nodes.filter(node=>node.parent_id).length}</strong> тем</span><button className="dashboard-statistics" onClick={()=>{rememberView();onStatistics?.();}}>Статистика ↗</button></div>
      <LibrarySearch value={search} onChange={setSearch} scope={searchScope} onScope={setSearchScope} container={scroll} resultKey={summaries?'loaded':'loading'}/>
    </section>
    <div className="library-divider">По темам <span>Один вопрос может входить в несколько тем</span></div>
    </>}
    {progressError && <p className="answer-error" role="alert">{progressError}</p>}
    <div className="library-tree library-scroll" ref={scroll}>{library.nodes.filter((node) => sectionId ? node.id === sectionId : !node.parent_id).map((node) => { const content = topic(node); return content ? <div className="library-root" key={node.id}>{content}</div> : null; })}</div>
  </main>;
}

type TopicAnswerProgress = { selected_letter: string; correct_letter: string; correct: boolean };
type TopicQuestion = { id: string; number: number; question: string; question_ru?: string; image_url: string; help: Record<HelpLanguage, string>; answers: Answer[]; progress?: TopicAnswerProgress };
type TopicRecord = { numbering_total?: number; run_id: number; kind: 'full' | 'new'; id: string; title: string; questions: TopicQuestion[] };

// Prepare the illustration before switching; failed images never block navigation.
function prepareQuestionImage(url: string | null | undefined, signal: AbortSignal) {
  if (!url || signal.aborted) return Promise.resolve();
  return new Promise<void>((resolve) => {
    const image = new Image();
    const finish = () => {
      window.clearTimeout(timeout);
      image.onload = null;
      image.onerror = null;
      signal.removeEventListener('abort', finish);
      resolve();
    };
    const timeout = window.setTimeout(finish, 10000);
    signal.addEventListener('abort', finish, { once: true });
    image.onload = finish;
    image.onerror = finish;
    image.src = url;
    if (image.complete) finish();
  });
}

function QuestionLoader() {
  return <span className="question-loader" aria-hidden="true"><span /><span /><span /></span>;
}
function AnswerSaving({loading}:{loading:boolean}) {
  return loading ? <span className="answer-saving" role="status" aria-label="Сохраняем ответ"><QuestionLoader /></span> : null;
}

function NextQuestionButton({ label = 'Следующий вопрос →', disabled, imageUrl, onNext }: {
  label?: string; disabled: boolean; imageUrl?: string | null; onNext: () => void;
}) {
  const [preparing, setPreparing] = useState(false);
  const [pending, startTransition] = useTransition();
  const navigation = useRef<AbortController | null>(null);
  useEffect(() => () => navigation.current?.abort(), []);
  const loading = preparing || pending;
  async function next() {
    if (disabled || loading || navigation.current) return;
    const controller = new AbortController();
    navigation.current = controller;
    setPreparing(true);
    // Give the busy state a paint before preparing and rendering the next question.
    await new Promise<void>((resolve) => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
    await prepareQuestionImage(imageUrl, controller.signal);
    if (controller.signal.aborted) return;
    startTransition(() => {
      onNext();
      setPreparing(false);
    });
    navigation.current = null;
  }
  return <button className="primary next next-loading-button" type="button" disabled={disabled || loading}
    title={label} aria-busy={loading} aria-label={loading ? 'Загружаем следующий вопрос' : label} onClick={() => void next()}>
    <span className="next-button-label" aria-hidden={loading}>{label}</span>
    {loading && <QuestionLoader />}
  </button>;
}

function AnswerResult({ correct, letter, onHelp }: { correct: boolean; letter: string; onHelp: () => void }) {
  return <aside className={`feedback anchored-feedback ${correct ? 'success' : 'error'}`} aria-live="polite"><div className="feedback-title"><div className="feedback-outcome"><span aria-hidden="true">{correct ? '✓' : '×'}</span><strong>{correct ? 'Верно' : `Неверно · правильный ответ ${letter}`}</strong></div><div className="feedback-actions"><button type="button" className="feedback-help-button" aria-label="Открыть объяснение" onClick={onHelp}>?</button></div></div></aside>;
}

function TopicTrainer({ topicId, initialQuestion, reviewParent, newQuestions, runId, path, sectionNavigation, onQuestion, onBack, onFinish }: {
  topicId: string; initialQuestion?: string; reviewParent?: number; newQuestions?: boolean; runId?: number; path: string; sectionNavigation: React.ReactNode; onQuestion: (question: string, replace?: boolean, runId?: number, reviewParent?: number) => void; onBack: () => void; onFinish: (question: string) => void;
}) {
  const [requestedQuestion] = useState(initialQuestion);
  const [topic, setTopic] = useState<TopicRecord | null>(null);
  const [reviewID, setReviewID] = useState<number | undefined>();
  const [initialPosition, setPosition] = useState(0);
  const requestedPosition = topic?.questions.findIndex((item) => item.id === initialQuestion) ?? -1;
  const position = requestedPosition >= 0 ? requestedPosition : initialPosition;
  const [menuOpen, setMenuOpen] = useState(false);
  const [helpOpen, setHelpOpen] = useState(false);
  const [language, setLanguage] = useState<HelpLanguage>('ru');
  const [busy, setBusy] = useState(false);
  const [pendingAnswer,setPendingAnswer]=useState<{question:string;letter:string}|null>(null);
  const [error, setError] = useState('');
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    let active = true;
    void (async () => {
      const review = reviewParent && reviewParent < -1 ? {id:-reviewParent} : reviewParent ? await api<{ id: number }>(`/api/topics/${encodeURIComponent(topicId)}/${reviewParent===-1?'error-reviews':`reviews/${reviewParent}`}`, { method: 'POST' }) : undefined;
      const newRun = newQuestions && !review ? await api<{ id: number }>(`/api/topics/${encodeURIComponent(topicId)}/new`, { method: 'POST' }) : undefined;
      const requestedRun = newRun?.id ?? runId;
      const loaded = await api<TopicRecord>(`/api/topics/${encodeURIComponent(topicId)}${review ? `?review=${review.id}` : requestedRun ? `?run=${requestedRun}` : ''}`);
      return { loaded, review };
    })().then(({ loaded, review }) => {
      if (!active) return;
      setTopic(loaded);
      setReviewID(review?.id);
      const requested = loaded.questions.findIndex((q) => q.id === requestedQuestion);
      const unanswered = loaded.questions.findIndex((q) => !q.progress);
      setPosition(requested >= 0 ? requested : unanswered >= 0 ? unanswered : 0);
    }).catch((err: unknown) => { if (active) setError(err instanceof Error ? err.message : 'Не удалось загрузить подтему'); });
    return () => { active = false; };
  }, [topicId, requestedQuestion, reviewParent, newQuestions, runId]);
  useEffect(() => {
    if (!topic) return;
    const requested = topic.questions.findIndex((item) => item.id === initialQuestion);
    if ((requested < 0 || reviewParent===-1) && topic.questions[position]) onQuestion(topic.questions[position].id, true, reviewParent ? undefined : topic.run_id, reviewParent===-1&&reviewID?-reviewID:undefined);
  }, [initialQuestion, topic, position, onQuestion, reviewParent, reviewID]);
  useEffect(() => { panel.current?.scrollTo({ top: 0 }); panel.current?.parentElement?.scrollTo({ top: 0 }); }, [position]);
  useEffect(() => {
    function close(event: KeyboardEvent) { if (event.key === 'Escape') setMenuOpen(false); }
    document.addEventListener('keydown', close);
    return () => document.removeEventListener('keydown', close);
  }, []);
  function goTo(index: number) { if (topic?.questions[index]) onQuestion(topic.questions[index].id, false, reviewParent ? undefined : topic.run_id); setHelpOpen(false); }
  async function answer(letter: string) {
    const question = topic?.questions[position];
    if (!question || question.progress || busy) return;
    setBusy(true); setPendingAnswer({question:question.id,letter}); setError('');
    try {
      const result = await api<TopicAnswerProgress>(`/api/topics/${encodeURIComponent(topicId)}/answers`, { method: 'POST', body: JSON.stringify({ question_id: question.id, selected_letter: letter, review_id: reviewID, run_id: reviewID ? undefined : topic?.run_id }) });
      setTopic((current) => current ? { ...current, questions: current.questions.map((q) => q.id === question.id ? { ...q, progress: result } : q) } : null);
    } catch (err) { setError(err instanceof Error ? err.message : 'Не удалось сохранить ответ'); }
    finally { setBusy(false); setPendingAnswer(null); }
  }
  const q = topic?.questions[position];
  if (!topic || !q) return error ? <main className="loading error-message"><p role="alert">{error}</p><button className="secondary" onClick={onBack}>Панель обучения</button></main> : <PageLoader />;
  const total = topic.questions.length;
  const done = topic.questions.filter((item) => item.progress).length;
  const correct = topic.questions.filter((item) => item.progress?.correct).length;
  const filteredRun = !!reviewParent || topic.kind === 'new';
  const finishReady = done === total;
  const nextUnanswered = topic.questions.findIndex((item, index) => index > position && !item.progress);
  const nextPosition = nextUnanswered >= 0 ? nextUnanswered : topic.questions.findIndex((item) => !item.progress);
  const previousPosition = position-1;
  const progress = Math.round(done / total * 100);
  const help = q.help[language] || q.help.en;
  return <TrainingFrame>
    <button className="library-back topic-back" type="button" disabled={busy} onClick={onBack}>‹ Панель обучения</button>
    <main className="app-shell topic-shell">
      <header className="topbar topic-topbar">
        <div className="title-block"><TopicHeading section={sectionNavigation} title={topic.title} />{filteredRun && <p className="review-mode-label">{reviewParent ? 'Работа над ошибками · исправьте неверные ответы' : 'Новые вопросы · отдельное прохождение'}</p>}</div>
        <div className="header-tools">
          <div className="question-menu-shell topic-menu-shell">
            <button type="button" className="question-menu-trigger" aria-expanded={menuOpen} onClick={() => setMenuOpen(!menuOpen)}><span className="question-menu-trigger-copy"><span className="question-menu-trigger-label">Текущий вопрос</span><span className="question-menu-trigger-title">Вопрос {q.number} из {topic.numbering_total??total}</span></span><span className="question-menu-chevron">⌄</span></button>
            {menuOpen && <><button className="question-menu-backdrop" aria-label="Закрыть список вопросов" onClick={() => setMenuOpen(false)} /><section className="question-menu-panel topic-menu" aria-label={`Вопросы подтемы ${topic.title}`}><header><div><div className="menu-topic-heading"><p className="menu-section">{path}</p><div className="menu-theme"><strong>{topic.title}</strong></div></div><p className="topic-summary-line">Всего: {total} · Выполнено: {done} · Верно: {correct} · Неверно: {done - correct}</p></div><button aria-label="Закрыть" onClick={() => setMenuOpen(false)}>×</button></header><div className="question-menu-list">
              {topic.questions.map((item, index) => <button key={item.id} type="button" className={`topic-menu-question ${index === position ? 'active' : ''}`} onClick={() => { goTo(index); setMenuOpen(false); }}><b>{item.number}</b><span>{item.question_ru || item.question.split('\n').filter(Boolean).at(-1)}</span><i>{item.progress ? item.progress.correct ? '✓' : '×' : '·'}</i></button>)}
            </div></section></>}
          </div>

          <div className="answer-stats"><div className="score"><span>Верно</span><strong>{correct}</strong></div><div className="score score-wrong"><span>Неверно</span><strong>{done - correct}</strong></div></div>
        </div>
      </header>
      <div className="progress-row"><span>Вопрос {q.number} из {topic.numbering_total??total}</span><div className="progress-track"><div style={{ width: `${progress}%` }} /></div><span>{progress}%</span></div>
      <section className="quiz-card">
        <div className="visual-panel"><div className="question-number">#{q.number}</div><div className="visual-media">{q.image_url ? <img src={q.image_url} alt={`Иллюстрация к вопросу ${q.number}`} /> : <span className="visual-empty">Вопрос без иллюстрации</span>}</div></div>
        <div className="question-panel fixed-question-layout">
          <div className="question-content">
          <div className="question-heading-row"><h2><BilingualText text={q.question} /></h2></div>
          <div className="answer-scroll" ref={panel}><div className="answers">{q.answers.map((choice) => {
            const state = q.progress ? choice.letter === q.progress.correct_letter ? 'correct' : choice.letter === q.progress.selected_letter ? 'wrong' : 'muted' : '';
            return <button type="button" className={`answer ${state}`} key={choice.letter} disabled={!!q.progress || busy} aria-busy={pendingAnswer?.question===q.id&&pendingAnswer.letter===choice.letter} onClick={() => answer(choice.letter)}><span className="answer-letter">{choice.letter}</span><BilingualText text={choice.text} answer /><AnswerSaving loading={pendingAnswer?.question===q.id&&pendingAnswer.letter===choice.letter}/></button>;
          })}</div>
          {error && <p role="alert" className="answer-error">{error}</p>}

          {done === total && <p className="topic-completed">{reviewParent ? 'Работа над ошибками завершена' : topic.kind === 'new' ? 'Новые вопросы пройдены' : 'Тема пройдена'}. Верно: {correct} из {total}. Можно вернуться к списку тем.</p>}
          </div>
          </div>
          <footer className="question-navigation">{q.progress && <AnswerResult correct={q.progress.correct} letter={q.progress.correct_letter} onHelp={() => setHelpOpen(true)} />}<div className="controls topic-controls"><button className="secondary" disabled={previousPosition < 0 || busy} onClick={() => goTo(previousPosition)}>← Предыдущий</button><NextQuestionButton key={q.id} label={finishReady ? reviewParent ? 'Завершить работу над ошибками' : topic.kind === 'new' ? 'Завершить новые вопросы' : 'Завершить тему' : 'Следующий вопрос →'} disabled={busy || !q.progress} imageUrl={topic.questions[nextPosition]?.image_url} onNext={() => finishReady ? onFinish(q.id) : goTo(nextPosition)} /></div>
          <button className="return-link" type="button" disabled={busy} onClick={onBack}><span className="return-arrow" aria-hidden="true">↩</span><span className="return-text">Вернуться</span></button></footer>
        </div>
      </section>
    </main>
    {helpOpen && <Modal eyebrow={`${topic.title} · вопрос ${q.number}`} title="Объяснение" onClose={() => setHelpOpen(false)}><div className="help-languages">{(['ru', 'es', 'en'] as HelpLanguage[]).map((lang) => <button key={lang} onClick={() => setLanguage(lang)} className={language === lang ? 'active' : ''}>{lang.toUpperCase()}</button>)}</div><HelpContent text={help || 'Объяснение отсутствует в источнике.'} /></Modal>}
  </TrainingFrame>;
}

function TrainerApp() {
  const {route,navigate}=useTrainingNavigation();
  const [library,setLibrary]=useState<LibraryData|null>(null);
  const [loadError,setLoadError]=useState('');
  const [libraryView,setLibraryView]=useState<LibraryView>(()=>window.history.state?.libraryView??{search:'',expanded:[],scrollTop:0});
  const home=route.screen==='home';
  useEffect(()=>{
    document.documentElement.classList.toggle('trainer-viewport',!home);
    return()=>document.documentElement.classList.remove('trainer-viewport');
  },[home]);
  useEffect(()=>{
    const controller=new AbortController();
    fetch('/library.json',{cache:'no-store',signal:controller.signal}).then(response=>{
      if(!response.ok)throw Error();return response.json() as Promise<LibraryData>;
    }).then(setLibrary).catch(()=>{if(!controller.signal.aborted)setLoadError('Не удалось загрузить темы. Обновите страницу.');});
    return()=>controller.abort();
  },[]);
  useEffect(()=>{
    const restore=(event:PopStateEvent)=>{
      const saved=event.state?.libraryView;
      if(route.screen==='topic'&&readRoute().screen==='home'&&library){
        const snapshot=topicReturnState(saved??libraryView,route.id,library.nodes);
        setLibraryView(snapshot);window.history.replaceState({...window.history.state,libraryView:snapshot},'');
      }else if(saved)setLibraryView(saved);
    };
    window.addEventListener('popstate',restore);return()=>window.removeEventListener('popstate',restore);
  },[route,library,libraryView]);
  const onQuestion=useCallback((question:string,replace=false,runId?:number,reviewParent?:number)=>{
    if(route.screen==='topic')navigate({...route,question,runId:runId??route.runId,reviewParent:reviewParent??route.reviewParent},replace);
  },[route,navigate]);
  function remember(snapshot:LibraryView){setLibraryView(snapshot);window.history.replaceState({...window.history.state,libraryView:snapshot},'');}
  function openTopic(id:string,question?:string,options?:TopicLaunch){navigate({screen:'topic',id,question,...options});}
  function returnToLibrary(id:string){
    const snapshot=topicReturnState(libraryView,id,library?.nodes??[]);
    navigate({screen:'home'});remember(snapshot);
  }
  if(loadError)return <main className="loading error-message">{loadError}</main>;
  if(!library)return <PageLoader/>;
  if(route.screen==='statistics')return <LinearStatistics onBack={()=>navigate({screen:'home'})} onOpenTopic={id=>{
    const snapshot=topicReturnState(libraryView,id,library.nodes);navigate({screen:'home'});remember(snapshot);
  }}/>;
  if(route.screen==='topic'){
    const node=library.nodes.find(item=>item.id===route.id);
    const ancestors:string[]=[];let parent=node?.parent_id;
    while(parent){const ancestor=library.nodes.find(item=>item.id===parent);if(!ancestor)break;ancestors.unshift(ancestor.label_ru);parent=ancestor.parent_id;}
    return <TopicTrainer key={`${route.id}:${route.reviewParent??(route.newQuestions?'new':'full')}`} topicId={route.id} initialQuestion={route.question} reviewParent={route.reviewParent} newQuestions={route.newQuestions} runId={route.runId} path={ancestors.join(' / ')} onQuestion={onQuestion} sectionNavigation={<SectionNavigator library={library} sectionId={node?.parent_id??route.id} onOpenTopic={openTopic}/>} onBack={()=>returnToLibrary(route.id)} onFinish={()=>returnToLibrary(route.id)}/>;
  }
  return <QuestionLibrary library={library} view={libraryView} onRemember={remember} onOpenTopic={openTopic} onStatistics={()=>navigate({screen:'statistics'})}/>;
}

function AuthenticatedApp() {
  const [user, setUser] = useState<AuthUser | null>(null);
  const [checkingSession, setCheckingSession] = useState(true);

  useEffect(() => {
    api<AuthUser>('/api/auth/me')
      .then(setUser)
      .catch(() => setUser(null))
      .finally(() => setCheckingSession(false));
  }, []);

  async function logout() {
    await api('/api/auth/logout', { method: 'POST' }).catch(() => undefined);
    setUser(null);
  }

  if (checkingSession) return <PageLoader />;
  if (!user) return <LoginScreen onAuthenticated={setUser} />;
  return <><AccountMenu key={`account:${user.id}`} user={user} onLogout={logout} /><TrainerApp key={user.id} /></>;
}

export default function App() {
  return <PreferencesProvider><AuthenticatedApp /></PreferencesProvider>;
}

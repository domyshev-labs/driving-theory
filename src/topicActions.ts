type ReviewRun = {
 id:number;
 kind:'full'|'new';
 wrong:number;
 review_pending?:number;
 completed_at:string|null;
 review?:{completed_at:string|null};
};

// Reviews belong to a particular run. A fresh full repeat starts a new cycle.
export function topicReviewState<T extends ReviewRun>(runs:T[]) {
 const full=runs.find(run=>run.kind==='full');
 const failed=runs.filter(run=>run.id>=(full?.id??0)&&run.completed_at&&run.wrong>0);
 const outstanding=failed.filter(run=>(run.review_pending??(run.review?.completed_at?0:run.wrong))>0);
 const pending=outstanding.find(run=>run.review&&!run.review.completed_at)??outstanding.find(run=>!run.review);
 return {pending,completed:failed.length>0&&outstanding.length===0};
}

type ContinuationProgress={remaining:number;new_count:number;total:number;answered:number;next_question:number};
const questionForms=new Intl.PluralRules('ru');
const questions=(count:number)=>`${count} ${questionForms.select(count)==='one'?'вопрос':questionForms.select(count)==='few'?'вопроса':'вопросов'}`;

export function topicErrorLabel(count:number) {return `${questions(count)} с ${questionForms.select(count)==='one'?'ошибкой':'ошибками'}`;}

export function topicContinuation(progress:ContinuationProgress) {
 const existing=Math.max(0,progress.remaining);
 const added=Math.max(0,progress.new_count);
 const remaining=existing+added;
 if(!remaining)return {
  target:'repeat' as const,
  label:'Тема пройдена',
  tooltip:`Вы ответили на все ${questions(progress.total)}. «Пройти тему снова» начнёт новое прохождение. Предыдущие результаты останутся в истории.`,
 };
 if(!existing&&added)return {
  target:'new' as const,
  label:`Тема пройдена · ${questionForms.select(added)==='one'?'появился':'появилось'} ${added} ${questionForms.select(added)==='one'?'новый вопрос':questionForms.select(added)==='few'?'новых вопроса':'новых вопросов'}`,
  tooltip:`Вы уже прошли эту тему. Добавлено ${questions(added)}. «Пройти новые вопросы» откроет только новые вопросы; предыдущие результаты сохранятся.`,
 };
 const totalSuffix=remaining===progress.total?'':` из ${progress.total}`;
 return {
  target:'main' as const,
  label:added?`Осталось ${remaining}${totalSuffix}, из них ${added} новых`:`Осталось ${questions(remaining)}${totalSuffix}`,
  tooltip:`Не отвечено: ${questions(remaining)} из ${progress.total}. Продолжение с вопроса ${progress.next_question}; отвеченные вопросы пропускаются.${added?` Сначала оставшиеся вопросы основного прохождения (${existing}), затем новые (${added}) — отдельным набором с сохранением статистики.`:''}`,
 };
}

export function topicReturnState<T extends {expanded:string[]}>(view:T,id:string,nodes:{id:string;parent_id?:string|null}[]) {
 const parents:string[]=[];
 let parent=nodes.find(node=>node.id===id)?.parent_id;
 while(parent&&!parents.includes(parent)) {parents.push(parent);parent=nodes.find(node=>node.id===parent)?.parent_id;}
 return {...view,search:'',expanded:[...new Set([...view.expanded,...parents,id])],recentTopic:id,completedTopic:undefined};
}

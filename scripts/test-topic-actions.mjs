import test from 'node:test';
import assert from 'node:assert/strict';
import {topicReviewState,topicErrorLabel,topicContinuation,topicReturnState} from '../src/topicActions.ts';
const run=(id,kind='full',review)=>({id,kind,wrong:2,completed_at:'2026-10-27T10:00:00Z',review});
test('completed review is retained without another invitation',()=>{
 const state=topicReviewState([run(1,'full',{completed_at:'2026-10-27T11:00:00Z'})]);
 assert.equal(state.pending,undefined);assert.equal(state.completed,true);
});
test('unfinished review resumes before a different unstarted batch',()=>{
 const state=topicReviewState([run(3,'new'),run(2,'new',{completed_at:null}),run(1)]);
 assert.equal(state.pending.id,2);assert.equal(state.completed,false);
});
test('a completed review for one batch does not hide errors in another',()=>{
 const state=topicReviewState([run(2,'new'),run(1,'full',{completed_at:'2026-10-27T11:00:00Z'})]);
 assert.equal(state.pending.id,2);assert.equal(state.completed,false);
});
test('new full repeat does not invite review of older cycles',()=>{
 const state=topicReviewState([{...run(3),completed_at:null},run(1)]);
 assert.equal(state.pending,undefined);assert.equal(state.completed,false);
});
test('no errors does not offer a review',()=>{
 assert.deepEqual(topicReviewState([{...run(1),wrong:0}]),{pending:undefined,completed:false});
});

const progress={remaining:7,new_count:0,total:20,answered:13,next_question:4};
test('ordinary continuation describes remaining questions separately',()=>{
 const state=topicContinuation(progress);assert.equal(state.target,'main');assert.equal(state.label,'Осталось 7 вопросов из 20');
});
test('mixed continuation prioritizes the main run while showing all pending questions',()=>{
 const state=topicContinuation({...progress,new_count:3,total:23});assert.equal(state.target,'main');assert.equal(state.label,'Осталось 10 из 23, из них 3 новых');assert.match(state.tooltip,/отдельным набором/);
 assert.equal(topicContinuation({...progress,remaining:1,new_count:2,total:3,answered:0}).label,'Осталось 3, из них 2 новых');
});
test('only new questions resume the separate new-question batch',()=>{
 const state=topicContinuation({...progress,remaining:0,new_count:3});assert.equal(state.target,'new');assert.match(state.label,/Тема пройдена/);assert.match(state.tooltip,/3 вопроса/);
});
test('completed topics offer a new run while retaining historical results',()=>{
 const state=topicContinuation({...progress,remaining:0});assert.equal(state.target,'repeat');assert.equal(state.label,'Тема пройдена');assert.match(state.tooltip,/Предыдущие результаты останутся в истории/);
});
test('unstarted topics use the same continuation action',()=>{
 const state=topicContinuation({...progress,remaining:20,answered:0,next_question:1});assert.equal(state.target,'main');assert.equal(state.label,'Осталось 20 вопросов');
});

test('return opens the worked topic and ancestors while retaining other branches',()=>{
 const state=topicReturnState({expanded:['other','leaf'],search:'old filter',completedTopic:{id:'old'}},'leaf',[{id:'section'},{id:'middle',parent_id:'section'},{id:'leaf',parent_id:'middle'}]);
 assert.deepEqual(state.expanded,['other','leaf','middle','section']);assert.equal(state.recentTopic,'leaf');assert.equal(state.search,'');assert.equal(state.completedTopic,undefined);
});
test('working with a different topic replaces the recent marker',()=>{
 const state=topicReturnState({expanded:['first','second'],recentTopic:'first'},'second',[{id:'second'}]);
 assert.equal(state.recentTopic,'second');assert.deepEqual(state.expanded,['first','second']);
});

test('new-only chip includes its count with Russian plural forms',()=>{
 for(const [count,label] of [[1,'появился 1 новый вопрос'],[2,'появилось 2 новых вопроса'],[5,'появилось 5 новых вопросов'],[21,'появился 21 новый вопрос']]) {
  const state=topicContinuation({...progress,remaining:0,new_count:count});
  assert.equal(state.label,`Тема пройдена · ${label}`);assert.match(state.tooltip,/Пройти новые вопросы/);
 }
});

test('reviewed event inherited by the full run does not invite another review',()=>{
 const state=topicReviewState([
  {...run(2,'new',{completed_at:'2026-10-28T14:13:00Z'}),review_pending:0},
  {...run(1),review_pending:0},
 ]);
 assert.equal(state.pending,undefined);assert.equal(state.completed,true);
});
test('unrelated errors still require review after an inherited duplicate is covered',()=>{
 const state=topicReviewState([
  {...run(2,'new',{completed_at:'2026-10-28T14:13:00Z'}),review_pending:0},
  {...run(1),review_pending:1},
 ]);
 assert.equal(state.pending.id,1);assert.equal(state.completed,false);
});

test('error chip displays the exact current review count with Russian plural forms',()=>{
 assert.equal(topicErrorLabel(1),'1 вопрос с ошибкой');assert.equal(topicErrorLabel(2),'2 вопроса с ошибками');assert.equal(topicErrorLabel(5),'5 вопросов с ошибками');
});

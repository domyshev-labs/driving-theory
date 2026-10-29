import test from 'node:test';
import assert from 'node:assert/strict';
import {readRoute,routeURL} from '../src/navigation.ts';

test('current-error review session survives URL round trips and refreshes',()=>{
 globalThis.window={location:{pathname:'/'}};
 const route={screen:'topic',id:'numbers.dimensions',question:'example',reviewParent:-123};
 const url=routeURL(route);
 assert.deepEqual(readRoute(url.slice(1)),{...route,runId:undefined,newQuestions:false});
});

test('starting a topic-wide review does not become normal training after navigation',()=>{
 const route=readRoute('?screen=topic&id=numbers.dimensions&review=-1');
 assert.equal(route.reviewParent,-1);
 assert.equal(readRoute('?screen=topic&id=numbers.dimensions&review=74').reviewParent,74);
});

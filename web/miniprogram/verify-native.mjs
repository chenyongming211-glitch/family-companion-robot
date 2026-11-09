import assert from 'node:assert/strict';
import {readFile,access} from 'node:fs/promises';
import vm from 'node:vm';
const config=JSON.parse(await readFile('dist/app.json','utf8'));
let page;
const context={Page:p=>{page=p;},BigInt:undefined,wx:{},setTimeout,clearTimeout,require:()=>assert.fail('Security2 must not load before capability check')};
const main=await readFile('dist/pages/index/index.js','utf8');
assert.ok(!/\b\d+n\b/.test(main),'entry bundle must parse on engines without BigInt literals');
vm.runInNewContext(main,context);
assert.equal(typeof page.onLoad,'function');
for(const route of config.pages){for(const ext of ['js','json','wxml'])await access('dist/'+route+'.'+ext);const wxml=await readFile('dist/'+route+'.wxml','utf8');for(const match of wxml.matchAll(/bind(?:tap|input|change)="([a-zA-Z0-9_]+)"/g))assert.equal(typeof page[match[1]],'function',match[1]);}
const crypto=await readFile('dist/provisioning/security2.js','utf8');
const module={exports:{}};
vm.runInNewContext(crypto,{module,exports:module.exports,Uint8Array,Uint32Array,BigInt,DataView,TextEncoder,console});
assert.equal(typeof module.exports.Security2Session,'function');
console.log('native artifact: routes, WXML handlers, BigInt-free initial bundle and deferred pure-JS Security2 module verified');

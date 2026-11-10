import {build} from 'esbuild';
import {cp,mkdir,rm} from 'node:fs/promises';
const pages=['home','device','chats','chat','settings','index'];
await rm('dist',{recursive:true,force:true});await mkdir('dist/pages/index',{recursive:true});
// Native CommonJS keeps literal require() visible to WeChat's dependency scan.
await build({entryPoints:['src/app.ts',...pages.map(p=>'src/pages/'+p+'/'+p+'.ts')],outdir:'dist',outbase:'src',bundle:true,platform:'browser',format:'cjs',external:['../../provisioning/security2.js'],target:'es2018',legalComments:'none',logLevel:'info'});
await build({entryPoints:['src/provisioning/security2.ts'],outfile:'dist/provisioning/security2.js',bundle:true,platform:'browser',format:'cjs',target:'es2018',legalComments:'none',logLevel:'info'});
for(const p of pages)await mkdir('dist/pages/'+p,{recursive:true});
for(const file of ['app.json','app.wxss',...pages.flatMap(p=>['pages/'+p+'/'+p+'.json','pages/'+p+'/'+p+'.wxml'])])await cp('src/'+file,'dist/'+file);
await cp('project.config.json','dist/project.config.json');

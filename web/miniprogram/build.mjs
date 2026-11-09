import {build} from 'esbuild';
import {cp,mkdir,rm} from 'node:fs/promises';
await rm('dist',{recursive:true,force:true});await mkdir('dist/pages/index',{recursive:true});
await build({entryPoints:['src/app.ts','src/pages/index/index.ts'],outdir:'dist',outbase:'src',bundle:true,platform:'browser',format:'iife',external:['../../provisioning/security2.js'],target:'es2018',legalComments:'none',logLevel:'info'});
await build({entryPoints:['src/provisioning/security2.ts'],outfile:'dist/provisioning/security2.js',bundle:true,platform:'browser',format:'cjs',target:'es2020',legalComments:'none',logLevel:'info'});
for(const file of ['app.json','app.wxss','pages/index/index.json','pages/index/index.wxml'])await cp('src/'+file,'dist/'+file);
await cp('project.config.json','dist/project.config.json');

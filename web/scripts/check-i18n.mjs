import fs from 'node:fs';
import path from 'node:path';
import ts from 'typescript';
const catalog=JSON.parse(fs.readFileSync(new URL('../src/lib/i18n/messages.json',import.meta.url)));
const technical=new Set(['English','.aib','.proto.gz','.tachibk','/data/manga/en','JPEG','Komga','MANGARR_DB','MANGARR_MODE=worker','MANGARR_SERVER_URL','MANGARR_WORKER_KEY','SSL','WebP','en','env','gpu-box','https://auth.example.com','https://manga.example.com:25600','https://…/index.min.json','keep','mangarr','openid','postgres://mangarr:secret@postgres:5432/mangarr?sslmode=disable','postgres://user:password@host:5432/database?sslmode=disable','preferred_username','px','v']);
const errors=[];
function walk(dir){for(const e of fs.readdirSync(dir,{withFileTypes:true})){const p=path.join(dir,e.name);if(e.isDirectory())walk(p);else if(p.endsWith('.tsx')){
 const f=ts.createSourceFile(p,fs.readFileSync(p,'utf8'),ts.ScriptTarget.Latest,true,ts.ScriptKind.TSX);
 function visit(n){let value;
  if(ts.isJsxText(n))value=n.text.replace(/\s+/g,' ').trim();
  if(ts.isJsxAttribute(n)&&/^(title|label|placeholder|subtitle|help|aria-label|confirmLabel)$/.test(n.name.text)&&n.initializer&&ts.isStringLiteral(n.initializer))value=n.initializer.text;
  if(value&&/[a-zA-Z]/.test(value)&&!technical.has(value))errors.push(`${p}: untranslated literal ${value}`);
  ts.forEachChild(n,visit);
 }visit(f);
}}}
for(const [key,row]of Object.entries(catalog))for(const locale of ['ru','uk'])if(!row[locale])errors.push(`Missing ${locale}: ${key}`);
walk(new URL('../src',import.meta.url).pathname);
if(errors.length){console.error(errors.join('\n'));process.exit(1)}
console.log(`Checked ${Object.keys(catalog).length} messages and static JSX coverage.`);

// Colours come from the theme tokens in src/index.css (bg, panel, fg, muted,
// accent, primary…), so a theme or contrast change reaches every page. This
// rejects Tailwind's raw palette classes (text-orange-400, bg-neutral-800…).
import fs from 'node:fs';
import path from 'node:path';
const palette='slate|gray|zinc|neutral|stone|red|orange|amber|yellow|lime|green|emerald|teal|cyan|sky|blue|indigo|violet|purple|fuchsia|pink|rose';
const re=new RegExp(`(?<![\\w-])(?:[\\w-]+:)*(?:bg|text|border|ring|outline|fill|stroke|accent|from|via|to|divide|placeholder|decoration|shadow|caret)-(?:${palette})-\\d{2,3}(?:/\\d+)?(?![\\w-])`,'g');
const errors=[];
function walk(dir){for(const e of fs.readdirSync(dir,{withFileTypes:true})){const p=path.join(dir,e.name);if(e.isDirectory())walk(p);else if(/\.(tsx?|css)$/.test(p)){
  fs.readFileSync(p,'utf8').split('\n').forEach((line,i)=>{for(const m of line.matchAll(re))errors.push(`${p}:${i+1}: ${m[0]} (use a theme token)`);});
}}}
walk(new URL('../src',import.meta.url).pathname);
if(errors.length){console.error(errors.join('\n'));process.exit(1)}
console.log('No raw palette colours.');

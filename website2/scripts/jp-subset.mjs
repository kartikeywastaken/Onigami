// Re-subsets Shippori Mincho B1 800 to the Japanese characters used in src/. Run after changing copy.
import fs from 'node:fs';
import path from 'node:path';
const files = [];
const walk = (d) => fs.readdirSync(d, { withFileTypes: true }).forEach((e) => (e.isDirectory() ? walk(path.join(d, e.name)) : files.push(path.join(d, e.name))));
walk('src');
const text = files.map((f) => fs.readFileSync(f, 'utf8')).join('');
const chars = [...new Set([...text].filter((c) => /[　-鿿＀-￯]/.test(c)))].sort().join('');
const UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0 Safari/537.36';
const css = await (await fetch(`https://fonts.googleapis.com/css2?family=Shippori+Mincho+B1:wght@800&text=${encodeURIComponent(chars)}&display=swap`, { headers: { 'user-agent': UA } })).text();
const url = css.match(/url\((https:[^)]+)\)/)?.[1];
if (!url) throw new Error('no font url in Google response');
const buf = Buffer.from(await (await fetch(url)).arrayBuffer());
fs.writeFileSync('public/fonts/shippori-mincho-b1-800-subset.woff2', buf);
console.log(`${chars.length} characters, ${(buf.length / 1024).toFixed(1)} KB: ${chars}`);

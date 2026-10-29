// A toy prompt that runs in the browser, so the hero garden can answer. It is a playground
// for the growth metaphor, not the real Onigami (which runs an engagement in your terminal).

export type Reply = { out: string[]; replant?: string; go?: string; exit?: boolean; clear?: boolean; err?: boolean };

const BUILTINS = ['echo', 'exit', 'type', 'pwd', 'cd', 'help', 'clear', 'history'];
const PROGRAMS: Record<string, string> = { ls: '/bin/ls', cat: '/bin/cat', git: '/usr/bin/git', go: '/usr/bin/go', nmap: '/usr/bin/nmap', sqlmap: '/usr/bin/sqlmap', vim: '/usr/bin/vim', grep: '/usr/bin/grep', man: '/usr/bin/man' };
const PAGES: Record<string, string> = { '~': '/', '/': '/', home: '/' };

let cwd = '~';
const hist: string[] = [];

/** Splits a line the way the shell will: spaces cut, quotes protect, a backslash escapes one character. */
export function split(line: string): string[] {
  const words: string[] = [];
  let cur = '', q: string | null = null, has = false;
  for (let i = 0; i < line.length; i++) {
    const c = line[i];
    if (q) { if (c === q) q = null; else if (c === '\\' && q === '"' && /["\\$]/.test(line[i + 1] || '')) cur += line[++i]; else cur += c; continue; }
    if (c === '"' || c === "'") { q = c; has = true; continue; }
    if (c === '\\') { cur += line[++i] ?? ''; has = true; continue; }
    if (/\s/.test(c)) { if (cur || has) words.push(cur); cur = ''; has = false; continue; }
    cur += c; has = true;
  }
  if (cur || has) words.push(cur);
  return words;
}

export function run(line: string): Reply {
  const argv = split(line);
  if (!argv.length) return { out: [] };
  hist.push(line.trim());
  const [cmd, ...args] = argv;
  switch (cmd) {
    case 'echo': return { out: [args.join(' ')], replant: args.join(' ') };
    case 'exit': return { out: ['さようなら. The garden goes to sleep.'], exit: true };
    case 'pwd': return { out: [cwd === '~' ? '/home/kami' : `/home/kami/${cwd}`] };
    case 'onigami': return { out: ['onigami: the crew runs in your terminal, not here. Clone it and give it a scope.'] };
    case 'recon': case 'scan': return { out: [`${cmd}: needs a real, authorized target. Not in this garden.`] };
    case 'clear': return { out: [], clear: true };
    case 'help': return { out: [`builtins: ${BUILTINS.join(' ')}`, 'try: echo hello, type nmap, pwd'] };
    case 'history': return { out: hist.slice(-5).map((h, i, a) => `${String(hist.length - a.length + i + 1).padStart(4)}  ${h}`) };
    case 'type': {
      if (!args.length) return { out: [] };
      return { out: args.map((a) => BUILTINS.includes(a) ? `${a} is a shell builtin` : PROGRAMS[a] ? `${a} is ${PROGRAMS[a]}` : `${a}: not found`) };
    }
    case 'cd': {
      const to = (args[0] ?? '~').replace(/\/+$/, '').replace(/^\.\//, '') || '/';
      if (PAGES[to]) { cwd = '~'; return { out: [], go: PAGES[to] }; }
      return { out: [`cd: ${args[0]}: No such file or directory`], err: true };
    }
    default:
      if (PROGRAMS[cmd]) return { out: [`${cmd}: the browser has no ${PROGRAMS[cmd]}. Clone Onigami to run it.`], err: true };
      return { out: [`${cmd}: command not found`], err: true };
  }
}

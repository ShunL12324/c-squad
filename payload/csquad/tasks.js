#!/usr/bin/env node
// Background tasks for Claude Code: sessions started with `claude --bg` and
// named "T12 · title". Used by the create-task, task-status, message-task and
// finish-task skills and by the status line.
//
//   tasks.js launch --title TITLE [--cwd DIR] [--model M] < PROMPT
//   tasks.js status [--all]        (this session's tasks; --all lists every task)
//   tasks.js peek T12 [-n N] [--all]   (refuses tasks other sessions launched)
//   tasks.js watch                 (blocks until one of this session's tasks needs input, finishes or fails)
//   tasks.js statusline            (status line command; reads Claude's JSON)
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync, spawnSync } = require('child_process');

const HERE = __dirname;
// Installed at <claude dir>/csquad, so the Claude config directory is one level up.
const CLAUDE_HOME = path.dirname(HERE);
const NAME = /^T(\d+) · (.*)$/s;
const ANSI = /\x1b\[[0-9;?]*[ -/]*[@-~]/g;

function readJSON(file, fallback) {
  try {
    return JSON.parse(fs.readFileSync(file, 'utf8'));
  } catch {
    return fallback;
  }
}

const config = { permissionMode: 'bypassPermissions', model: '', ...readJSON(path.join(HERE, 'config.json'), {}) };

function claude(args, opts = {}) {
  return execFileSync('claude', args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], timeout: 60000, ...opts });
}

// tasks returns background sessions named like tasks, newest first.
function tasks() {
  const sessions = JSON.parse(claude(['agents', '--json', '--all']));
  const out = [];
  for (const s of sessions) {
    const m = s.kind === 'background' && s.name && s.name.match(NAME);
    if (m) out.push({ ...s, num: Number(m[1]), label: 'T' + m[1], title: m[2] });
  }
  return out.sort((a, b) => b.num - a.num);
}

function within(dir, root) {
  return dir === root || dir.startsWith(root.endsWith(path.sep) ? root : root + path.sep);
}

function canonical(dir) {
  try {
    return fs.realpathSync(path.resolve(dir));
  } catch {
    return path.resolve(dir);
  }
}

// ---- launch ---------------------------------------------------------------

// withLock runs fn while holding an exclusive lock, so concurrent launches
// see each other's sessions and never pick the same number.
function withLock(fn) {
  const lock = path.join(HERE, 'launch.lock');
  for (let i = 0; ; i++) {
    try {
      fs.closeSync(fs.openSync(lock, 'wx'));
      break;
    } catch (e) {
      if (e.code !== 'EEXIST') throw e;
      // A lock older than a launch can take belongs to a crashed one.
      try {
        if (Date.now() - fs.statSync(lock).mtimeMs > 150000) fs.rmSync(lock, { force: true });
      } catch {}
      if (i > 3000) throw new Error('timed out waiting for ' + lock);
      Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 50);
    }
  }
  try {
    return fn();
  } finally {
    fs.rmSync(lock, { force: true });
  }
}

function launch(args) {
  const title = args.title;
  if (!title) fail('--title is required');
  const cwd = canonical(args.cwd || process.cwd());
  if (!fs.statSync(cwd, { throwIfNoEntry: false })?.isDirectory()) fail(cwd + ' is not a directory');
  const prompt = fs.readFileSync(0, 'utf8').trim();
  if (!prompt) fail('the prompt (stdin) is empty');
  withLock(() => start(args, cwd, prompt));
}

// start launches the task numbered one above the highest task Claude Code
// lists; claude --bg returns once the new session is listed.
function start(args, cwd, prompt) {
  const num = Math.max(0, ...tasks().map((t) => t.num)) + 1;
  const label = 'T' + num;
  const name = `${label} · ${args.title}`;
  const system = fs.readFileSync(path.join(HERE, 'worker.md'), 'utf8') + `\nYour task ID is ${label}.\n`;
  const argv = ['--bg', '--name', name];
  if (config.permissionMode) argv.push('--permission-mode', config.permissionMode);
  const model = args.model || config.model;
  if (model) argv.push('--model', model);
  argv.push('--append-system-prompt', system, '--', prompt);
  const r = spawnSync('claude', argv, { cwd, encoding: 'utf8', timeout: 120000 });
  const output = ((r.stdout || '') + (r.stderr || '')).replace(ANSI, '').trim();
  const m = output.match(/backgrounded\s+·\s+([0-9a-f]{6,})/);
  if (r.status !== 0 || !m) fail(`launching ${name} failed:\n${output || r.error}`);
  // The "launched" marker lets the status line and status find, in the
  // launching session's transcript, which tasks that session started.
  console.log(`launched ${name} · session ${m[1]} · ${cwd}`);
}

// ---- transcripts ----------------------------------------------------------

function transcriptPath(sessionId) {
  const root = path.join(CLAUDE_HOME, 'projects');
  let best = null;
  for (const dir of fs.readdirSync(root, { withFileTypes: true })) {
    if (!dir.isDirectory()) continue;
    const file = path.join(root, dir.name, sessionId + '.jsonl');
    const st = fs.statSync(file, { throwIfNoEntry: false });
    if (st && (!best || st.mtimeMs > best.mtime)) best = { file, mtime: st.mtimeMs };
  }
  return best && best.file;
}

// messages returns the conversation without tool traffic: user and assistant
// text, with runs of tool calls folded into one "tools" entry.
function messages(sessionId) {
  const file = transcriptPath(sessionId);
  if (!file) return [];
  const out = [];
  let tools = null;
  for (const line of fs.readFileSync(file, 'utf8').split('\n')) {
    if (!line) continue;
    let e;
    try {
      e = JSON.parse(line);
    } catch {
      continue;
    }
    if ((e.type !== 'user' && e.type !== 'assistant') || e.isSidechain || e.isMeta) continue;
    const content = e.message && e.message.content;
    const blocks = typeof content === 'string' ? [{ type: 'text', text: content }] : content || [];
    for (const b of blocks) {
      if (b.type === 'tool_use') {
        if (!tools) out.push((tools = { role: 'tools', counts: {} }));
        tools.counts[b.name] = (tools.counts[b.name] || 0) + 1;
      } else if (b.type === 'text') {
        let text = (b.text || '').replace(/<system-reminder>[\s\S]*?<\/system-reminder>/g, '').trim();
        // Harness wrappers such as <command-name> or <task-notification>.
        if (!text || /^<[a-z][a-z0-9-]*>/.test(text)) continue;
        tools = null;
        out.push({ role: e.type, text });
      }
    }
  }
  return out;
}

function lastAssistant(msgs) {
  for (let i = msgs.length - 1; i >= 0; i--) if (msgs[i].role === 'assistant') return msgs[i].text;
  return '';
}

// launchedIds returns the ids of the tasks a transcript records as launched.
// It scans the raw bytes for the marker and parses only around each hit.
function launchedIds(buf) {
  const ids = new Set();
  const marker = Buffer.from('launched T');
  for (let i = buf.indexOf(marker); i !== -1; i = buf.indexOf(marker, i + 1)) {
    let line = buf.toString('utf8', i, Math.min(buf.length, i + 1000));
    // The record ends at a newline, an escaped \n (JSONL) or an unescaped
    // closing quote; a quote inside the title is escaped as \" and kept.
    line = line.split(/\n|\\n|(?<!\\)"/, 1)[0];
    const m = line.match(/^launched T(\d+) · .*? · session ([0-9a-f]{6,})/);
    if (m) ids.add(m[2]);
  }
  return ids;
}

// predecessorOf returns the id of the session that was continued in `id`: the
// transcript in `dir` whose last line is a continued-in record pointing at it.
// Only the tail of each file is read.
function predecessorOf(dir, id) {
  let names;
  try {
    names = fs.readdirSync(dir);
  } catch {
    return null;
  }
  const buf = Buffer.alloc(2048);
  for (const name of names) {
    if (!name.endsWith('.jsonl')) continue;
    let fd;
    try {
      fd = fs.openSync(path.join(dir, name), 'r');
      const n = fs.readSync(fd, buf, 0, buf.length, Math.max(0, fs.fstatSync(fd).size - buf.length));
      const last = buf.toString('utf8', 0, n).trimEnd().split('\n').pop();
      if (!last.includes('continued-in')) continue;
      const e = JSON.parse(last);
      if (e.type === 'continued-in' && e.continuedInSessionId === id) return name.slice(0, -'.jsonl'.length);
    } catch {
      // Unreadable or partial line: not a predecessor.
    } finally {
      if (fd !== undefined) fs.closeSync(fd);
    }
  }
  return null;
}

// historyOf returns the transcripts holding a session's history. Normally that
// is `file`. When Claude continues a conversation in a new session (leaving
// and reopening from agent view), the new transcript only appears with the
// next message; until then the history is in the predecessors, so follow
// continued-in records backwards (an intermediate file may be a stub).
function historyOf(file, id) {
  if (fs.existsSync(file)) return [file];
  const dir = path.dirname(file);
  const out = [];
  const seen = new Set();
  for (let depth = 0; depth < 10 && id && !seen.has(id); depth++) {
    seen.add(id);
    id = predecessorOf(dir, id);
    if (id) out.push(path.join(dir, id + '.jsonl'));
  }
  return out;
}

// historyOfSession is historyOf for a session id whose transcript location is unknown.
function historyOfSession(sid) {
  const file = transcriptPath(sid);
  if (file) return [file];
  const root = path.join(CLAUDE_HOME, 'projects');
  try {
    for (const d of fs.readdirSync(root, { withFileTypes: true })) {
      const hist = d.isDirectory() ? historyOf(path.join(root, d.name, sid + '.jsonl'), sid) : [];
      if (hist.length) return hist;
    }
  } catch {}
  return [];
}

function ownTasks(list, transcripts) {
  const ids = new Set();
  for (const file of transcripts) {
    try {
      for (const id of launchedIds(fs.readFileSync(file))) ids.add(id);
    } catch {}
  }
  return list.filter((t) => [...ids].some((id) => t.id === id || t.id.startsWith(id)));
}

// ---- status and peek ------------------------------------------------------

const STATE = { blocked: 'needs input', working: 'working', done: 'done', failed: 'failed', stopped: 'stopped' };

function stateOf(t) {
  return STATE[t.state] || t.state || t.status || 'unknown';
}

function age(ms) {
  const s = Math.max(0, Math.round((Date.now() - ms) / 1000));
  if (s < 60) return s + 's';
  if (s < 3600) return Math.round(s / 60) + 'm';
  if (s < 86400) return Math.round(s / 3600) + 'h';
  return Math.round(s / 86400) + 'd';
}

function shortDir(dir) {
  const home = os.homedir();
  return within(dir, home) ? '~' + dir.slice(home.length) : dir;
}

function status(args) {
  let list = tasks();
  const sid = process.env.CLAUDE_CODE_SESSION_ID;
  if (!args.all && sid) {
    list = ownTasks(list, historyOfSession(sid));
  }
  if (!list.length) {
    console.log(args.all || !sid ? 'No tasks.' : 'No tasks launched by this session (status --all lists every task).');
    return;
  }
  const order = ['needs input', 'failed', 'done', 'working', 'stopped'];
  list.sort((a, b) => order.indexOf(stateOf(a)) - order.indexOf(stateOf(b)) || a.num - b.num);
  for (const t of list) {
    const last = lastAssistant(messages(t.sessionId)) || '(no reply yet)';
    console.log(`## ${t.label} · ${t.title}`);
    console.log(`state: ${stateOf(t)} · started ${age(t.startedAt)} ago · session ${t.id} · ${shortDir(t.cwd)}`);
    console.log(`latest:\n${clip(last, 1200)}\n`);
  }
}

function find(label) {
  const m = String(label || '').match(/^[Tt]?(\d+)$/);
  if (!m) fail('expected a task like T12');
  const t = tasks().find((x) => x.num === Number(m[1]));
  if (!t) fail(`T${m[1]} is not listed by claude agents (removed or never started)`);
  return t;
}

function peek(args) {
  const t = find(args._[0]);
  const sid = process.env.CLAUDE_CODE_SESSION_ID;
  if (!args.all && sid && !ownTasks([t], historyOfSession(sid)).length) {
    fail(`${t.label} was launched by another session; pass --all to peek it anyway`);
  }
  const n = Number(args.n) || 6;
  console.log(`${t.label} · ${t.title} · ${stateOf(t)} · session ${t.id} · ${shortDir(t.cwd)}\n`);
  for (const m of messages(t.sessionId).slice(-n)) {
    if (m.role === 'tools') {
      console.log('[tools] ' + Object.entries(m.counts).map(([k, v]) => `${k} ×${v}`).join(', ') + '\n');
    } else {
      console.log(`[${m.role}]\n${clip(m.text, 3000)}\n`);
    }
  }
}

function clip(text, max) {
  return text.length > max ? text.slice(0, max - 1) + '…' : text;
}

// ---- watch ----------------------------------------------------------------

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

// lockWatcher makes sure one watcher runs per session. It returns the lock
// file, or null when a live watcher already holds it; a lock whose pid is dead
// is taken over.
function lockWatcher(sid) {
  const file = path.join(HERE, `watch-${sid.replace(/[^\w-]/g, '_')}.pid`);
  for (let i = 0; i < 5; i++) {
    try {
      fs.writeFileSync(file, String(process.pid), { flag: 'wx' });
      return file;
    } catch (e) {
      if (e.code !== 'EEXIST') throw e;
    }
    let pid = 0;
    try {
      pid = Number(fs.readFileSync(file, 'utf8')) || 0;
    } catch {}
    let alive = false;
    try {
      alive = pid > 0 && (process.kill(pid, 0), true);
    } catch (e) {
      alive = e.code === 'EPERM';
    }
    if (alive) return null;
    fs.rmSync(file, { force: true });
  }
  return null;
}

// The states a task is reported for when it changes into one of them.
const EVENTS = new Set(['blocked', 'done', 'failed', 'stopped']);

async function watch() {
  const sid = process.env.CLAUDE_CODE_SESSION_ID;
  if (!sid) {
    console.log('not watching: CLAUDE_CODE_SESSION_ID is not set, so this session\'s tasks are unknown');
    return;
  }
  const lock = lockWatcher(sid);
  if (!lock) {
    console.log('already watching');
    return;
  }
  const cleanup = () => fs.rmSync(lock, { force: true });
  process.on('exit', cleanup);
  for (const sig of ['SIGTERM', 'SIGINT']) process.on(sig, () => process.exit(0));

  const interval = Number(process.env.CSQUAD_WATCH_INTERVAL_MS) || 10000;
  const giveUp = Number(process.env.CSQUAD_WATCH_GIVEUP_MS) || 120000;
  const seen = new Map(); // task id -> state when last polled
  let first = true;
  let failingSince = 0;
  for (;; await sleep(interval)) {
    let own;
    try {
      own = ownTasks(tasks(), historyOfSession(sid));
      failingSince = 0;
    } catch (e) {
      failingSince = failingSince || Date.now();
      if (Date.now() - failingSince >= giveUp) {
        fail('giving up: claude agents kept failing: ' + String(e.stderr || e.message).replace(ANSI, '').trim());
      }
      continue;
    }
    const events = [];
    const current = new Set();
    for (const t of own) {
      current.add(t.id);
      const state = t.state;
      // A task first seen after the first poll was launched after the watcher
      // started, so it counts as having been working.
      const before = seen.has(t.id) ? seen.get(t.id) : first ? state : 'working';
      if (state !== before && EVENTS.has(state)) events.push(`${t.label} · ${t.title} · ${stateOf(t)}`);
      seen.set(t.id, state);
    }
    for (const id of seen.keys()) if (!current.has(id)) seen.delete(id);
    first = false;
    if (events.length) {
      console.log(events.join('\n'));
      return;
    }
    if (!own.some((t) => t.state === 'working')) {
      console.log('no open tasks');
      return;
    }
  }
}

// ---- status line ----------------------------------------------------------

const C = {
  reset: '\x1b[0m',
  bold: '\x1b[1m',
  chipBg: '\x1b[48;2;49;50;68m',
  idFg: '\x1b[38;2;205;214;244m',
  titleFg: '\x1b[38;2;166;173;200m',
  darkFg: '\x1b[38;2;17;17;27m',
  mutedFg: '\x1b[38;2;127;132;156m',
  badgeBg: '\x1b[48;2;69;71;90m',
  blueFg: '\x1b[38;2;137;180;250m',
};

// Badges by state; lower ranks come first.
const BADGES = {
  'needs input': [0, '\x1b[48;2;249;226;175m' + C.darkFg + C.bold, ' ◆ needs input '],
  done: [1, '\x1b[48;2;166;227;161m' + C.darkFg + C.bold, ' ✓ done '],
  failed: [2, '\x1b[48;2;243;139;168m' + C.darkFg + C.bold, ' ✗ failed '],
  working: [3, C.badgeBg + C.blueFg, ' ● working '],
  stopped: [4, C.badgeBg + C.mutedFg, ' ■ stopped '],
};
const TITLE_WIDTH = 28;

function charWidth(cp) {
  return (cp >= 0x1100 && cp <= 0x115f) || (cp >= 0x2e80 && cp <= 0xa4cf) || (cp >= 0xac00 && cp <= 0xd7a3) ||
    (cp >= 0xf900 && cp <= 0xfaff) || (cp >= 0xfe30 && cp <= 0xfe4f) || (cp >= 0xff00 && cp <= 0xff60) ||
    (cp >= 0xffe0 && cp <= 0xffe6) || (cp >= 0x1f300 && cp <= 0x1faff) || (cp >= 0x20000 && cp <= 0x3fffd)
    ? 2
    : 1;
}

function width(s) {
  let w = 0;
  for (const ch of s) w += charWidth(ch.codePointAt(0));
  return w;
}

function truncate(s, max) {
  if (max <= 1) return '';
  if (width(s) <= max) return s;
  let out = '';
  let w = 0;
  for (const ch of s) {
    const cw = charWidth(ch.codePointAt(0));
    if (w + cw > max - 1) break;
    out += ch;
    w += cw;
  }
  return out + '…';
}

function pad(s, w) {
  return s + ' '.repeat(Math.max(0, w - width(s)));
}

// grid lays out one chip per task: every cell as wide as the widest chip, as
// many columns as the terminal allows, most urgent tasks first.
function grid(list, columns) {
  const chips = list.map((t) => {
    const state = stateOf(t);
    const [rank, style, text] = BADGES[state] || [5, C.badgeBg + C.mutedFg, ` ${state} `];
    return { rank, num: t.num, label: ` ${t.label} `, title: t.title, style, text };
  });
  if (!chips.length) return '';
  chips.sort((a, b) => a.rank - b.rank || a.num - b.num);
  const labelW = Math.max(...chips.map((c) => width(c.label)));
  const badgeW = Math.max(...chips.map((c) => width(c.text)));
  let titleW = Math.max(...chips.map((c) => Math.min(width(c.title) + 1, TITLE_WIDTH)));
  titleW = Math.max(0, Math.min(titleW, columns - labelW - badgeW));
  const gap = 2;
  const cell = labelW + titleW + badgeW;
  const cols = Math.max(1, Math.floor((columns + gap) / (cell + gap)));
  const rows = [];
  for (let i = 0; i < chips.length; i += cols) {
    rows.push(chips.slice(i, i + cols).map((c) =>
      C.chipBg + C.idFg + C.bold + pad(c.label, labelW) + C.reset +
      C.chipBg + C.titleFg + pad(truncate(c.title, titleW - 1), titleW) +
      c.style + pad(c.text, badgeW) + C.reset).join(' '.repeat(gap)));
  }
  return rows.join('\n');
}

// statusline prints the user's own status line, saved at install time in
// statusline.json as {"present": bool, "statusLine": {...}}, then every task.
function statusline() {
  const input = fs.readFileSync(0);
  let transcript, sid;
  try {
    ({ transcript_path: transcript, session_id: sid } = JSON.parse(input.toString('utf8')));
  } catch {}
  const saved = readJSON(path.join(HERE, 'statusline.json'), {});
  const command = saved.statusLine && saved.statusLine.command;
  if (typeof command === 'string' && command) {
    const r = spawnSync('sh', ['-c', command], { input, encoding: 'utf8', timeout: 3000 });
    const own = (r.stdout || '').replace(/\n+$/, '');
    if (own) process.stdout.write(own + '\n');
  }
  let list;
  try {
    const id = typeof sid === 'string' && sid ? sid : typeof transcript === 'string' ? path.basename(transcript, '.jsonl') : '';
    list = typeof transcript === 'string' ? ownTasks(tasks(), historyOf(transcript, id)) : [];
  } catch {
    return;
  }
  const out = grid(list, Number(process.env.COLUMNS) || 120);
  if (out) process.stdout.write(out + '\n');
}

// ---- main -----------------------------------------------------------------

function fail(message) {
  console.error('tasks: ' + message);
  process.exit(1);
}

function parse(argv) {
  const args = { _: [] };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--all') args.all = true;
    else if (/^--?[a-z]+$/.test(a) && i + 1 < argv.length) args[a.replace(/^-+/, '')] = argv[++i];
    else args._.push(a);
  }
  return args;
}

const [command, ...rest] = process.argv.slice(2);
const commands = { launch, status, peek, watch, statusline };
if (!commands[command]) fail('usage: tasks.js launch|status|peek|watch|statusline');
const onError = (e) => {
  if (command === 'statusline') process.exit(0);
  fail(e.stderr ? String(e.stderr).replace(ANSI, '').trim() : e.message);
};
try {
  Promise.resolve(commands[command](parse(rest))).catch(onError);
} catch (e) {
  onError(e);
}

'use strict';
// tasks.js is installed first and run from the temporary config dir, with a
// fake `claude` executable on PATH.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');
const { test, before, after } = require('node:test');

const CLI = path.join(__dirname, '..', 'bin', 'csquad.js');

let root, dir, tasksJs, state, env;

// The fake claude keeps its sessions in a JSON file. `--bg` records the
// session before printing, as the real one does once the session is listed.
const FAKE = `#!/usr/bin/env node
const fs = require('fs');
const state = process.env.FAKE_STATE;
const read = () => JSON.parse(fs.readFileSync(state, 'utf8'));
const args = process.argv.slice(2);
if (args[0] === 'agents') {
  console.log(JSON.stringify(read()));
} else if (args.includes('--bg')) {
  const name = args[args.indexOf('--name') + 1];
  const id = Math.random().toString(16).slice(2, 10).padEnd(8, '0');
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 30);
  const sessions = read();
  sessions.push({ id, sessionId: id + '-0000', kind: 'background', name, state: 'working', cwd: process.cwd(), startedAt: Date.now() });
  fs.writeFileSync(state, JSON.stringify(sessions));
  console.log('\\x1b[32mbackgrounded\\x1b[0m · \\x1b[1m' + id + '\\x1b[0m · ' + name);
} else {
  process.exit(1);
}
`;

before(() => {
  root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'csquad-tasks-')));
  dir = path.join(root, 'claude');
  tasksJs = path.join(dir, 'csquad', 'tasks.js');
  state = path.join(root, 'sessions.json');
  const bin = path.join(root, 'bin');
  fs.mkdirSync(bin);
  fs.writeFileSync(path.join(bin, 'claude'), FAKE, { mode: 0o755 });
  env = { ...process.env, CLAUDE_CONFIG_DIR: dir, HOME: root, FAKE_STATE: state, PATH: bin + path.delimiter + process.env.PATH };
  delete env.CLAUDE_CODE_SESSION_ID;
  const r = spawnSync(process.execPath, [CLI, 'install'], { env, cwd: root, encoding: 'utf8' });
  assert.equal(r.status, 0, r.stdout + r.stderr);
});
after(() => fs.rmSync(root, { recursive: true, force: true }));

const session = (num, title, st = 'working') => ({
  id: String(num).padStart(8, 'a'), sessionId: 's' + num, kind: 'background', name: `T${num} · ${title}`, state: st, cwd: root, startedAt: Date.now(),
});
const setSessions = (list) => fs.writeFileSync(state, JSON.stringify(list));
const strip = (s) => s.replace(/\x1b\[[0-9;]*m/g, '');

// Display width with CJK and fullwidth characters counted as two columns.
const dw = (s) => [...s].reduce((w, ch) => w + (/[ᄀ-ᅟ⺀-꓏가-힣＀-｠]/.test(ch) ? 2 : 1), 0);

// transcript writes a JSONL transcript in which the session launched `list`.
function transcript(list, extra = []) {
  const file = path.join(root, 'transcript.jsonl');
  const lines = list.map((t) => JSON.stringify({ type: 'user', message: { content: [{ type: 'tool_result', content: `launched ${t.name} · session ${t.id} · ${root}\n` }] } }));
  fs.writeFileSync(file, [...lines, ...extra].join('\n') + '\n');
  return file;
}

function statusline(columns, input) {
  // By default the transcript launched every task in the state file.
  if (input === undefined) input = JSON.stringify({ transcript_path: transcript(JSON.parse(fs.readFileSync(state, 'utf8'))) });
  const r = spawnSync(process.execPath, [tasksJs, 'statusline'], { env: { ...env, COLUMNS: String(columns) }, input, encoding: 'utf8' });
  assert.equal(r.status, 0, r.stderr);
  return strip(r.stdout).split('\n').filter(Boolean);
}

test('statusline: column count follows COLUMNS', () => {
  setSessions([1, 2, 3, 4, 5].map((n) => session(n, 'job')));
  // A cell is 4 (label) + 4 ("job" + 1) + 11 (badge) = 19 wide, gaps are 2.
  assert.equal(statusline(40).length, 3); // 2 columns
  assert.equal(statusline(62).length, 2); // 3 columns
  assert.equal(statusline(104).length, 1); // 5 columns
  assert.equal(statusline(10).length, 5); // always at least 1 column
});

test('statusline: rows are aligned', () => {
  setSessions([session(1, 'a'), session(2, 'a much longer title for this one'), session(10, 'b', 'blocked'), session(3, 'c', 'done')]);
  const rows = statusline(100);
  assert.equal(rows.length, 2);
  assert.equal(new Set(rows.map(dw)).size, 1, rows.join('\n'));
  // Most urgent first: needs input, then done.
  assert.match(rows[0], /^ T10 b\s+◆ needs input\s+T3\s+c\s+✓ done/);
});

test('statusline: CJK titles keep the grid aligned', () => {
  setSessions([session(1, '修复登录页面的问题'), session(2, 'fix login'), session(3, '写测试', 'done'), session(4, 'ok')]);
  const rows = statusline(70);
  assert.ok(rows.length >= 2);
  assert.equal(new Set(rows.map(dw)).size, 1, rows.join('\n'));
  assert.ok(rows.some((r) => r.includes('修复登录页面的问题')));
});

test('statusline: prints the saved original status line first', () => {
  setSessions([session(1, 'job')]);
  fs.writeFileSync(path.join(dir, 'csquad', 'statusline.json'), JSON.stringify({ present: true, statusLine: { type: 'command', command: 'grep -o transcript_path | tr a-z A-Z' } }));
  const rows = statusline(80, JSON.stringify({ transcript_path: transcript([session(1, 'job')]) }));
  assert.equal(rows[0], 'TRANSCRIPT_PATH');
  assert.match(rows[1], /T1 job/);
  fs.rmSync(path.join(dir, 'csquad', 'statusline.json'));
});

test('launch: concurrent calls get distinct numbers', async () => {
  setSessions([session(7, 'existing')]);
  const run = (title) => new Promise((resolve, reject) => {
    const p = spawn(process.execPath, [tasksJs, 'launch', '--title', title, '--cwd', root], { env });
    let out = '';
    p.stdout.on('data', (d) => (out += d));
    p.stderr.on('data', (d) => (out += d));
    p.on('close', (code) => (code === 0 ? resolve(out) : reject(new Error(out))));
    p.stdin.end('do the thing\n');
  });
  const outs = await Promise.all(['one', 'two', 'three', 'four', 'five'].map(run));
  const nums = outs.map((o) => Number(o.match(/^launched T(\d+) · /)[1])).sort((a, b) => a - b);
  assert.deepEqual(nums, [8, 9, 10, 11, 12]);
  const names = JSON.parse(fs.readFileSync(state, 'utf8')).map((s) => s.name);
  assert.equal(new Set(names).size, names.length);
});

test('statusline: shows only tasks launched by this session', () => {
  setSessions([session(1, 'mine'), session(2, 'theirs'), session(3, 'old output'), session(4, 'peeked')]);
  const t = transcript([session(1, 'mine')], [
    // Old output without the marker, a peek header and prose must not match.
    JSON.stringify({ text: `T3 · old output · session ${session(3).id} · ${root}` }),
    JSON.stringify({ text: `T4 · peeked · working · session ${session(4).id} · ${root}` }),
    JSON.stringify({ text: `we launched T2 later` }),
  ]);
  const rows = statusline(100, JSON.stringify({ transcript_path: t }));
  assert.equal(rows.length, 1);
  assert.match(rows[0], /T1\s+mine/);
  assert.doesNotMatch(rows[0], /theirs|old output|peeked/);
});

test('statusline: no task line without a readable transcript', () => {
  setSessions([session(1, 'job')]);
  assert.deepEqual(statusline(100, '{}'), []);
  assert.deepEqual(statusline(100, JSON.stringify({ transcript_path: path.join(root, 'missing.jsonl') })), []);
  assert.deepEqual(statusline(100, 'not json'), []);
});

test('status: this session by default, everything with --all', () => {
  setSessions([session(1, 'mine'), session(2, 'theirs')]);
  const sid = 'abcd1234-0000-0000-0000-000000000000';
  const proj = path.join(dir, 'projects', 'p');
  fs.mkdirSync(proj, { recursive: true });
  fs.writeFileSync(path.join(proj, sid + '.jsonl'), JSON.stringify({ type: 'user', message: { content: `launched T1 · mine · session ${session(1).id} · /x\n` } }) + '\n');
  const run = (extra, e = {}) => spawnSync(process.execPath, [tasksJs, 'status', ...extra], { env: { ...env, ...e }, encoding: 'utf8' }).stdout;
  const own = run([], { CLAUDE_CODE_SESSION_ID: sid });
  assert.match(own, /## T1 · mine/);
  assert.doesNotMatch(own, /T2/);
  assert.match(run(['--all'], { CLAUDE_CODE_SESSION_ID: sid }), /## T2 · theirs/);
  assert.match(run([]), /## T2 · theirs/); // variable unset: every task
});

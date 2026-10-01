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

// Continued sessions: the new session's transcript does not exist yet.
const cont = (from, to) => JSON.stringify({ type: 'continued-in', sessionId: from, continuedInSessionId: to });
function chainDir(name) {
  const d = path.join(root, name);
  fs.mkdirSync(d, { recursive: true });
  return d;
}
function history(d, id, list, ...tail) {
  const lines = list.map((t) => JSON.stringify({ type: 'user', message: { content: `launched ${t.name} · session ${t.id} · /x\n` } }));
  fs.writeFileSync(path.join(d, id + '.jsonl'), [...lines, ...tail].join('\n') + '\n');
}
const continued = (d, id) => statusline(100, JSON.stringify({ session_id: id, transcript_path: path.join(d, id + '.jsonl') }));

test('statusline: a continued session shows its predecessor\'s tasks', () => {
  setSessions([session(1, 'mine'), session(2, 'theirs')]);
  const d = chainDir('cont1');
  history(d, 'aaa', [session(1, 'mine')], cont('aaa', 'bbb'));
  const rows = continued(d, 'bbb');
  assert.equal(rows.length, 1);
  assert.match(rows[0], /T1\s+mine/);
});

test('statusline: follows a chain of continued sessions', () => {
  setSessions([session(1, 'mine')]);
  const d = chainDir('cont2');
  history(d, 'aaa', [session(1, 'mine')], cont('aaa', 'bbb'));
  history(d, 'bbb', [], cont('bbb', 'ccc')); // stub without history
  const rows = continued(d, 'ccc');
  assert.equal(rows.length, 1);
  assert.match(rows[0], /T1\s+mine/);
});

test('statusline: ignores continued-in records that do not match', () => {
  setSessions([session(1, 'mine')]);
  const d = chainDir('cont3');
  history(d, 'aaa', [session(1, 'mine')], cont('aaa', 'zzz')); // points elsewhere
  history(d, 'bbb', [session(1, 'mine')], cont('bbb', 'ccc'), JSON.stringify({ type: 'user' })); // not the last line
  assert.deepEqual(continued(d, 'ccc'), []);
});

test('statusline: a continued-in cycle terminates', () => {
  setSessions([session(1, 'mine')]);
  const d = chainDir('cont4');
  fs.writeFileSync(path.join(d, 'aaa.jsonl'), cont('aaa', 'bbb') + '\n');
  fs.writeFileSync(path.join(d, 'bbb.jsonl'), cont('bbb', 'aaa') + '\n');
  fs.writeFileSync(path.join(d, 'ccc.jsonl'), cont('ccc', 'ccc') + '\n');
  assert.deepEqual(continued(d, 'aaa'), []);
  assert.deepEqual(continued(d, 'ddd'), []);
});

test('statusline: agent view host without a transcript prints only the own line', () => {
  setSessions([session(1, 'mine')]);
  const saved = path.join(dir, 'csquad', 'statusline.json');
  fs.writeFileSync(saved, JSON.stringify({ present: true, statusLine: { type: 'command', command: 'echo own' } }));
  try {
    assert.deepEqual(continued(chainDir('cont5'), 'host'), ['own']);
  } finally {
    fs.rmSync(saved);
  }
});

test('statusline: an existing transcript is not resolved through predecessors', () => {
  setSessions([session(1, 'mine'), session(2, 'decoy')]);
  const d = chainDir('cont6');
  history(d, 'aaa', [session(1, 'mine')]);
  // A predecessor of aaa; scanning the directory would add its task.
  history(d, 'zzz', [session(2, 'decoy')], cont('zzz', 'aaa'));
  const rows = continued(d, 'aaa');
  assert.equal(rows.length, 1);
  assert.doesNotMatch(rows[0], /decoy/);
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

test('status: finds tasks whose title contains quotes', () => {
  const t = session(1, 'say "hi"');
  setSessions([t, session(2, 'theirs')]);
  const sid = 'beef5678-0000-0000-0000-000000000000';
  const proj = path.join(dir, 'projects', 'p');
  fs.mkdirSync(proj, { recursive: true });
  fs.writeFileSync(path.join(proj, sid + '.jsonl'), JSON.stringify({ type: 'user', message: { content: `launched T1 · say "hi" · session ${t.id} · /x\n` } }) + '\n');
  const out = spawnSync(process.execPath, [tasksJs, 'status'], { env: { ...env, CLAUDE_CODE_SESSION_ID: sid }, encoding: 'utf8' }).stdout;
  assert.match(out, /## T1 · say "hi"/);
  assert.doesNotMatch(out, /T2/);
});

// ---- peek scoping and watch ------------------------------------------------

// ownSession writes a transcript for a fresh session id that launched `list`.
function ownSession(list) {
  const sid = Math.random().toString(16).slice(2, 10) + '-0000-0000-0000-000000000000';
  const proj = path.join(dir, 'projects', 'p');
  fs.mkdirSync(proj, { recursive: true });
  const lines = list.map((t) => JSON.stringify({ type: 'user', message: { content: `launched ${t.name} · session ${t.id} · /x\n` } }));
  fs.writeFileSync(path.join(proj, sid + '.jsonl'), lines.join('\n') + '\n');
  return sid;
}
const addLaunch = (sid, t) =>
  fs.appendFileSync(path.join(dir, 'projects', 'p', sid + '.jsonl'), JSON.stringify({ type: 'user', message: { content: `launched ${t.name} · session ${t.id} · /x\n` } }) + '\n');
const launched = (num, title) => ({ ...session(num), name: `T${num} · ${title}` });

test('peek: refuses tasks another session launched', () => {
  setSessions([session(1, 'mine'), session(2, 'theirs')]);
  const sid = ownSession([launched(1, 'mine')]);
  const peek = (label, extra = [], e = { CLAUDE_CODE_SESSION_ID: sid }) =>
    spawnSync(process.execPath, [tasksJs, 'peek', label, ...extra], { env: { ...env, ...e }, encoding: 'utf8' });
  assert.equal(peek('T1').status, 0);
  const r = peek('T2');
  assert.equal(r.status, 1);
  assert.match(r.stderr, /T2 was launched by another session; pass --all to peek it anyway/);
  assert.equal(peek('T2', ['--all']).status, 0);
  assert.equal(peek('T2', [], {}).status, 0); // variable unset: any task
});

const watchEnv = (sid, extra = {}) => ({ ...env, CLAUDE_CODE_SESSION_ID: sid, CSQUAD_WATCH_INTERVAL_MS: '50', ...extra });

// startWatch runs `tasks.js watch`; done resolves with its exit code and output.
function startWatch(sid, extra) {
  const p = spawn(process.execPath, [tasksJs, 'watch'], { env: watchEnv(sid, extra) });
  let out = '';
  p.stdout.on('data', (d) => (out += d));
  p.stderr.on('data', (d) => (out += d));
  const done = new Promise((resolve) => p.on('close', (code) => resolve({ code, out: out.trim() })));
  return { p, done };
}
const wait = (ms) => new Promise((r) => setTimeout(r, ms));
const lockFile = (sid) => path.join(dir, 'csquad', `watch-${sid}.pid`);

test('watch: reports a task that needs input, finishes or fails', async () => {
  const sid = ownSession([launched(1, 'mine'), launched(2, 'other'), launched(3, 'third')]);
  setSessions([session(1, 'mine'), session(2, 'other'), session(3, 'third')]);
  const w = startWatch(sid);
  await wait(300);
  setSessions([session(1, 'mine', 'blocked'), session(2, 'other', 'done'), session(3, 'third', 'failed')]);
  const r = await w.done;
  assert.equal(r.code, 0);
  assert.deepEqual(r.out.split('\n').sort(), ['T1 · mine · needs input', 'T2 · other · done', 'T3 · third · failed']);
});

test('watch: ignores tasks already finished at start and other sessions', async () => {
  const sid = ownSession([launched(1, 'mine'), launched(2, 'old')]);
  setSessions([session(1, 'mine'), session(2, 'old', 'done'), session(3, 'theirs')]);
  const w = startWatch(sid);
  await wait(300);
  setSessions([session(1, 'mine'), session(2, 'old', 'done'), session(3, 'theirs', 'done')]);
  await wait(300);
  setSessions([session(1, 'mine', 'done'), session(2, 'old', 'done'), session(3, 'theirs', 'done')]);
  const r = await w.done;
  assert.equal(r.out, 'T1 · mine · done');
});

test('watch: includes a task launched after it started; a removed task is no event', async () => {
  const sid = ownSession([launched(1, 'mine')]);
  setSessions([session(1, 'mine')]);
  const w = startWatch(sid);
  await wait(300);
  addLaunch(sid, launched(2, 'later'));
  setSessions([session(2, 'later')]); // T1 was removed
  await wait(300);
  setSessions([session(2, 'later', 'done')]);
  const r = await w.done;
  assert.equal(r.out, 'T2 · later · done');
});

test('watch: exits when nothing is working', async () => {
  const sid = ownSession([launched(1, 'mine')]);
  setSessions([session(1, 'mine', 'done')]);
  const r = await startWatch(sid).done;
  assert.deepEqual([r.code, r.out], [0, 'no open tasks']);
  assert.ok(!fs.existsSync(lockFile(sid)));
});

test('watch: without a session id it exits quietly', () => {
  const r = spawnSync(process.execPath, [tasksJs, 'watch'], { env, encoding: 'utf8' });
  assert.equal(r.status, 0);
  assert.match(r.stdout, /CLAUDE_CODE_SESSION_ID/);
});

test('watch: one watcher per session, stale locks are taken over, lock is removed', async () => {
  const sid = ownSession([launched(1, 'mine')]);
  setSessions([session(1, 'mine')]);
  const first = startWatch(sid);
  await wait(300);
  assert.equal(fs.readFileSync(lockFile(sid), 'utf8'), String(first.p.pid));
  const second = await startWatch(sid).done;
  assert.deepEqual([second.code, second.out], [0, 'already watching']);
  assert.ok(fs.existsSync(lockFile(sid)), 'the second watcher must not remove the first one\'s lock');
  first.p.kill('SIGTERM');
  await first.done;
  assert.ok(!fs.existsSync(lockFile(sid)));

  fs.writeFileSync(lockFile(sid), '2147483646'); // no such process
  setSessions([session(1, 'mine', 'done')]);
  const r = await startWatch(sid).done;
  assert.equal(r.out, 'no open tasks');
  assert.ok(!fs.existsSync(lockFile(sid)));
});

test('watch: retries a failing claude and gives up after a while', async () => {
  const sid = ownSession([launched(1, 'mine')]);
  fs.writeFileSync(state, 'not json');
  const w = startWatch(sid, { CSQUAD_WATCH_GIVEUP_MS: '600' });
  await wait(300);
  setSessions([session(1, 'mine')]); // recovers: still watching
  await wait(300);
  setSessions([session(1, 'mine', 'done')]);
  assert.equal((await w.done).out, 'T1 · mine · done');

  fs.writeFileSync(state, 'not json');
  const r = await startWatch(sid, { CSQUAD_WATCH_GIVEUP_MS: '300' }).done;
  assert.equal(r.code, 1);
  assert.match(r.out, /giving up/);
});

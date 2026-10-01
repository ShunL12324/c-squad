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
  const out = JSON.stringify(read());
  fs.appendFileSync(state + '.calls', 'x');
  console.log(out);
} else if (args.includes('--bg')) {
  const name = args[args.indexOf('--name') + 1];
  // Like MAX_ARG_STRLEN / ARG_MAX: refuse oversized arguments.
  const max = Number(process.env.FAKE_MAX_ARG || 0);
  if (max && args.some((a) => Buffer.byteLength(a) > max)) {
    console.error('Argument list too long');
    process.exit(1);
  }
  fs.appendFileSync(state + '.argv', JSON.stringify(args) + '\\n');
  if (name.includes('FAILME')) {
    console.error('workspace not trusted');
    process.exit(1);
  }
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

// installFake puts a fake `claude` into `bin`: a node script with a shebang on
// Unix; on Windows a node script behind an npm-style claude.cmd shim, the way
// `npm i -g @anthropic-ai/claude-code` installs it.
function installFake(bin, source) {
  if (process.platform !== 'win32') {
    fs.writeFileSync(path.join(bin, 'claude'), source, { mode: 0o755 });
    return;
  }
  fs.writeFileSync(path.join(bin, 'claude-fake.js'), source.replace(/^#!.*\n/, ''));
  fs.writeFileSync(path.join(bin, 'claude.cmd'), [
    '@ECHO off', 'GOTO start', ':find_dp0', 'SET dp0=%~dp0', 'EXIT /b', ':start', 'SETLOCAL', 'CALL :find_dp0', '',
    'IF EXIST "%dp0%\\node.exe" (', '  SET "_prog=%dp0%\\node.exe"', ') ELSE (', '  SET "_prog=node"', ')', '',
    'endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\\claude-fake.js" %*', '',
  ].join('\r\n'));
}

// withPath returns env with `bin` first on PATH, whatever the variable's case.
function withPath(base, bin) {
  const key = Object.keys(base).find((k) => k.toLowerCase() === 'path') || 'PATH';
  return { ...base, [key]: bin + path.delimiter + base[key] };
}

before(() => {
  root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'csquad-tasks-')));
  dir = path.join(root, 'claude');
  tasksJs = path.join(dir, 'csquad', 'tasks.js');
  state = path.join(root, 'sessions.json');
  const bin = path.join(root, 'bin');
  fs.mkdirSync(bin);
  installFake(bin, FAKE);
  env = withPath({ ...process.env, CLAUDE_CONFIG_DIR: dir, HOME: root, USERPROFILE: root, FAKE_STATE: state }, bin);
  delete env.CLAUDE_CODE_SESSION_ID;
  fs.mkdirSync(path.join(root, 'tmp'));
  env.TMPDIR = env.TEMP = env.TMP = path.join(root, 'tmp'); // the transcript scan cache lives here
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

// launchLines returns the JSONL lines of a Bash call running `command` and its
// tool_result holding `text`.
let toolSeq = 0;
const LAUNCH = "node ~/.claude/csquad/tasks.js launch --title x <<'EOF'\nprompt\nEOF";
function launchLines(text, command = LAUNCH, id = 'toolu_' + ++toolSeq) {
  return [
    JSON.stringify({ type: 'assistant', message: { content: [{ type: 'tool_use', id, name: 'Bash', input: { command } }] } }),
    JSON.stringify({ type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: id, content: text }] } }),
  ];
}
const launchOf = (t, where = root, id) => launchLines(`launched ${t.name} · session ${t.id} · ${where}\n`, LAUNCH, id);

// transcript writes a JSONL transcript in which the session launched `list`.
function transcript(list, extra = []) {
  const file = path.join(root, 'transcript.jsonl');
  const lines = list.flatMap((t) => launchOf(t));
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
  const lines = list.flatMap((t) => launchOf(t, '/x'));
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
  fs.writeFileSync(path.join(proj, sid + '.jsonl'), launchOf(session(1, 'mine'), '/x').join('\n') + '\n');
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
  fs.writeFileSync(path.join(proj, sid + '.jsonl'), launchOf(t, '/x').join('\n') + '\n');
  const out = spawnSync(process.execPath, [tasksJs, 'status'], { env: { ...env, CLAUDE_CODE_SESSION_ID: sid }, encoding: 'utf8' }).stdout;
  assert.match(out, /## T1 · say "hi"/);
  assert.doesNotMatch(out, /T2/);
});

// ---- peek scoping ------------------------------------------------

// ownSession writes a transcript for a fresh session id that launched `list`.
function ownSession(list) {
  const sid = Math.random().toString(16).slice(2, 10) + '-0000-0000-0000-000000000000';
  const proj = path.join(dir, 'projects', 'p');
  fs.mkdirSync(proj, { recursive: true });
  const lines = list.flatMap((t) => launchOf(t, '/x'));
  fs.writeFileSync(path.join(proj, sid + '.jsonl'), lines.join('\n') + '\n');
  return sid;
}
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

// ---- launch marker attribution and the incremental scan ---------------------

const shown = (file) => statusline(200, JSON.stringify({ transcript_path: file })).join('\n').match(/T\d+(?= )/g) || [];
const marker = (t) => `launched ${t.name} · session ${t.id} · /x\n`;
const writeLines = (file, lines) => fs.writeFileSync(file, lines.join('\n') + '\n');

test('statusline: only markers from this session\'s own launches count', () => {
  setSessions([1, 2, 3, 4, 5, 6, 7, 8, 9].map((n) => session(n, n === 9 ? 'say "hi"' : 'job')));
  const s = (n) => session(n, n === 9 ? 'say "hi"' : 'job');
  const [t1, t8, t9] = [s(1), s(8), s(9)];
  const file = path.join(root, 'attribution.jsonl');
  writeLines(file, [
    ...launchOf(t1),
    JSON.stringify({ type: 'user', message: { content: marker(s(2)) } }), // pasted
    JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: marker(s(3)) }] } }), // quoted
    ...launchLines(marker(s(4)), 'node ~/.claude/csquad/tasks.js peek T12'), // a worker's own launch, peeked
    ...launchLines(marker(s(5)), 'node ~/.claude/csquad/tasks.js status'),
    JSON.stringify({ type: 'user', message: { content: [{ type: 'tool_result', tool_use_id: 'toolu_none', content: marker(s(6)) }] } }), // no tool_use
    ...launchLines(`worker said: ${marker(s(7))}`), // not at the start of a line
    ...launchLines([{ type: 'text', text: marker(t8) }]), // content as blocks
    ...launchOf(t9),
  ]);
  assert.deepEqual(shown(file).sort(), ['T1', 'T8', 'T9']);
});

test('statusline: every task launched by one Bash call counts', () => {
  const [a, b, c] = [session(1, 'one'), session(2, 'two'), session(3, 'three')];
  setSessions([a, b, c]);
  const file = path.join(root, 'batch.jsonl');
  writeLines(file, launchLines(marker(a) + marker(b) + 'some other output\n' + marker(c)));
  assert.deepEqual(shown(file).sort(), ['T1', 'T2', 'T3']);
});

test('statusline: a launch whose tool_use is far before its result still counts', () => {
  setSessions([session(1, 'job')]);
  const file = path.join(root, 'far.jsonl');
  const [use, result] = launchOf(session(1, 'job'));
  const filler = JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'x'.repeat(1 << 20) }] } });
  writeLines(file, [use, filler, filler, result]);
  assert.deepEqual(shown(file), ['T1']);
});

test('statusline: appended launches are found and only new bytes are scanned', () => {
  setSessions([session(1, 'one'), session(2, 'two')]);
  const file = path.join(root, 'inc.jsonl');
  const pad = JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'p'.repeat(600) }] } });
  writeLines(file, [...launchOf(session(1, 'one')), pad]);
  assert.deepEqual(shown(file), ['T1']);
  // Damage the already scanned marker in place (same size): a rescan would lose T1.
  const buf = fs.readFileSync(file);
  buf.write('xaunched', buf.indexOf('launched'));
  fs.writeFileSync(file, buf);
  assert.deepEqual(shown(file), ['T1']);
  fs.appendFileSync(file, launchOf(session(2, 'two')).join('\n') + '\n');
  assert.deepEqual(shown(file).sort(), ['T1', 'T2']);
});

test('statusline: a marker line split across scans is found once complete', () => {
  setSessions([session(1, 'one')]);
  const file = path.join(root, 'split.jsonl');
  const [use, result] = launchOf(session(1, 'one'));
  const cut = result.indexOf('launched T1') + 6; // in the middle of the marker
  fs.writeFileSync(file, use + '\n' + result.slice(0, cut));
  assert.deepEqual(shown(file), []);
  fs.appendFileSync(file, result.slice(cut) + '\n');
  assert.deepEqual(shown(file), ['T1']);
});

test('statusline: the cache is dropped when the file shrinks, is replaced or is corrupt', () => {
  const one = session(1, 'one');
  const two = session(2, 'two');
  setSessions([one, two]);
  const file = path.join(root, 'reset.jsonl');
  const cacheDir = () => {
    const d = fs.readdirSync(env.TMPDIR).find((n) => n.startsWith('csquad-scan-'));
    return path.join(env.TMPDIR, d);
  };
  writeLines(file, [...launchOf(one), ...launchOf(two)]);
  assert.deepEqual(shown(file).sort(), ['T1', 'T2']);
  // Shrunk (same inode).
  writeLines(file, launchOf(two));
  assert.deepEqual(shown(file), ['T2']);
  // Replaced by a different file of the same size.
  writeLines(file + '.new', launchOf(one));
  fs.renameSync(file + '.new', file);
  assert.deepEqual(shown(file), ['T1']);
  // Corrupt and unwritable caches.
  for (const f of fs.readdirSync(cacheDir())) fs.writeFileSync(path.join(cacheDir(), f), '{not json');
  assert.deepEqual(shown(file), ['T1']);
  const d = cacheDir();
  fs.rmSync(d, { recursive: true });
  fs.writeFileSync(d, 'a file where the cache directory should be');
  try {
    assert.deepEqual(shown(file), ['T1']);
  } finally {
    fs.rmSync(d);
  }
});

test('statusline: a rewritten transcript is not mistaken for an appended one', () => {
  const [one, two, three] = [1, 2, 3].map((n) => session(n, 'job'));
  setSessions([one, two, three]);
  const file = path.join(root, 'rewrite.jsonl');
  const pad = JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'p'.repeat(600) }] } });
  writeLines(file, [...launchOf(one, root, 'toolu_fix'), pad]);
  assert.deepEqual(shown(file), ['T1']);
  // Same inode, larger, different content.
  writeLines(file, [...launchOf(two, root, 'toolu_fix'), pad.replace(/p+/, 'q'.repeat(600)), ...launchOf(three, root, 'toolu_fix')]);
  assert.deepEqual(shown(file).sort(), ['T2', 'T3']);
  // A new file (new inode) with the old tail at the old offset.
  writeLines(file + '.new', [...launchOf(one, root, 'toolu_fix'), pad, ...launchOf(three, root, 'toolu_fix')]);
  fs.renameSync(file + '.new', file);
  assert.deepEqual(shown(file).sort(), ['T1', 'T3']);
  writeLines(file + '.new', [...launchOf(two, root, 'toolu_fix'), pad, ...launchOf(three, root, 'toolu_fix'), pad]);
  fs.renameSync(file + '.new', file);
  assert.deepEqual(shown(file).sort(), ['T2', 'T3']);
});

// ---- hot path, robustness and locks ----------------------------------------

const calls = () => (fs.existsSync(state + '.calls') ? fs.statSync(state + '.calls').size : 0);

test('statusline: does not run claude when the session launched nothing', () => {
  setSessions([session(1, 'job')]);
  const before = calls();
  assert.deepEqual(statusline(100, JSON.stringify({ transcript_path: transcript([]) })), []);
  assert.equal(calls(), before);
  statusline(100, JSON.stringify({ transcript_path: transcript([session(1, 'job')]) }));
  assert.equal(calls(), before + 1);
});

test('statusline: copes with an agents list larger than 1 MB', () => {
  const big = [session(1, 'job'), ...Array.from({ length: 3000 }, (_, i) => ({ ...session(i + 100, 'x'), name: 'other', pad: 'p'.repeat(500) }))];
  setSessions(big);
  assert.ok(fs.statSync(state).size > 1 << 20);
  const rows = statusline(100, JSON.stringify({ transcript_path: transcript([session(1, 'job')]) }));
  assert.equal(rows.length, 1);
  assert.match(rows[0], /T1\s+job/);
});

test('statusline: control characters in a title are not passed on', () => {
  setSessions([session(1, 'a\x1b[31mred\x07b')]);
  const r = spawnSync(process.execPath, [tasksJs, 'statusline'], {
    env: { ...env, COLUMNS: '100' }, input: JSON.stringify({ transcript_path: transcript([session(1, 'x')]) }), encoding: 'utf8',
  });
  const plain = r.stdout.replace(/\x1b\[[0-9;]*m/g, '');
  assert.doesNotMatch(plain, /[\x00-\x08\x0b-\x1f]/);
  assert.match(plain, /a \[31mred b/);
});

test('status and peek work before Claude has created projects/', () => {
  const bare = path.join(root, 'bare');
  fs.mkdirSync(path.join(bare, 'csquad'), { recursive: true });
  fs.copyFileSync(tasksJs, path.join(bare, 'csquad', 'tasks.js'));
  setSessions([session(1, 'job')]);
  for (const args of [['status'], ['status', '--all'], ['peek', 'T1']]) {
    const r = spawnSync(process.execPath, [path.join(bare, 'csquad', 'tasks.js'), ...args], { env: { ...env, ...(args[0] === 'status' ? { CLAUDE_CODE_SESSION_ID: 'abc' } : {}) }, encoding: 'utf8' });
    assert.equal(r.status, 0, args + ' ' + r.stderr);
  }
});

const launchProc = (title, e = env) => spawnSync(process.execPath, [tasksJs, 'launch', '--title', title, '--cwd', root], { env: e, input: 'do it\n', encoding: 'utf8', timeout: 20000 });
const launchLock = () => path.join(dir, 'csquad', 'launch.lock');

test('launch: takes over a lock whose process is gone', () => {
  setSessions([]);
  fs.writeFileSync(launchLock(), '2147483646'); // no such process
  const r = launchProc('after crash');
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /^launched T1 · after crash · session [0-9a-f]+ · /);
  assert.ok(!fs.existsSync(launchLock()));
});

test('launch: a failing claude is reported and releases the lock', () => {
  setSessions([]);
  const r = launchProc('FAILME');
  assert.equal(r.status, 1);
  assert.match(r.stderr, /launching T1 · FAILME failed:\nworkspace not trusted/);
  assert.ok(!fs.existsSync(launchLock()));
});

test('launch: a prompt too large for argv goes through a file', () => {
  setSessions([]);
  fs.rmSync(state + '.argv', { force: true });
  const big = 'x'.repeat(300000) + '\nEND-MARKER';
  const r = spawnSync(process.execPath, [tasksJs, 'launch', '--title', 'big', '--cwd', root], { env: { ...env, FAKE_MAX_ARG: '100000' }, input: big, encoding: 'utf8', timeout: 20000 });
  assert.equal(r.status, 0, r.stderr);
  assert.match(r.stdout, /^launched T1 · big · /);
  const argv = JSON.parse(fs.readFileSync(state + '.argv', 'utf8').trim().split('\n').pop());
  const prompt = argv[argv.length - 1];
  const file = path.join(dir, 'csquad', 'prompts', 'T1.md');
  assert.ok(prompt.length < 1000 && prompt.includes(file));
  assert.equal(fs.readFileSync(file, 'utf8'), big + '\n');
});

const lastArgv = () => JSON.parse(fs.readFileSync(state + '.argv', 'utf8').trim().split('\n').pop());

test('launch: awkward characters in the title and prompt reach claude intact', () => {
  setSessions([]);
  fs.rmSync(state + '.argv', { force: true });
  const title = 'a b "q" \'s\' 100% ^c & (d) | <e> !f! $g `h` \\';
  const prompt = 'line one\nsays "hi" and \'bye\'\r\n100% ^caret & amp | pipe <in> out\n\nC:\\dir\\ \\\\ trailing\\';
  const r = spawnSync(process.execPath, [tasksJs, 'launch', '--title', title, '--cwd', root], { env, input: prompt, encoding: 'utf8', timeout: 20000 });
  assert.equal(r.status, 0, r.stderr);
  const argv = lastArgv();
  assert.equal(argv[argv.indexOf('--name') + 1], `T1 · ${title}`);
  assert.equal(argv[argv.length - 1], prompt);
  const system = argv[argv.indexOf('--append-system-prompt') + 1];
  assert.ok(system.includes('\n') && system.endsWith('Your task ID is T1.\n'));
});

test('launch: --prompt-file reads the prompt from a file and --rm-prompt deletes it afterwards', () => {
  setSessions([]);
  const file = path.join(root, 'draft-x.md');
  fs.writeFileSync(file, 'from a file\nsecond line\n');
  let r = spawnSync(process.execPath, [tasksJs, 'launch', '--title', 'f', '--prompt-file', file, '--cwd', root], { env, encoding: 'utf8', timeout: 20000 });
  assert.equal(r.status, 0, r.stderr);
  assert.equal(lastArgv().pop(), 'from a file\nsecond line');
  assert.ok(fs.existsSync(file), 'kept without --rm-prompt');
  setSessions([]);
  r = spawnSync(process.execPath, [tasksJs, 'launch', '--title', 'g', '--prompt-file', file, '--rm-prompt', '--cwd', root], { env, encoding: 'utf8', timeout: 20000 });
  assert.equal(r.status, 0, r.stderr);
  assert.ok(!fs.existsSync(file));
  r = spawnSync(process.execPath, [tasksJs, 'launch', '--title', 'h', '--prompt-file', file, '--cwd', root], { env, encoding: 'utf8', timeout: 20000 });
  assert.equal(r.status, 1);
  assert.match(r.stderr, /cannot read the prompt file/);
  fs.writeFileSync(file, 'x');
  setSessions([]);
  r = spawnSync(process.execPath, [tasksJs, 'launch', '--title', 'FAILME', '--prompt-file', file, '--rm-prompt', '--cwd', root], { env, encoding: 'utf8', timeout: 20000 });
  assert.equal(r.status, 1);
  assert.ok(fs.existsSync(file), 'kept when the launch fails');
});

test('launch: small prompts stay on the command line; prompt files are pruned and removed on failure', () => {
  fs.rmSync(state + '.argv', { force: true });
  const prompts = path.join(dir, 'csquad', 'prompts');
  fs.mkdirSync(prompts, { recursive: true });
  fs.writeFileSync(path.join(prompts, 'T7.md'), 'old'); // its task is gone
  setSessions([]);
  assert.equal(launchProc('small').status, 0);
  const argv = JSON.parse(fs.readFileSync(state + '.argv', 'utf8').trim().split('\n').pop());
  assert.equal(argv[argv.length - 1], 'do it');
  assert.ok(!fs.existsSync(path.join(prompts, 'T7.md')));
  setSessions([]);
  const r = spawnSync(process.execPath, [tasksJs, 'launch', '--title', 'FAILME', '--cwd', root], { env, input: 'y'.repeat(100000), encoding: 'utf8', timeout: 20000 });
  assert.equal(r.status, 1);
  assert.deepEqual(fs.readdirSync(prompts), []);
});

const runLaunch = (title) => new Promise((resolve) => {
  const p = spawn(process.execPath, [tasksJs, 'launch', '--title', title, '--cwd', root], { env });
  let out = '';
  p.stdout.on('data', (d) => (out += d));
  p.stderr.on('data', (d) => (out += d));
  p.on('close', (code) => resolve({ code, out }));
  p.stdin.end('go\n');
});

test('launch: many launchers racing over a stale lock get distinct numbers', async () => {
  for (let round = 0; round < 3; round++) {
    setSessions([]);
    fs.writeFileSync(launchLock(), '2147483646');
    const results = await Promise.all(Array.from({ length: 10 }, (_, i) => runLaunch('r' + i)));
    for (const r of results) assert.equal(r.code, 0, r.out);
    const nums = results.map((r) => Number(r.out.match(/^launched T(\d+) · /)[1])).sort((a, b) => a - b);
    assert.deepEqual(nums, [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]);
    assert.ok(!fs.existsSync(launchLock()));
    assert.ok(!fs.existsSync(launchLock() + '.takeover'));
    assert.deepEqual(fs.readdirSync(path.join(dir, 'csquad')).filter((f) => f.startsWith('launch.lock')), []);
  }
});

test('launch: a lock held by a live process is never taken over', async () => {
  setSessions([]);
  const holder = spawn(process.execPath, ['-e', 'setTimeout(() => {}, 30000)']);
  try {
    fs.writeFileSync(launchLock(), String(holder.pid));
    const p = runLaunch('waits');
    await new Promise((r) => setTimeout(r, 1500));
    assert.equal(fs.readFileSync(launchLock(), 'utf8'), String(holder.pid));
    assert.deepEqual(JSON.parse(fs.readFileSync(state, 'utf8')), []); // still waiting
    holder.kill();
    await new Promise((r) => holder.on('close', r));
    const r = await p;
    assert.equal(r.code, 0, r.out);
    assert.ok(!fs.existsSync(launchLock()));
  } finally {
    holder.kill();
  }
});

// ---- wait ----------------------------------------------------------

const FAST = { CSQUAD_WAIT_INTERVAL_MS: '30', CSQUAD_WAIT_GRACE_MS: '200' };

// runWait runs `tasks.js wait`; `script` is a list of [ms, sessions] steps that
// rewrite the fake claude's sessions while it runs.
function runWait(args, { script = [], e = {} } = {}) {
  return new Promise((resolve) => {
    const child = spawn(process.execPath, [tasksJs, 'wait', ...args], { env: { ...env, ...FAST, ...e } });
    let stdout = '';
    let stderr = '';
    child.stdout.on('data', (d) => (stdout += d));
    child.stderr.on('data', (d) => (stderr += d));
    for (const [ms, list] of script) setTimeout(() => setSessions(list), ms);
    // A broken wait would otherwise run for its default four hours.
    const guard = setTimeout(() => child.kill('SIGKILL'), 8000);
    child.on('close', (status) => {
      clearTimeout(guard);
      resolve({ status, stdout, stderr });
    });
  });
}
const T = (n, title, st) => session(n, title, st);

test('wait: reports a task that finishes, needs input, fails, stops or vanishes', async () => {
  for (const [st, text] of [['done', 'done'], ['blocked', 'needs input'], ['failed', 'failed'], ['stopped', 'stopped']]) {
    setSessions([T(12, 'fix login')]);
    const r = await runWait(['T12'], { script: [[150, [T(12, 'fix login', st)]]] });
    assert.equal(r.status, 0, r.stderr);
    assert.equal(r.stdout, `T12 · fix login · ${text}\n`);
  }
  setSessions([T(12, 'fix login'), T(13, 'other')]);
  const r = await runWait(['12'], { script: [[150, [T(13, 'other')]]] });
  assert.equal(r.stdout, 'T12 · fix login · removed\n');
});

test('wait: a state seen on one poll only is not reported', async () => {
  // A claude whose n-th `agents` call returns the n-th snapshot (the last one repeats).
  const bin = path.join(root, 'seq-bin');
  fs.mkdirSync(bin, { recursive: true });
  installFake(bin, `#!/usr/bin/env node
const fs = require('fs');
const f = process.env.FAKE_STATE + '.seq';
const n = fs.existsSync(f) ? Number(fs.readFileSync(f, 'utf8')) : 0;
fs.writeFileSync(f, String(n + 1));
const seq = JSON.parse(process.env.FAKE_SEQ);
console.log(JSON.stringify([{ id: 'aaaaaaaa', sessionId: 's12', kind: 'background', name: 'T12 · fix login', state: seq[Math.min(n, seq.length - 1)], cwd: '/', startedAt: 0 }]));
`);
  const run = (seq) => {
    fs.rmSync(state + '.seq', { force: true });
    return runWait(['T12'], { e: { ...withPath(env, bin), FAKE_SEQ: JSON.stringify(seq) } });
  };
  // Poll 0 is the start state; "done" on poll 2 only, then back to working.
  let r = await run(['working', 'working', 'done', 'working', 'working', 'failed', 'failed']);
  assert.equal(r.stdout, 'T12 · fix login · failed\n');
  r = await run(['working', 'done', 'done']);
  assert.equal(r.stdout, 'T12 · fix login · done\n');
});

test('wait: events within the grace period share one exit', async () => {
  setSessions([T(1, 'a'), T(2, 'b'), T(3, 'c')]);
  const r = await runWait(['T2', 'T1', 'T3'], {
    e: { CSQUAD_WAIT_GRACE_MS: '800' },
    script: [[150, [T(1, 'a', 'done'), T(2, 'b'), T(3, 'c')]], [350, [T(1, 'a', 'done'), T(2, 'b', 'blocked'), T(3, 'c')]]],
  });
  assert.equal(r.status, 0);
  assert.equal(r.stdout, 'T1 · a · done\nT2 · b · needs input\n');
});

test('wait: a task already finished is reported at once', async () => {
  setSessions([T(5, 'old job', 'done'), T(6, 'busy')]);
  const t0 = Date.now();
  const r = await runWait(['T5', 'T6'], { e: { CSQUAD_WAIT_INTERVAL_MS: '60000', CSQUAD_WAIT_GRACE_MS: '50' } });
  assert.equal(r.stdout, 'T5 · old job · already done\n');
  assert.ok(Date.now() - t0 < 5000);
});

test("wait: unknown labels and other sessions' tasks", async () => {
  setSessions([T(1, 'mine'), T(2, 'theirs')]);
  let r = await runWait(['T1', 'T9']);
  assert.equal(r.status, 1);
  assert.match(r.stderr, /T9 is not listed by claude agents/);
  assert.equal(r.stdout, '');
  r = await runWait(['banana']);
  assert.equal(r.status, 1);
  assert.match(r.stderr, /expected a task like T12/);
  const sid = ownSession([launched(1, 'mine')]);
  r = await runWait(['T2'], { e: { CLAUDE_CODE_SESSION_ID: sid } });
  assert.equal(r.status, 1);
  assert.match(r.stderr, /T2 was launched by another session; pass --all/);
  setSessions([T(1, 'mine'), T(2, 'theirs', 'done')]);
  r = await runWait(['T2', '--all'], { e: { CLAUDE_CODE_SESSION_ID: sid } });
  assert.equal(r.stdout, 'T2 · theirs · already done\n');
});

test('wait: timeout reports the tasks still working', async () => {
  setSessions([T(12, 'fix login'), T(13, 'tests')]);
  const r = await runWait(['T12', 'T13', '--timeout', '0.01']);
  assert.equal(r.status, 0);
  assert.equal(r.stdout, 'T12 · fix login · still working after 0m\nT13 · tests · still working after 0m\n');
});

test('wait: retries brief claude failures, gives up with exit 1 after continuous ones', async () => {
  setSessions([T(12, 'fix login')]);
  const r = await runWait(['T12'], { e: { CSQUAD_WAIT_FAIL_MS: '300', FAKE_STATE: state + '.missing' } });
  assert.equal(r.status, 1);
  assert.match(r.stderr, /claude agents keeps failing/);
  assert.equal(r.stdout, '');
  // Unreadable at first, valid 150 ms later: retried, not fatal.
  fs.writeFileSync(state, 'not json');
  const ok = await runWait(['T12'], { e: { CSQUAD_WAIT_FAIL_MS: '5000' }, script: [[150, [T(12, 'fix login', 'done')]]] });
  assert.equal(ok.status, 0);
  assert.equal(ok.stdout, 'T12 · fix login · already done\n');
});

test('wait: SIGTERM exits quietly', { skip: process.platform === 'win32' && 'Windows has no SIGTERM: kill() terminates the process abruptly' }, async () => {
  setSessions([T(12, 'fix login')]);
  const child = spawn(process.execPath, [tasksJs, 'wait', 'T12'], { env: { ...env, ...FAST } });
  let out = '';
  child.stdout.on('data', (d) => (out += d));
  child.stderr.on('data', (d) => (out += d));
  await new Promise((r) => setTimeout(r, 300));
  child.kill('SIGTERM');
  const code = await new Promise((r) => child.on('close', r));
  assert.equal(code, 0);
  assert.equal(out, '');
});

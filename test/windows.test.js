'use strict';
// Windows behaviour: the pure parts run on every OS (with the platform passed
// in), the parts that need a real cmd.exe only on Windows.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync, execFileSync } = require('node:child_process');
const { test, beforeEach, afterEach } = require('node:test');

const installer = require('../lib/installer');
const tasks = require('../payload/csquad/tasks.js');

const CLI = path.join(__dirname, '..', 'bin', 'csquad.js');
const WIN = process.platform === 'win32';
const onlyWindows = !WIN && 'needs a real cmd.exe';

let root;
beforeEach(() => {
  root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'csquad-win-')));
});
afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

// ---- status line command and skills --------------------------------------

const SPACED = 'C:\\Users\\First Last\\.claude';

test('statusline command: forward slashes, quoted only when needed', () => {
  assert.equal(installer.statuslineCommand(SPACED, true), 'node "C:/Users/First Last/.claude/csquad/tasks.js" statusline');
  assert.equal(installer.statuslineCommand('C:\\Users\\shun\\.claude', true), 'node C:/Users/shun/.claude/csquad/tasks.js statusline');
  if (!WIN) assert.equal(installer.statuslineCommand('/home/a b/.claude', false), "node '/home/a b/.claude/csquad/tasks.js' statusline");
});

test('skills: the path of a file is quoted as a whole', () => {
  const text = 'node {{CSQUAD_DIR}}/tasks.js status; write {{CSQUAD_DIR}}/prompts/draft-x.md';
  assert.equal(
    installer.render(text, SPACED, true),
    'node "C:/Users/First Last/.claude/csquad/tasks.js" status; write "C:/Users/First Last/.claude/csquad/prompts/draft-x.md"',
  );
  assert.equal(installer.render(text, 'C:\\Users\\shun\\.claude', true), 'node C:/Users/shun/.claude/csquad/tasks.js status; write C:/Users/shun/.claude/csquad/prompts/draft-x.md');
  if (!WIN) assert.equal(installer.render('{{CSQUAD_DIR}}/tasks.js', '/a b', false), "'/a b/csquad'/tasks.js");
});

test('install recognizes a Windows-style wrapper as its own and never wraps it twice', () => {
  const dir = path.join(root, 'claude');
  fs.mkdirSync(dir);
  const old = 'node "C:/Users/First Last/.claude/csquad/tasks.js" statusline';
  fs.writeFileSync(path.join(dir, 'settings.json'), JSON.stringify({ statusLine: { type: 'command', command: old, refreshInterval: 5 } }));
  const r = spawnSync(process.execPath, [CLI, 'install'], { cwd: root, encoding: 'utf8', env: { ...process.env, CLAUDE_CONFIG_DIR: dir, HOME: root, USERPROFILE: root } });
  assert.equal(r.status, 0, r.stdout + r.stderr);
  const saved = JSON.parse(fs.readFileSync(path.join(dir, 'csquad', 'statusline.json'), 'utf8'));
  assert.deepEqual(saved, { present: false });
  const command = JSON.parse(fs.readFileSync(path.join(dir, 'settings.json'), 'utf8')).statusLine.command;
  assert.equal(command, installer.statuslineCommand(dir));
});

// ---- renames ---------------------------------------------------------------

function failRename(codes, fn) {
  const real = fs.renameSync;
  const calls = [];
  fs.renameSync = (from, to) => {
    calls.push([from, to]);
    if (calls.length <= codes.length) throw Object.assign(new Error(`${codes[calls.length - 1]}: simulated`), { code: codes[calls.length - 1] });
    return real(from, to);
  };
  try {
    return fn(calls);
  } finally {
    fs.renameSync = real;
  }
}

test('renameRetry: retries EPERM, EBUSY and EACCES, then succeeds', () => {
  const from = path.join(root, 'a');
  const to = path.join(root, 'b');
  fs.writeFileSync(from, 'new');
  fs.writeFileSync(to, 'old');
  failRename(['EPERM', 'EBUSY', 'EACCES'], (calls) => {
    installer.renameRetry(from, to, { retry: true });
    assert.equal(calls.length, 4);
  });
  assert.equal(fs.readFileSync(to, 'utf8'), 'new');
});

test('renameRetry: gives up after the last try and does not retry other errors', () => {
  const from = path.join(root, 'a');
  fs.writeFileSync(from, 'x');
  failRename(['EPERM', 'EPERM', 'EPERM', 'EPERM'], (calls) => {
    assert.throws(() => installer.renameRetry(from, path.join(root, 'b'), { retry: true, tries: 3 }), { code: 'EPERM' });
    assert.equal(calls.length, 3);
  });
  failRename(['ENOENT'], (calls) => {
    assert.throws(() => installer.renameRetry(from, path.join(root, 'b'), { retry: true }), { code: 'ENOENT' });
    assert.equal(calls.length, 1);
  });
  failRename(['EPERM'], (calls) => {
    assert.throws(() => installer.renameRetry(from, path.join(root, 'b'), { retry: false }), { code: 'EPERM' });
    assert.equal(calls.length, 1);
  });
});

test('writeAtomic survives a settings.json that is briefly locked', { skip: !WIN && 'retries only on Windows' }, () => {
  const file = path.join(root, 'settings.json');
  fs.writeFileSync(file, 'old');
  failRename(['EPERM', 'EPERM'], () => installer.writeAtomic(file, 'new'));
  assert.equal(fs.readFileSync(file, 'utf8'), 'new');
  assert.deepEqual(fs.readdirSync(root), ['settings.json']);
});

// ---- finding and running claude -------------------------------------------

// Argument strings that are hard to pass through cmd.exe, without newlines.
const AWKWARD = ['plain', 'two words', 'say "hi"', "it's", '100%', '%PATH%', '^caret', 'a&b', 'a|b', '<in>', '(x)', '!y!', 'back\\slash\\', 'trail\\\\', 'C:\\Program Files\\x y\\', '', 'a;b,c=d'];
const ECHO = 'process.stdout.write(JSON.stringify(process.argv.slice(2)));\n';

// npmShim writes an npm-style claude.cmd that runs target.js with node.
function npmShim(dir, lines) {
  fs.writeFileSync(path.join(dir, 'claude.cmd'), lines.join('\r\n') + '\r\n');
}
const NPM_SHIM = [
  '@ECHO off', 'GOTO start', ':find_dp0', 'SET dp0=%~dp0', 'EXIT /b', ':start', 'SETLOCAL', 'CALL :find_dp0', '',
  'IF EXIST "%dp0%\\node.exe" (', '  SET "_prog=%dp0%\\node.exe"', ') ELSE (', '  SET "_prog=node"', ')', '',
  'endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\\node_modules\\claude\\cli.js" %*',
];

function runResolved(c) {
  return JSON.parse(execFileSync(c.file, c.args, { encoding: 'utf8', windowsHide: true, ...c.opts }));
}

test('claude.cmd from npm resolves to its node script; every argument arrives intact', () => {
  const bin = path.join(root, 'bin');
  fs.mkdirSync(path.join(bin, 'node_modules', 'claude'), { recursive: true });
  fs.writeFileSync(path.join(bin, 'node_modules', 'claude', 'cli.js'), ECHO);
  npmShim(bin, NPM_SHIM);
  const env = { PATH: bin, PATHEXT: '.COM;.EXE;.BAT;.CMD' };
  const args = [...AWKWARD, 'multi\nline "prompt"\r\nwith 100% ^ & |', 'x'.repeat(20000)];
  const c = tasks.claudeCommand(args, env, true);
  assert.equal(c.file, process.execPath);
  assert.deepEqual(runResolved(c), args);
});

test('claude.exe is preferred in PATH order and used as it is', () => {
  const a = path.join(root, 'a');
  const b = path.join(root, 'b');
  fs.mkdirSync(a);
  fs.mkdirSync(b);
  fs.writeFileSync(path.join(a, 'claude.exe'), '');
  fs.writeFileSync(path.join(b, 'claude.cmd'), '');
  const env = { PATH: [a, b].join(path.delimiter), PATHEXT: '.COM;.EXE;.BAT;.CMD' };
  assert.deepEqual(tasks.claudeCommand(['agents'], env, true), { file: path.join(a, 'claude.exe'), args: ['agents'], opts: {} });
  assert.equal(tasks.claudeCommand(['agents'], { PATH: path.join(root, 'none') }, true).file, 'claude');
  assert.equal(tasks.claudeCommand(['agents'], env, false).file, 'claude');
});

test('a shim csquad cannot read is run through cmd.exe, which refuses newlines clearly', { skip: onlyWindows }, () => {
  const bin = path.join(root, 'bin');
  fs.mkdirSync(bin);
  fs.writeFileSync(path.join(bin, 'cli.js'), ECHO);
  // The target hides behind a variable, so it cannot be resolved without running the shim.
  npmShim(bin, ['@ECHO off', 'SET "F=%~dp0cli.js"', 'node "%F%" %*']);
  const env = { ...process.env, PATH: bin + path.delimiter + process.env.PATH };
  const args = AWKWARD.filter((a) => a !== '%PATH%'); // %VAR% is expanded by cmd.exe itself
  const c = tasks.claudeCommand(args, env, true);
  assert.match(c.file, /cmd(\.exe)?$/i);
  assert.deepEqual(runResolved(c), args);
  assert.throws(() => tasks.claudeCommand(['a\nb'], env, true), /multi-line/);
});

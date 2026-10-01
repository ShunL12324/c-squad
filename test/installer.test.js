'use strict';
// Every test runs the CLI against a temporary CLAUDE_CONFIG_DIR (and a
// temporary HOME), never the real ~/.claude.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { test, beforeEach, afterEach } = require('node:test');

const CLI = path.join(__dirname, '..', 'bin', 'csquad.js');
const VERSION = require('../package.json').version;
const SKILLS = ['create-task', 'task-status', 'message-task', 'watch-task', 'finish-task'];

let root, dir, settings;

beforeEach(() => {
  root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'csquad-test-')));
  dir = path.join(root, 'claude');
  settings = path.join(dir, 'settings.json');
});
afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

function csquad(cmd, { configDir = dir, cwd = root } = {}) {
  const r = spawnSync(process.execPath, [CLI, cmd], {
    cwd,
    encoding: 'utf8',
    env: { ...process.env, CLAUDE_CONFIG_DIR: configDir, HOME: root },
  });
  return { code: r.status, out: r.stdout + r.stderr };
}

const ok = (cmd, opts) => {
  const r = csquad(cmd, opts);
  assert.equal(r.code, 0, r.out);
  return r.out;
};
const read = (...p) => fs.readFileSync(path.join(dir, ...p), 'utf8');
const json = (...p) => JSON.parse(read(...p));
const seed = (obj, indent = 2) => {
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(settings, JSON.stringify(obj, null, indent) + '\n');
};
const command = () => `node ${path.join(dir, 'csquad', 'tasks.js')} statusline`;

test('install into an empty dir', () => {
  ok('install');
  for (const f of ['tasks.js', 'worker.md', 'manifest.json']) assert.ok(fs.existsSync(path.join(dir, 'csquad', f)), f);
  for (const s of SKILLS) assert.ok(fs.existsSync(path.join(dir, 'skills', s, 'SKILL.md')), s);
  assert.equal(json('csquad', 'manifest.json').version, VERSION);
  assert.deepEqual(json('settings.json'), { statusLine: { type: 'command', command: command(), refreshInterval: 5 } });
  assert.equal(json('csquad', 'statusline.json').present, false);
  assert.ok(ok('status').includes('Status line: wrapped'));
});

test('install wraps an existing statusLine and keeps its refreshInterval', () => {
  const own = { type: 'command', command: 'echo mine', refreshInterval: 30 };
  seed({ theme: 'dark', statusLine: own, env: { A: '1' } }, 4);
  ok('install');
  const s = json('settings.json');
  assert.deepEqual(s.statusLine, { type: 'command', command: command(), refreshInterval: 30 });
  assert.deepEqual(Object.keys(s), ['theme', 'statusLine', 'env']);
  assert.match(read('settings.json'), /^ {4}"theme"/m, 'indentation preserved');
  assert.deepEqual(json('csquad', 'statusline.json'), { present: true, statusLine: own });
  assert.ok(fs.existsSync(path.join(dir, 'csquad', 'settings.json.bak')));
});

test('install defaults refreshInterval to 5 when the original has none', () => {
  seed({ statusLine: { type: 'command', command: 'echo mine' } });
  ok('install');
  assert.equal(json('settings.json').statusLine.refreshInterval, 5);
});

test('reinstall is a no-op and does not wrap twice', () => {
  seed({ statusLine: { type: 'command', command: 'echo mine' } });
  ok('install');
  const before = fs.readdirSync(dir, { recursive: true }).sort().map((f) => [f, fs.statSync(path.join(dir, f)).isFile() ? read(f) : '']);
  const out = ok('install');
  const after = fs.readdirSync(dir, { recursive: true }).sort().map((f) => [f, fs.statSync(path.join(dir, f)).isFile() ? read(f) : '']);
  assert.deepEqual(after, before);
  assert.equal(json('csquad', 'statusline.json').statusLine.command, 'echo mine');
  assert.doesNotMatch(out, /edited/);
});

test('update keeps config.json and backs up user-edited files', () => {
  ok('install');
  const config = '{"permissionMode":"plan"}\n';
  fs.writeFileSync(path.join(dir, 'csquad', 'config.json'), config);
  const skill = path.join(dir, 'skills', 'create-task', 'SKILL.md');
  const original = fs.readFileSync(skill, 'utf8');
  fs.writeFileSync(skill, original + '\nmy edit\n');

  const out = ok('update');
  assert.match(out, /skills\/create-task\/SKILL\.md was edited/);
  assert.equal(read('csquad', 'config.json'), config);
  assert.equal(fs.readFileSync(skill, 'utf8'), original);
  assert.equal(fs.readFileSync(skill + '.bak', 'utf8'), original + '\nmy edit\n');
});

test('update requires an existing installation', () => {
  const r = csquad('update');
  assert.equal(r.code, 1);
  assert.match(r.out, /not installed/);
});

test('uninstall restores settings.json byte for byte', () => {
  seed({ model: 'opus', statusLine: { type: 'command', command: 'echo mine', refreshInterval: 2 }, hooks: {} }, 4);
  const before = read('settings.json');
  ok('install');
  fs.writeFileSync(path.join(dir, 'csquad', 'config.json'), '{}');
  ok('uninstall');
  assert.equal(read('settings.json'), before);
  assert.ok(!fs.existsSync(path.join(dir, 'skills', 'create-task')));
  assert.ok(!fs.existsSync(path.join(dir, 'csquad', 'tasks.js')));
  // The user's config.json is left alone, so the directory stays.
  assert.ok(fs.existsSync(path.join(dir, 'csquad', 'config.json')));
});

test('install migrates the hand-made setup that saved {"command": ...}', () => {
  const wrapper = `node ${path.join(dir, 'csquad', 'tasks.js')} statusline`;
  seed({ statusLine: { type: 'command', command: wrapper, refreshInterval: 5 } });
  fs.mkdirSync(path.join(dir, 'csquad'), { recursive: true });
  fs.writeFileSync(path.join(dir, 'csquad', 'statusline.json'), '{"command":"echo mine"}');
  ok('install');
  assert.deepEqual(json('csquad', 'statusline.json'), { present: true, statusLine: { type: 'command', command: 'echo mine' } });
  ok('uninstall');
  assert.deepEqual(json('settings.json').statusLine, { type: 'command', command: 'echo mine' });
});

test('uninstall removes files tasks.js generated', () => {
  ok('install');
  fs.mkdirSync(path.join(dir, 'csquad', 'prompts'));
  fs.writeFileSync(path.join(dir, 'csquad', 'prompts', 'T3.md'), 'big prompt');
  fs.mkdirSync(path.join(dir, 'csquad', 'launch.lock.takeover'));
  ok('uninstall');
  assert.ok(!fs.existsSync(path.join(dir, 'csquad')));
});

test('uninstall removes the statusLine key when there was none, and the dirs', () => {
  const content = JSON.stringify({ theme: 'dark' }, null, 2) + '\n';
  seed({ theme: 'dark' });
  ok('install');
  ok('uninstall');
  assert.equal(read('settings.json'), content);
  assert.ok(!fs.existsSync(path.join(dir, 'csquad')));
  assert.ok(!fs.existsSync(path.join(dir, 'skills')) || fs.readdirSync(path.join(dir, 'skills')).length === 0);
});

test('uninstall from an empty dir removes the settings.json it created', () => {
  ok('install');
  ok('uninstall');
  assert.ok(!fs.existsSync(settings));
});

test('uninstall leaves a statusLine the user replaced', () => {
  ok('install');
  const s = json('settings.json');
  s.statusLine = { type: 'command', command: 'echo replaced' };
  fs.writeFileSync(settings, JSON.stringify(s, null, 2) + '\n');
  const before = read('settings.json');
  const out = ok('uninstall');
  assert.equal(read('settings.json'), before);
  assert.match(out, /left as it is/);
  assert.ok(!fs.existsSync(path.join(dir, 'csquad', 'tasks.js')));
});

test('paths follow CLAUDE_CONFIG_DIR, including ones with spaces', () => {
  const odd = path.join(root, 'my claude');
  ok('install', { configDir: odd });
  const skill = fs.readFileSync(path.join(odd, 'skills', 'create-task', 'SKILL.md'), 'utf8');
  assert.ok(skill.includes(`node '${path.join(odd, 'csquad')}'/tasks.js launch`));
  assert.ok(!skill.includes('{{CSQUAD_DIR}}'));
  const sl = JSON.parse(fs.readFileSync(path.join(odd, 'settings.json'), 'utf8')).statusLine;
  assert.equal(sl.command, `node '${path.join(odd, 'csquad', 'tasks.js')}' statusline`);
  ok('uninstall', { configDir: odd });
  assert.ok(!fs.existsSync(path.join(odd, 'csquad')));

  ok('install');
  assert.ok(read('skills', 'task-status', 'SKILL.md').includes(`node ${path.join(dir, 'csquad')}/tasks.js status`));
});

test('invalid settings.json aborts without changes', () => {
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(settings, '{ nope');
  const r = csquad('install');
  assert.equal(r.code, 1);
  assert.equal(read('settings.json'), '{ nope');
  assert.ok(!fs.existsSync(path.join(dir, 'csquad')));
});

test('warns when the project overrides statusLine', () => {
  const project = path.join(root, 'project');
  fs.mkdirSync(path.join(project, '.claude'), { recursive: true });
  fs.writeFileSync(path.join(project, '.claude', 'settings.json'), '{"statusLine":{"type":"command","command":"x"}}');
  assert.match(ok('install', { cwd: project }), /overrides the csquad wrapper/);
});

test('--flag spellings, --version and --help', () => {
  assert.equal(ok('--version').trim(), VERSION);
  assert.match(ok('--help'), /Usage: npx csquad/);
  ok('--install');
  assert.match(ok('--update'), /Updated/);
  ok('--uninstall');
  assert.equal(csquad('bogus').code, 2);
});

test('CRLF and compact settings.json keep their format through install and uninstall', () => {
  fs.mkdirSync(dir, { recursive: true });
  const crlf = '{\r\n  "theme": "dark",\r\n  "statusLine": {\r\n    "type": "command",\r\n    "command": "echo mine"\r\n  }\r\n}\r\n';
  fs.writeFileSync(settings, crlf);
  ok('install');
  const wrapped = read('settings.json');
  assert.ok(wrapped.includes(command()));
  assert.equal(wrapped.replace(/\r\n/g, '').includes('\n'), false, 'only CRLF line breaks');
  ok('uninstall');
  assert.equal(read('settings.json'), crlf);

  const compact = '{"theme":"dark","statusLine":{"type":"command","command":"echo mine"}}';
  fs.writeFileSync(settings, compact);
  ok('install');
  assert.equal(read('settings.json').includes('\n'), false);
  ok('uninstall');
  assert.equal(read('settings.json'), compact);
});

test('a symlinked settings.json stays a symlink; its target is edited and restored', () => {
  const target = path.join(root, 'dotfiles-settings.json');
  fs.mkdirSync(dir, { recursive: true });
  fs.writeFileSync(target, JSON.stringify({ theme: 'dark' }, null, 2) + '\n');
  fs.symlinkSync(target, settings);
  const before = fs.readFileSync(target, 'utf8');
  ok('install');
  assert.ok(fs.lstatSync(settings).isSymbolicLink());
  assert.ok(JSON.parse(fs.readFileSync(target, 'utf8')).statusLine.command.includes('tasks.js'));
  ok('uninstall');
  assert.ok(fs.lstatSync(settings).isSymbolicLink());
  assert.equal(fs.readFileSync(target, 'utf8'), before);
});

test('status survives an invalid settings.json and a manifest without files', () => {
  ok('install');
  fs.writeFileSync(settings, '{ nope');
  assert.match(ok('status'), /Status line: unknown/);
  fs.writeFileSync(path.join(dir, 'csquad', 'manifest.json'), '{"version":"0"}');
  assert.match(ok('status'), /Installed:\s+csquad 0/);
});

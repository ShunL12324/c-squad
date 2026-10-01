'use strict';
// Symlink layouts (dotfile setups) in the Claude config dir: links must never
// be replaced and nothing may be written outside what the link points at.

const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawnSync } = require('node:child_process');
const { test, beforeEach, afterEach } = require('node:test');

const CLI = path.join(__dirname, '..', 'bin', 'csquad.js');
const SKILLS = ['create-task', 'task-status', 'message-task', 'watch-task', 'finish-task'];

let root, dir, settings;

beforeEach(() => {
  root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'csquad-test-')));
  dir = path.join(root, 'claude');
  settings = path.join(dir, 'settings.json');
});
afterEach(() => fs.rmSync(root, { recursive: true, force: true }));

function csquad(cmd) {
  const r = spawnSync(process.execPath, [CLI, cmd], {
    cwd: root,
    encoding: 'utf8',
    env: { ...process.env, CLAUDE_CONFIG_DIR: dir, HOME: root },
  });
  return { code: r.status, out: r.stdout + r.stderr };
}

const ok = (cmd) => {
  const r = csquad(cmd);
  assert.equal(r.code, 0, r.out);
  return r.out;
};
const lstat = (p) => fs.lstatSync(p, { throwIfNoEntry: false });
const snapshot = (d) => fs.readdirSync(d, { recursive: true }).sort();
const command = () => `node ${path.join(dir, 'csquad', 'tasks.js')} statusline`;
const readJSON = (...p) => JSON.parse(fs.readFileSync(path.join(...p), 'utf8'));

test('dangling settings.json symlink: install writes through, uninstall leaves the link dangling', () => {
  const dots = path.join(root, 'dotfiles');
  fs.mkdirSync(dots);
  fs.mkdirSync(dir);
  fs.symlinkSync(path.join(dots, 'settings.json'), settings);
  ok('install');
  assert.ok(lstat(settings).isSymbolicLink(), 'link kept');
  assert.equal(fs.readlinkSync(settings), path.join(dots, 'settings.json'));
  assert.equal(readJSON(dots, 'settings.json').statusLine.command, command());
  assert.deepEqual(fs.readdirSync(dots), ['settings.json'], 'no temp files left');
  ok('uninstall');
  assert.ok(lstat(settings).isSymbolicLink(), 'link kept');
  assert.equal(fs.existsSync(path.join(dots, 'settings.json')), false, 'file install created is removed');
  assert.equal(fs.existsSync(path.join(dir, 'csquad')), false);
});

test('dangling relative settings.json symlink resolves against the link directory', () => {
  fs.mkdirSync(path.join(dir, 'real'), { recursive: true });
  fs.symlinkSync(path.join('real', 'settings.json'), settings);
  ok('install');
  assert.ok(lstat(settings).isSymbolicLink());
  assert.equal(fs.readlinkSync(settings), path.join('real', 'settings.json'));
  assert.equal(readJSON(dir, 'real', 'settings.json').statusLine.command, command());
  ok('uninstall');
  assert.ok(lstat(settings).isSymbolicLink());
  assert.equal(fs.existsSync(path.join(dir, 'real', 'settings.json')), false);
});

test('dangling settings.json symlink into a missing directory is refused with nothing changed', () => {
  fs.mkdirSync(dir);
  const target = path.join(root, 'nowhere', 'settings.json');
  fs.symlinkSync(target, settings);
  const before = snapshot(dir);
  const r = csquad('install');
  assert.notEqual(r.code, 0);
  assert.match(r.out, /symlink/);
  assert.match(r.out, /nothing was changed/);
  assert.deepEqual(snapshot(dir), before);
  assert.equal(fs.readlinkSync(settings), target);
  assert.equal(fs.existsSync(path.join(root, 'nowhere')), false);
});

test('settings.json symlinks to a file (relative, chained) survive install and uninstall', () => {
  const dots = path.join(root, 'dotfiles');
  fs.mkdirSync(dots);
  fs.mkdirSync(dir);
  const real = path.join(dots, 'settings.json');
  const original = JSON.stringify({ theme: 'dark', statusLine: { type: 'command', command: 'echo mine' } }, null, 4) + '\n';
  fs.writeFileSync(real, original);
  fs.symlinkSync(real, path.join(dots, 'hop.json'));
  fs.symlinkSync(path.relative(dir, path.join(dots, 'hop.json')), settings);
  ok('install');
  assert.ok(lstat(settings).isSymbolicLink());
  assert.ok(lstat(path.join(dots, 'hop.json')).isSymbolicLink());
  assert.equal(readJSON(real).statusLine.command, command());
  ok('uninstall');
  assert.ok(lstat(settings).isSymbolicLink());
  assert.equal(fs.readFileSync(real, 'utf8'), original);
});

test('symlinked csquad/ and skills/ directories are written through and keep their links', () => {
  const store = path.join(root, 'store');
  fs.mkdirSync(path.join(store, 'csquad'), { recursive: true });
  fs.mkdirSync(path.join(store, 'skills'));
  fs.mkdirSync(dir);
  fs.symlinkSync(path.join(store, 'csquad'), path.join(dir, 'csquad'));
  fs.symlinkSync(path.join(store, 'skills'), path.join(dir, 'skills'));
  ok('install');
  assert.ok(lstat(path.join(dir, 'csquad')).isSymbolicLink());
  assert.ok(lstat(path.join(dir, 'skills')).isSymbolicLink());
  assert.ok(fs.existsSync(path.join(store, 'csquad', 'tasks.js')));
  for (const s of SKILLS) assert.ok(fs.existsSync(path.join(store, 'skills', s, 'SKILL.md')), s);
  assert.deepEqual(fs.readdirSync(root).sort(), ['claude', 'store']);
  ok('uninstall');
  assert.ok(lstat(path.join(dir, 'csquad')).isSymbolicLink());
  assert.ok(lstat(path.join(dir, 'skills')).isSymbolicLink());
  assert.deepEqual(fs.readdirSync(path.join(store, 'csquad')), []);
  assert.deepEqual(fs.readdirSync(path.join(store, 'skills')), []);
});

test('a dangling csquad/ symlink is refused with nothing changed', () => {
  fs.mkdirSync(dir);
  fs.symlinkSync(path.join(root, 'gone'), path.join(dir, 'csquad'));
  const r = csquad('install');
  assert.notEqual(r.code, 0);
  assert.match(r.out, /dangling symlink/);
  assert.deepEqual(snapshot(dir), ['csquad']);
  assert.equal(fs.existsSync(path.join(root, 'gone')), false);
});

test('a symlinked file csquad manages is refused, not replaced or written through', () => {
  const elsewhere = path.join(root, 'elsewhere.js');
  fs.writeFileSync(elsewhere, 'mine');
  fs.mkdirSync(path.join(dir, 'csquad'), { recursive: true });
  fs.symlinkSync(elsewhere, path.join(dir, 'csquad', 'tasks.js'));
  const r = csquad('install');
  assert.notEqual(r.code, 0);
  assert.match(r.out, /is a symlink/);
  assert.equal(fs.readFileSync(elsewhere, 'utf8'), 'mine');
  assert.ok(lstat(path.join(dir, 'csquad', 'tasks.js')).isSymbolicLink());
  assert.equal(fs.existsSync(settings), false);
});

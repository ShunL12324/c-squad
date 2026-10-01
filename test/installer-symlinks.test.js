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

// Windows needs a privilege (or developer mode) to create file symlinks.
const canSymlink = (() => {
  const d = fs.mkdtempSync(path.join(os.tmpdir(), 'csquad-link-'));
  try {
    fs.writeFileSync(path.join(d, 'a'), '');
    fs.symlinkSync(path.join(d, 'a'), path.join(d, 'b'));
    return true;
  } catch {
    return false;
  } finally {
    fs.rmSync(d, { recursive: true, force: true });
  }
})();
const t = (name, fn) => test(name, { skip: !canSymlink && 'symlinks are not permitted here' }, fn);

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
    env: { ...process.env, CLAUDE_CONFIG_DIR: dir, HOME: root, USERPROFILE: root },
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
const command = () => {
  const p = path.join(dir, 'csquad', 'tasks.js');
  if (process.platform !== 'win32') return `node ${p} statusline`;
  const f = p.replace(/\\/g, '/');
  return `node ${/[^\w@+=:,./~-]/.test(f) ? `"${f}"` : f} statusline`;
};
const readJSON = (...p) => JSON.parse(fs.readFileSync(path.join(...p), 'utf8'));

t('dangling settings.json symlink: install writes through, uninstall leaves the link dangling', () => {
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

t('dangling relative settings.json symlink resolves against the link directory', () => {
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

t('dangling settings.json symlink into a missing directory is refused with nothing changed', () => {
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

t('settings.json symlinks to a file (relative, chained) survive install and uninstall', () => {
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

t('symlinked csquad/ and skills/ directories are written through and keep their links', () => {
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

t('a dangling csquad/ symlink is refused with nothing changed', () => {
  fs.mkdirSync(dir);
  fs.symlinkSync(path.join(root, 'gone'), path.join(dir, 'csquad'));
  const r = csquad('install');
  assert.notEqual(r.code, 0);
  assert.match(r.out, /dangling symlink/);
  assert.deepEqual(snapshot(dir), ['csquad']);
  assert.equal(fs.existsSync(path.join(root, 'gone')), false);
});

t('a symlinked file csquad manages is refused, not replaced or written through', () => {
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

// Junctions need no privilege on Windows (elsewhere they are plain symlinks);
// a skills/ directory shared with other tools is often one.
test('a skills/ junction is written through; uninstall removes only our skills and keeps the link', () => {
  const shared = path.join(root, 'shared-skills');
  fs.mkdirSync(path.join(shared, 'their-skill'), { recursive: true });
  fs.writeFileSync(path.join(shared, 'their-skill', 'SKILL.md'), 'theirs');
  fs.mkdirSync(dir);
  fs.symlinkSync(shared, path.join(dir, 'skills'), 'junction');
  ok('install');
  assert.ok(lstat(path.join(dir, 'skills')).isSymbolicLink());
  for (const s of SKILLS) assert.ok(fs.existsSync(path.join(shared, s, 'SKILL.md')), s);
  ok('uninstall');
  assert.ok(lstat(path.join(dir, 'skills')).isSymbolicLink(), 'link kept');
  assert.deepEqual(fs.readdirSync(shared), ['their-skill']);
  assert.equal(fs.readFileSync(path.join(shared, 'their-skill', 'SKILL.md'), 'utf8'), 'theirs');
});

test('uninstall keeps an emptied skills/ junction', () => {
  const shared = path.join(root, 'shared-skills');
  fs.mkdirSync(shared);
  fs.mkdirSync(dir);
  fs.symlinkSync(shared, path.join(dir, 'skills'), 'junction');
  ok('install');
  ok('uninstall');
  assert.ok(lstat(path.join(dir, 'skills')).isSymbolicLink(), 'link kept');
  assert.deepEqual(fs.readdirSync(shared), []);
});

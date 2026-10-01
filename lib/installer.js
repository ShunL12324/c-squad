'use strict';
// Install, update, uninstall and inspect the C Squad files in a Claude config
// directory. Everything here is plain fs; no dependencies.

const crypto = require('crypto');
const fs = require('fs');
const os = require('os');
const path = require('path');

const PAYLOAD = path.join(__dirname, '..', 'payload');
const VERSION = require('../package.json').version;
const PLACEHOLDER = '{{CSQUAD_DIR}}';
const DEFAULT_REFRESH = 5;

// Files the installer writes itself, relative to the Claude config dir.
const GENERATED = ['csquad/manifest.json', 'csquad/statusline.json', 'csquad/settings.json.bak', 'csquad/launch.lock'];

function claudeDir(env = process.env) {
  return path.resolve(env.CLAUDE_CONFIG_DIR || path.join(os.homedir(), '.claude'));
}

// shellPath quotes a path for sh only when it needs it.
function shellPath(p) {
  return /^[\w@%+=:,./-]+$/.test(p) ? p : `'${p.replace(/'/g, `'\\''`)}'`;
}

function statuslineCommand(dir) {
  return `node ${shellPath(path.join(dir, 'csquad', 'tasks.js'))} statusline`;
}

// Any C Squad wrapper, whatever directory it points at.
function isOurs(statusLine) {
  return !!statusLine && typeof statusLine.command === 'string' && /csquad[\\/]tasks\.js'? statusline$/.test(statusLine.command);
}

const sha = (buf) => crypto.createHash('sha256').update(buf).digest('hex');

function walk(root, rel = '') {
  const out = [];
  for (const e of fs.readdirSync(path.join(root, rel), { withFileTypes: true })) {
    const r = path.join(rel, e.name);
    if (e.isDirectory()) out.push(...walk(root, r));
    else out.push(r);
  }
  return out;
}

// payloadFiles returns [relative path, rendered content] for everything we
// ship, with the install location templated into the skills.
function payloadFiles(dir) {
  const csquadDir = shellPath(path.join(dir, 'csquad'));
  return walk(PAYLOAD).sort().map((rel) => [
    rel.split(path.sep).join('/'),
    Buffer.from(fs.readFileSync(path.join(PAYLOAD, rel), 'utf8').split(PLACEHOLDER).join(csquadDir)),
  ]);
}

function readJSON(file) {
  try {
    return JSON.parse(fs.readFileSync(file, 'utf8'));
  } catch {
    return null;
  }
}

// writeAtomic writes through a temp file and rename, following a symlinked
// target (dotfile setups) and keeping its permissions.
function writeAtomic(file, data) {
  const target = fs.existsSync(file) ? fs.realpathSync(file) : file;
  const mode = fs.statSync(target, { throwIfNoEntry: false })?.mode;
  const tmp = `${target}.csquad-${process.pid}.tmp`;
  fs.mkdirSync(path.dirname(target), { recursive: true });
  fs.writeFileSync(tmp, data);
  if (mode !== undefined) fs.chmodSync(tmp, mode & 0o7777);
  fs.renameSync(tmp, target);
}

// ---- settings.json ---------------------------------------------------------

// loadSettings parses settings.json, remembering its indentation and final
// newline so a rewrite only changes the key we touch.
function loadSettings(file) {
  if (!fs.existsSync(file)) return { exists: false, data: {}, indent: 2, newline: true, raw: '' };
  const raw = fs.readFileSync(file, 'utf8');
  let data;
  try {
    data = JSON.parse(raw);
  } catch (e) {
    throw new Error(`${file} is not valid JSON (${e.message}); fix it first, nothing was changed`);
  }
  if (!data || typeof data !== 'object' || Array.isArray(data)) throw new Error(`${file} is not a JSON object`);
  const m = raw.match(/^([ \t]+)"/m);
  return { exists: true, data, indent: m ? m[1] : 2, newline: raw.endsWith('\n'), raw };
}

function saveSettings(file, s) {
  writeAtomic(file, JSON.stringify(s.data, null, s.indent) + (s.newline ? '\n' : ''));
}

// ---- commands --------------------------------------------------------------

// install copies the files and wraps the status line. It also serves update.
function install(dir, log, { update = false } = {}) {
  const csquad = path.join(dir, 'csquad');
  const manifestFile = path.join(csquad, 'manifest.json');
  const old = readJSON(manifestFile);
  if (update && !old) throw new Error(`csquad is not installed in ${dir}; run: npx csquad@latest install`);
  const oldFiles = (old && old.files) || {};
  const files = {};

  // Parse settings first so invalid JSON aborts before anything is written.
  loadSettings(path.join(dir, 'settings.json'));

  for (const [rel, content] of payloadFiles(dir)) {
    const dest = path.join(dir, rel);
    const hash = sha(content);
    const current = fs.existsSync(dest) ? fs.readFileSync(dest) : null;
    if (current && sha(current) !== hash && sha(current) !== oldFiles[rel]) {
      // Not what we installed last time: keep the user's version.
      fs.copyFileSync(dest, dest + '.bak');
      log(`${rel} was edited; saved your version as ${rel}.bak`);
    }
    if (!current || sha(current) !== hash) {
      fs.mkdirSync(path.dirname(dest), { recursive: true });
      fs.writeFileSync(dest, content, { mode: rel.endsWith('.js') ? 0o755 : 0o644 });
    }
    files[rel] = hash;
  }
  // Files an older version shipped that this one no longer does.
  for (const rel of Object.keys(oldFiles)) if (!files[rel]) removeManaged(dir, rel, oldFiles[rel], log);

  wrapStatusLine(dir, log);
  writeAtomic(manifestFile, JSON.stringify({ version: VERSION, files }, null, 2) + '\n');
  log(`${update ? 'Updated' : 'Installed'} csquad ${VERSION} in ${dir}`);
  warnProjectOverride(log);
}

function wrapStatusLine(dir, log) {
  const csquad = path.join(dir, 'csquad');
  const file = path.join(dir, 'settings.json');
  const s = loadSettings(file);
  const savedFile = path.join(csquad, 'statusline.json');
  const command = statuslineCommand(dir);

  if (isOurs(s.data.statusLine)) {
    // Already wrapped: never wrap twice, and keep the saved original.
    if (s.data.statusLine.command !== command) {
      s.data.statusLine.command = command;
      saveSettings(file, s);
    }
    if (!fs.existsSync(savedFile)) writeAtomic(savedFile, JSON.stringify({ present: false }, null, 2) + '\n');
    return;
  }
  const original = s.data.statusLine;
  const saved = original === undefined ? { present: false } : { present: true, statusLine: original };
  if (!s.exists) saved.createdSettings = true;
  writeAtomic(savedFile, JSON.stringify(saved, null, 2) + '\n');
  const backup = path.join(csquad, 'settings.json.bak');
  if (s.exists && !fs.existsSync(backup)) fs.writeFileSync(backup, s.raw);

  const refresh = original && typeof original.refreshInterval === 'number' ? original.refreshInterval : DEFAULT_REFRESH;
  s.data.statusLine = { type: 'command', command, refreshInterval: refresh };
  saveSettings(file, s);
  log(original === undefined ? 'Status line: set' : 'Status line: wrapped your own (saved to csquad/statusline.json)');
}

// removeManaged deletes a file we installed; an edited one is renamed to .bak.
function removeManaged(dir, rel, expectedHash, log) {
  const file = path.join(dir, rel);
  if (!fs.existsSync(file)) return;
  if (sha(fs.readFileSync(file)) !== expectedHash) {
    fs.renameSync(file, file + '.bak');
    log(`${rel} was edited; kept your version as ${rel}.bak`);
  } else {
    fs.rmSync(file);
  }
  // Drop the skill directory once it is empty.
  const parent = path.dirname(file);
  if (parent !== dir && parent !== path.join(dir, 'csquad')) rmdirIfEmpty(parent);
}

function rmdirIfEmpty(d) {
  try {
    fs.rmdirSync(d);
  } catch {}
}

function uninstall(dir, log) {
  const csquad = path.join(dir, 'csquad');
  const manifest = readJSON(path.join(csquad, 'manifest.json'));
  if (!manifest) throw new Error(`csquad is not installed in ${dir}`);

  const file = path.join(dir, 'settings.json');
  const saved = readJSON(path.join(csquad, 'statusline.json'));
  const s = loadSettings(file);
  if (s.data.statusLine && s.data.statusLine.command === statuslineCommand(dir)) {
    if (saved && saved.present) s.data.statusLine = saved.statusLine;
    else delete s.data.statusLine;
    if (saved && saved.createdSettings && !Object.keys(s.data).length) fs.rmSync(fs.realpathSync(file));
    else saveSettings(file, s);
    log('Status line: restored');
  } else if (s.exists) {
    log('Status line: settings.json no longer points at the csquad wrapper; left as it is');
  }

  for (const [rel, hash] of Object.entries(manifest.files)) removeManaged(dir, rel, hash, log);
  for (const rel of GENERATED) fs.rmSync(path.join(dir, rel), { force: true });
  rmdirIfEmpty(csquad);
  rmdirIfEmpty(path.join(dir, 'skills'));
  if (fs.existsSync(csquad)) log(`Left ${csquad} in place; it still holds: ${fs.readdirSync(csquad).join(', ')}`);
  log(`Uninstalled csquad from ${dir}. Running task sessions were not touched.`);
}

function status(dir, log) {
  const csquad = path.join(dir, 'csquad');
  const manifest = readJSON(path.join(csquad, 'manifest.json'));
  log(`Config dir:  ${dir}`);
  log(`Package:     csquad ${VERSION}`);
  if (!manifest) {
    log('Installed:   no');
    return;
  }
  log(`Installed:   csquad ${manifest.version}${manifest.version === VERSION ? '' : ` (this package is ${VERSION}; run update)`}`);
  for (const [rel, hash] of Object.entries(manifest.files)) {
    const f = path.join(dir, rel);
    const state = !fs.existsSync(f) ? 'missing' : sha(fs.readFileSync(f)) === hash ? 'ok' : 'edited';
    log(`  ${state.padEnd(8)} ${rel}`);
  }
  const sl = loadSettings(path.join(dir, 'settings.json')).data.statusLine;
  const saved = readJSON(path.join(csquad, 'statusline.json'));
  if (sl && sl.command === statuslineCommand(dir)) {
    const orig = saved && saved.present ? JSON.stringify(saved.statusLine.command || saved.statusLine) : 'none';
    log(`Status line: wrapped (your original: ${orig})`);
  } else {
    log('Status line: not wrapped');
  }
  warnProjectOverride(log);
}

// A project-level statusLine wins over the user-level wrapper.
function warnProjectOverride(log, cwd = process.cwd()) {
  for (const name of ['settings.json', 'settings.local.json']) {
    const f = path.join(cwd, '.claude', name);
    const data = readJSON(f);
    // The config dir itself is user-level, not a project.
    if (data && data.statusLine && path.dirname(f) !== claudeDir()) {
      log(`Warning: ${f} sets statusLine and overrides the csquad wrapper in this project.`);
    }
  }
}

module.exports = { install, uninstall, status, claudeDir, statuslineCommand, VERSION };

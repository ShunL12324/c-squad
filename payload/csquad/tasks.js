#!/usr/bin/env node
// Background tasks for Claude Code: sessions started with `claude --bg` and
// named "T12 · title". Used by the create-task, task-status, message-task and
// finish-task skills and by the status line.
//
//   tasks.js launch --title TITLE [--cwd DIR] [--model M] [--prompt-file FILE [--rm-prompt]]
//                                  (the prompt comes from FILE, or from stdin)
//   tasks.js status [--all]        (this session's tasks; --all lists every task)
//   tasks.js peek T12 [-n N] [--all]   (refuses tasks other sessions launched)
//   tasks.js wait T12 [T13 ...] [--timeout MIN] [--all]   (opt-in monitor: exits
//                                  when a named task needs input, finishes or vanishes)
//   tasks.js statusline            (status line command; reads Claude's JSON)
'use strict';

const fs = require('fs');
const os = require('os');
const path = require('path');
const { execFileSync, spawnSync } = require('child_process');

const IS_WIN = process.platform === 'win32';
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

// ---- running claude and the user's shell ------------------------------------

// findOnPath looks for `name` in PATH. On Windows it tries the PATHEXT
// extensions that can be started directly (an extensionless file such as npm's
// sh shim cannot be).
function findOnPath(name, env = process.env, win = IS_WIN) {
  const dirs = (env.PATH || env.Path || '').split(path.delimiter).map((d) => d.replace(/^"|"$/g, '')).filter(Boolean);
  const exts = win ? (env.PATHEXT || '.COM;.EXE;.BAT;.CMD').split(';').filter((e) => /^\.(com|exe|bat|cmd)$/i.test(e)) : [''];
  for (const dir of dirs) {
    for (const ext of exts) {
      const file = path.join(dir, name + ext.toLowerCase());
      if (fs.statSync(file, { throwIfNoEntry: false })?.isFile()) return file;
    }
  }
  return null;
}

// shimTarget returns the program an npm-style .cmd shim runs, so that it can be
// started directly: cmd.exe cannot pass newlines in arguments and has its own
// quoting rules. npm shims end with: "%_prog%"  "%dp0%\path\to\target" %*
function shimTarget(shim) {
  let text;
  try {
    text = fs.readFileSync(shim, 'utf8');
  } catch {
    return null;
  }
  const hits = [...text.matchAll(/"%~?dp0%?[\\/]*([^"%\r\n]+)"\s+%\*/gi)];
  if (!hits.length) return null;
  const target = path.join(path.dirname(shim), ...hits[hits.length - 1][1].split(/[\\/]/));
  return fs.existsSync(target) ? target : null;
}

// cmdQuote escapes one argument for a command line that cmd.exe parses (the
// cross-spawn algorithm). `twice` is for batch files, which parse it again.
function cmdQuote(arg, twice) {
  let a = String(arg);
  a = a.replace(/(\\*)"/g, '$1$1\\"').replace(/(\\+)$/, '$1$1');
  a = `"${a}"`.replace(/[()%!^"<>&|;, ]/g, '^$&');
  return twice ? a.replace(/[()%!^"<>&|;, ]/g, '^$&') : a;
}

// claudeCommand returns what to spawn for `claude args`: plain `claude`
// outside Windows; there the executable found on PATH. A claude.cmd shim is
// resolved to the program behind it, and only as a last resort run through
// cmd.exe, which cannot carry newlines or very long command lines.
function claudeCommand(args, env = process.env, win = IS_WIN) {
  if (!win) return { file: 'claude', args, opts: {} };
  const found = findOnPath('claude', env, true);
  if (!found) return { file: 'claude', args, opts: {} };
  if (/\.(exe|com)$/i.test(found)) return { file: found, args, opts: {} };
  const target = shimTarget(found);
  if (target && /\.exe$/i.test(target)) return { file: target, args, opts: {} };
  if (target && /\.[cm]?js$/i.test(target)) return { file: process.execPath, args: [target, ...args], opts: {} };
  if (args.some((a) => /[\r\n]/.test(a))) {
    throw new Error(`${found} is a batch file, which cannot pass multi-line arguments; put claude.exe on PATH (the native installer provides it)`);
  }
  const line = [found.replace(/[()%!^"<>&|;, ]/g, '^$&'), ...args.map((a) => cmdQuote(a, true))].join(' ');
  if (line.length > 8000) throw new Error(`${found} is a batch file and the command line is too long for cmd.exe; put claude.exe on PATH`);
  return { file: env.ComSpec || 'cmd.exe', args: ['/d', '/s', '/c', `"${line}"`], opts: { windowsVerbatimArguments: true } };
}

function claude(args, opts = {}) {
  const c = claudeCommand(args);
  // maxBuffer: the default 1 MB is too small for a long `agents --all` list.
  return execFileSync(c.file, c.args, { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'], timeout: 60000, maxBuffer: 256 << 20, windowsHide: true, ...c.opts, ...opts });
}

// gitBash finds Git for Windows' bash, which Claude Code runs status line
// commands with on Windows.
function gitBash(env = process.env) {
  const candidates = [env.CLAUDE_CODE_GIT_BASH_PATH];
  const git = findOnPath('git', env, true);
  if (git) candidates.push(path.join(path.dirname(git), '..', 'bin', 'bash.exe'), path.join(path.dirname(git), '..', '..', 'bin', 'bash.exe'));
  for (const base of [env.ProgramFiles, env['ProgramFiles(x86)'], env.LOCALAPPDATA && path.join(env.LOCALAPPDATA, 'Programs')]) {
    if (base) candidates.push(path.join(base, 'Git', 'bin', 'bash.exe'));
  }
  return candidates.find((c) => c && fs.statSync(c, { throwIfNoEntry: false })?.isFile()) || null;
}

// runShell runs a status line command the way Claude Code does: through sh,
// or on Windows through Git Bash (cmd.exe when Git is not installed).
function runShell(command, input) {
  const opts = { input, encoding: 'utf8', timeout: 3000, windowsHide: true };
  if (!IS_WIN) return spawnSync('sh', ['-c', command], opts);
  const bash = gitBash();
  return bash ? spawnSync(bash, ['-c', command], opts) : spawnSync(command, { ...opts, shell: true });
}

// readStdin returns all of stdin; an absent or closed stdin reads as empty.
function readStdin() {
  try {
    return fs.readFileSync(0);
  } catch (e) {
    if (e.code === 'EOF' || e.code === 'EAGAIN' || e.code === 'EBADF') return Buffer.alloc(0);
    throw e;
  }
}

// tasks returns background sessions named like tasks, newest first.
function tasks(timeout) {
  const sessions = JSON.parse(claude(['agents', '--json', '--all'], timeout ? { timeout } : {}));
  const out = [];
  for (const s of sessions) {
    const m = s.kind === 'background' && s.name && s.name.match(NAME);
    if (m) out.push({ ...s, num: Number(m[1]), label: 'T' + m[1], title: m[2] });
  }
  return out.sort((a, b) => b.num - a.num);
}

function within(dir, root) {
  if (IS_WIN) {
    dir = dir.toLowerCase().replace(/\//g, '\\');
    root = root.toLowerCase().replace(/\//g, '\\');
  }
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
// see each other's sessions and never pick the same number. The lock file
// holds its owner's pid and is stale once that process is gone. A launch takes
// at most ~3 minutes (listing, then claude --bg), so a live-pid lock that old
// is a reused pid or a hung process.
//
// The lock is created by hard-linking a fully written temp file, so it never
// exists empty and exactly one linker wins. Removing a stale lock is itself
// serialized by a mkdir mutex, with a re-check inside: otherwise two launchers
// could both judge the same lock stale and the slower one would delete the
// fresh lock the faster one had just taken.
function withLock(fn) {
  const lock = path.join(HERE, 'launch.lock');
  const mine = String(process.pid);
  const deadline = Date.now() + 240000;
  for (;;) {
    if (tryLink(lock, mine)) break;
    if (staleLock(lock, 200000)) breakStale(lock);
    else if (Date.now() > deadline) throw new Error('timed out waiting for ' + lock);
    else sleep(50);
  }
  try {
    return fn();
  } finally {
    // Only remove our own lock: after a long hang someone may have taken over.
    if (readPid(lock) === process.pid) fs.rmSync(lock, { force: true });
  }
}

function sleep(ms) {
  Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, ms);
}

function readPid(file) {
  try {
    return Number(fs.readFileSync(file, 'utf8')) || 0;
  } catch {
    return 0;
  }
}

// tryLink atomically creates file with the given content; false if it exists.
function tryLink(file, content) {
  const tmp = `${file}.${process.pid}.${Math.random().toString(36).slice(2)}`;
  fs.writeFileSync(tmp, content);
  try {
    fs.linkSync(tmp, file);
    return true;
  } catch (e) {
    if (e.code !== 'EEXIST') throw e;
    return false;
  } finally {
    fs.rmSync(tmp, { force: true });
  }
}

function breakStale(lock) {
  const mutex = lock + '.takeover';
  try {
    fs.mkdirSync(mutex);
  } catch (e) {
    if (e.code !== 'EEXIST') throw e;
    // Taking over takes milliseconds; an old mutex belongs to a crashed launcher.
    try {
      if (Date.now() - fs.statSync(mutex).mtimeMs > 30000) fs.rmdirSync(mutex);
    } catch {}
    sleep(20);
    return;
  }
  try {
    if (staleLock(lock, 200000)) fs.rmSync(lock, { force: true });
  } finally {
    fs.rmSync(mutex, { recursive: true, force: true });
  }
}

function staleLock(file, maxAge) {
  try {
    const age = Date.now() - fs.statSync(file).mtimeMs;
    const pid = readPid(file);
    if (pid > 0) {
      try {
        process.kill(pid, 0);
      } catch (e) {
        if (e.code !== 'EPERM') return true;
      }
    }
    return age > maxAge;
  } catch {
    return false; // gone already, or unreadable: the caller retries
  }
}

function launch(args) {
  const title = args.title;
  if (!title) fail('--title is required');
  const cwd = canonical(args.cwd || process.cwd());
  if (!fs.statSync(cwd, { throwIfNoEntry: false })?.isDirectory()) fail(cwd + ' is not a directory');
  const source = args['prompt-file'];
  let prompt;
  try {
    prompt = (source ? fs.readFileSync(source) : readStdin()).toString('utf8').trim();
  } catch (e) {
    fail(`cannot read the prompt file ${source}: ${e.message}`);
  }
  if (!prompt) fail(source ? `the prompt file ${source} is empty` : 'the prompt (stdin) is empty');
  withLock(() => start(args, cwd, prompt));
  // The prompt is the task's own copy now; a file made just for this launch goes.
  if (source && args['rm-prompt']) fs.rmSync(source, { force: true });
}

// A single argv entry is limited to 128 KB on Linux (MAX_ARG_STRLEN), all of
// argv to ARG_MAX on macOS and the whole command line to 32 K characters on
// Windows, and claude --help offers no way to read the
// positional prompt from a file or stdin. Larger prompts are therefore written
// to <csquad dir>/prompts/T<n>.md and the worker is told to read that file.
// The file stays as the task's record, is deleted if the launch fails, and is
// pruned by a later launch once `claude rm` has removed its task.
const MAX_INLINE_PROMPT = IS_WIN ? 16000 : 60000;
const PROMPTS = path.join(HERE, 'prompts');

function promptFile(num, prompt) {
  if (Buffer.byteLength(prompt) <= MAX_INLINE_PROMPT) return null;
  fs.mkdirSync(PROMPTS, { recursive: true });
  const file = path.join(PROMPTS, `T${num}.md`);
  fs.writeFileSync(file, prompt + '\n', { mode: 0o600 });
  return file;
}

function prunePrompts(live) {
  try {
    for (const f of fs.readdirSync(PROMPTS)) {
      const m = f.match(/^T(\d+)\.md$/);
      if (m && !live.has(Number(m[1]))) fs.rmSync(path.join(PROMPTS, f), { force: true });
      // Prompts staged by the create-task skill that a launch never consumed.
      else if (!m && /^draft-.*\.md$/.test(f) && Date.now() - fs.statSync(path.join(PROMPTS, f)).mtimeMs > 86400000) fs.rmSync(path.join(PROMPTS, f), { force: true });
    }
  } catch {}
}

// start launches the task numbered one above the highest task Claude Code
// lists; claude --bg returns once the new session is listed.
function start(args, cwd, prompt) {
  const listed = tasks();
  const num = Math.max(0, ...listed.map((t) => t.num)) + 1;
  prunePrompts(new Set(listed.map((t) => t.num)));
  const label = 'T' + num;
  const name = `${label} · ${args.title}`;
  const system = fs.readFileSync(path.join(HERE, 'worker.md'), 'utf8') + `\nYour task ID is ${label}.\n`;
  const argv = ['--bg', '--name', name];
  if (config.permissionMode) argv.push('--permission-mode', config.permissionMode);
  const model = args.model || config.model;
  if (model) argv.push('--model', model);
  const file = promptFile(num, prompt);
  if (file) prompt = `Your full task prompt is too large to pass on the command line. Read all of ${file} first (in chunks with offset and limit if needed, until its end), then carry out the task it describes.`;
  argv.push('--append-system-prompt', system, '--', prompt);
  const c = claudeCommand(argv);
  const r = spawnSync(c.file, c.args, { cwd, encoding: 'utf8', timeout: 120000, windowsHide: true, ...c.opts });
  const output = ((r.stdout || '') + (r.stderr || '')).replace(ANSI, '').trim();
  const m = output.match(/backgrounded\s+·\s+([0-9a-f]{6,})/);
  // Throw rather than fail(): process.exit would skip withLock's cleanup.
  if (r.status !== 0 || !m) {
    if (file) fs.rmSync(file, { force: true });
    throw new Error(`launching ${name} failed:\n${output || r.error}`);
  }
  // The "launched" marker lets the status line and status find, in the
  // launching session's transcript, which tasks that session started.
  console.log(`launched ${name} · session ${m[1]} · ${cwd}`);
}

// ---- transcripts ----------------------------------------------------------

function transcriptPath(sessionId) {
  const root = path.join(CLAUDE_HOME, 'projects');
  let best = null;
  let dirs = [];
  try {
    dirs = fs.readdirSync(root, { withFileTypes: true });
  } catch {}
  for (const dir of dirs) {
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

// ---- launch markers -------------------------------------------------------
//
// A launch prints `launched T12 · title · session <id> · <dir>`. The status
// line runs every few seconds in every open session, so transcripts are scanned
// incrementally: per transcript, a cache file records how far it was read and
// the ids found so far. Only markers in the tool_result of a Bash call that ran
// `tasks.js launch` count; quotes in prose, peeks and pastes do not.

const crypto = require('crypto');

const MARKER = Buffer.from('launched T');
const MARKER_LINE = /^launched T(\d+) · .*? · session ([0-9a-f]{6,})/gm;
const LAUNCH_CMD = /\btasks\.js["']?\s+launch\b/; // a quoted path closes its quote before `launch`
const TAIL = 256; // bytes before the cached offset that must still match
const LOOKBACK = [1 << 20, 8 << 20, 64 << 20]; // window sizes when looking for a tool_use

function cacheFile(file) {
  const uid = typeof process.getuid === 'function' ? process.getuid() : 'u';
  const key = crypto.createHash('sha1').update(file).digest('hex');
  return path.join(os.tmpdir(), 'csquad-scan-' + uid, key + '.json');
}

const sha = (buf) => crypto.createHash('sha1').update(buf).digest('hex');

function readAt(fd, pos, len) {
  const buf = Buffer.alloc(len);
  let got = 0;
  while (got < len) {
    const n = fs.readSync(fd, buf, got, len - got, pos + got);
    if (!n) break;
    got += n;
  }
  return buf.subarray(0, got);
}

// resultText returns the text of a tool_result block's content.
function resultText(c) {
  if (typeof c === 'string') return c;
  return Array.isArray(c) ? c.map((b) => (b && typeof b.text === 'string' ? b.text : '')).join('\n') : '';
}

// ranLaunch reports whether the tool_use `id` ran `tasks.js launch`. The
// tool_use precedes its result, possibly before the bytes read this time, so
// look in growing windows of the file before position `before`.
function ranLaunch(fd, id, before) {
  const needle = Buffer.from(`"id":${JSON.stringify(id)}`);
  for (const size of LOOKBACK) {
    const start = Math.max(0, before - size);
    const buf = readAt(fd, start, before - start);
    for (let i = buf.lastIndexOf(needle); i !== -1; i = i > 0 ? buf.lastIndexOf(needle, i - 1) : -1) {
      const a = buf.lastIndexOf(10, i) + 1;
      if (a === 0 && start > 0) break; // line cut by the window: widen it
      let b = buf.indexOf(10, i);
      if (b === -1) b = buf.length;
      try {
        const blocks = JSON.parse(buf.toString('utf8', a, b)).message.content;
        for (const blk of blocks) {
          if (blk && blk.type === 'tool_use' && blk.id === id) {
            const cmd = blk.input && blk.input.command;
            return typeof cmd === 'string' && LAUNCH_CMD.test(cmd);
          }
        }
      } catch {}
    }
    if (start === 0) break;
  }
  return false;
}

// scanLines adds the ids launched in the complete lines of buf, which starts
// at file position `base`. It finds marker hits by bytes and parses only their
// lines.
function scanLines(fd, buf, base, ids) {
  let lineEnd = -1;
  for (let i = buf.indexOf(MARKER); i !== -1; i = buf.indexOf(MARKER, i + 1)) {
    if (i < lineEnd) continue; // same line as the previous hit
    const a = buf.lastIndexOf(10, i) + 1;
    let b = buf.indexOf(10, i);
    if (b === -1) b = buf.length;
    lineEnd = b;
    let e;
    try {
      e = JSON.parse(buf.toString('utf8', a, b));
    } catch {
      continue;
    }
    const blocks = e && e.type === 'user' && e.message && e.message.content;
    if (!Array.isArray(blocks)) continue;
    for (const blk of blocks) {
      if (!blk || blk.type !== 'tool_result') continue;
      // One Bash call may launch several tasks, one marker line each.
      const found = [...resultText(blk.content).matchAll(MARKER_LINE)].map((m) => m[2]);
      if (found.length && typeof blk.tool_use_id === 'string' && ranLaunch(fd, blk.tool_use_id, base + a)) {
        for (const id of found) ids.add(id);
      }
    }
  }
}

// launchedIds returns the session ids a transcript records as launched,
// reading only what was appended since the cached offset when the cache still
// matches the file. Any cache trouble means a full scan.
function launchedIds(file) {
  const fd = fs.openSync(file, 'r');
  try {
    const st = fs.fstatSync(fd);
    const cf = cacheFile(file);
    let offset = 0;
    let ids = new Set();
    const c = readJSON(cf, null);
    if (c && c.v === 1 && c.dev === st.dev && c.ino === st.ino && Number.isInteger(c.offset) && c.offset > 0 && c.offset <= st.size && Array.isArray(c.ids)) {
      const n = Math.min(TAIL, c.offset);
      if (sha(readAt(fd, c.offset - n, n)) === c.tail) {
        offset = c.offset;
        ids = new Set(c.ids.filter((x) => typeof x === 'string'));
      }
    }
    if (offset === st.size) return ids;
    let buf = readAt(fd, offset, st.size - offset);
    // Whole lines only; an unterminated last line counts once it parses.
    let end = buf.lastIndexOf(10) + 1;
    if (end < buf.length) {
      try {
        JSON.parse(buf.toString('utf8', end));
        end = buf.length;
      } catch {}
    }
    scanLines(fd, buf.subarray(0, end), offset, ids);
    try {
      const pos = offset + end;
      const n = Math.min(TAIL, pos);
      const rec = { v: 1, dev: st.dev, ino: st.ino, offset: pos, tail: sha(readAt(fd, pos - n, n)), ids: [...ids] };
      fs.mkdirSync(path.dirname(cf), { recursive: true, mode: 0o700 });
      const tmp = `${cf}.${process.pid}.tmp`;
      fs.writeFileSync(tmp, JSON.stringify(rec), { mode: 0o600 });
      fs.renameSync(tmp, cf);
    } catch {} // an unwritable cache only costs a full scan next time
    return ids;
  } finally {
    fs.closeSync(fd);
  }
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

// launchedBy returns the session ids recorded as launched in the transcripts.
function launchedBy(transcripts) {
  const ids = new Set();
  for (const file of transcripts) {
    try {
      for (const id of launchedIds(file)) ids.add(id);
    } catch {}
  }
  return ids;
}

function ownTasks(list, ids) {
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
    list = ownTasks(list, launchedBy(historyOfSession(sid)));
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
  if (!args.all && sid && !ownTasks([t], launchedBy(historyOfSession(sid))).length) {
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

// ---- wait -----------------------------------------------------------------
//
// An opt-in monitor, run only when the user asks to be told about specific
// tasks (the watch-task skill runs it as a background shell command). It polls
// `claude agents` without any model involvement and exits once a named task
// changes into a state the user must hear about. A new state counts only when
// seen on two consecutive polls. Several waits may run at once; each is
// independent and leaves nothing behind.

const REPORT = new Set(['blocked', 'done', 'failed', 'stopped', 'removed']);
const REPORT_TEXT = { ...STATE, removed: 'removed' };

const envMs = (name, fallback) => (process.env[name] && Number(process.env[name]) >= 0 ? Number(process.env[name]) : fallback);
const delay = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

function span(min) {
  return min < 120 ? `${Math.round(min)}m` : `${Math.round(min / 60)}h`;
}

async function wait(args) {
  const interval = envMs('CSQUAD_WAIT_INTERVAL_MS', 10000);
  const grace = envMs('CSQUAD_WAIT_GRACE_MS', 5000);
  const failLimit = envMs('CSQUAD_WAIT_FAIL_MS', 120000);
  const minutes = args.timeout === undefined ? 240 : Number(args.timeout);
  if (!args._.length) fail('usage: tasks.js wait T12 [T13 ...] [--timeout MINUTES] [--all]');
  if (!(minutes > 0)) fail('--timeout expects a number of minutes');
  const nums = args._.map((l) => {
    const m = String(l).match(/^[Tt]?(\d+)$/);
    if (!m) fail('expected a task like T12, got ' + l);
    return Number(m[1]);
  });
  process.on('SIGTERM', () => process.exit(0));
  process.on('SIGINT', () => process.exit(0));

  const started = Date.now();
  const deadline = started + minutes * 60000;
  // watched: num -> { title, label, state (confirmed), seen (candidate), seenCount }
  let watched = null;
  const events = [];
  let firstFailure = 0;
  let stop = 0; // when to stop collecting events

  for (;;) {
    let list = null;
    try {
      list = tasks(30000);
      firstFailure = 0;
    } catch (e) {
      if (!firstFailure) firstFailure = Date.now();
      if (Date.now() - firstFailure >= failLimit) fail('claude agents keeps failing: ' + String(e.stderr || e.message).replace(ANSI, '').trim().split('\n')[0]);
    }
    if (list) {
      const byNum = new Map(list.map((t) => [t.num, t]));
      if (!watched) {
        const sid = process.env.CLAUDE_CODE_SESSION_ID;
        const missing = nums.filter((n) => !byNum.has(n));
        if (missing.length) fail(`T${missing[0]} is not listed by claude agents (removed or never started)`);
        if (!args.all && sid) {
          const own = ownTasks(nums.map((n) => byNum.get(n)), launchedBy(historyOfSession(sid)));
          const other = nums.find((n) => !own.some((t) => t.num === n));
          if (other) fail(`T${other} was launched by another session; pass --all to wait for it anyway`);
        }
        watched = new Map();
        for (const n of new Set(nums)) {
          const t = byNum.get(n);
          const state = t.state || 'unknown';
          const w = { label: t.label, title: t.title, state, seen: state, count: 0, done: false };
          watched.set(n, w);
          if (REPORT.has(state)) {
            w.done = true;
            events.push({ num: n, text: `${t.label} · ${t.title} · already ${REPORT_TEXT[state]}` });
          }
        }
      } else {
        for (const [n, w] of watched) {
          if (w.done) continue;
          const t = byNum.get(n);
          const state = t ? t.state || 'unknown' : 'removed';
          if (t) w.title = t.title;
          if (state === w.state) {
            w.seen = state;
            w.count = 0;
            continue;
          }
          if (state === w.seen) w.count++;
          else {
            w.seen = state;
            w.count = 1;
          }
          if (w.count < 2) continue;
          w.state = state;
          w.count = 0;
          if (REPORT.has(state)) {
            w.done = true;
            events.push({ num: n, text: `${w.label} · ${w.title} · ${REPORT_TEXT[state]}` });
          }
        }
      }
      if (events.length && !stop) stop = Date.now() + grace;
    }
    const now = Date.now();
    if (events.length && now >= stop) break;
    if (watched && [...watched.values()].every((w) => w.done)) break;
    if (now >= deadline) {
      for (const [n, w] of watched || []) if (!w.done) events.push({ num: n, text: `${w.label} · ${w.title} · still working after ${span(minutes)}` });
      break;
    }
    await delay(Math.max(0, Math.min(interval, events.length ? stop - now : deadline - now)) || 1);
  }
  events.sort((a, b) => a.num - b.num);
  console.log(events.map((e) => e.text).join('\n'));
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
    // Titles come from the model: no control characters (escape sequences) in the line.
    return { rank, num: t.num, label: ` ${t.label} `, title: t.title.replace(/[\x00-\x1f\x7f-\x9f]/g, ' '), style, text };
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
  const input = readStdin();
  let transcript, sid;
  try {
    ({ transcript_path: transcript, session_id: sid } = JSON.parse(input.toString('utf8')));
  } catch {}
  const saved = readJSON(path.join(HERE, 'statusline.json'), {});
  const command = saved.statusLine && saved.statusLine.command;
  if (typeof command === 'string' && command) {
    const r = runShell(command, input);
    const own = (r.stdout || '').replace(/\n+$/, '');
    if (own) process.stdout.write(own + '\n');
  }
  let list;
  try {
    const id = typeof sid === 'string' && sid ? sid : typeof transcript === 'string' ? path.basename(transcript, '.jsonl') : '';
    // Most sessions never launch a task: only ask claude when one did.
    const ids = typeof transcript === 'string' ? launchedBy(historyOf(transcript, id)) : new Set();
    list = ids.size ? ownTasks(tasks(4000), ids) : [];
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
    else if (a === '--rm-prompt') args['rm-prompt'] = true;
    else if (/^--?[a-z][a-z-]*$/.test(a) && i + 1 < argv.length) args[a.replace(/^-+/, '')] = argv[++i];
    else args._.push(a);
  }
  return args;
}

if (require.main === module) {
  const [command, ...rest] = process.argv.slice(2);
  const commands = { launch, status, peek, wait, statusline };
  if (!commands[command]) fail('usage: tasks.js launch|status|peek|wait|statusline');
  Promise.resolve()
    .then(() => commands[command](parse(rest)))
    .catch((e) => {
      if (command === 'statusline') process.exit(0);
      fail(e.stderr ? String(e.stderr).replace(ANSI, '').trim() : e.message);
    });
} else {
  module.exports = { findOnPath, shimTarget, cmdQuote, claudeCommand, gitBash };
}

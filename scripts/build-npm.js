#!/usr/bin/env node
// Builds the npm packages of one csquad version: one package per platform
// holding the CGO-free binary (the esbuild pattern), and the main `csquad`
// package whose optional dependencies pull in the right one and whose bin
// shim runs it.
//
//   node scripts/build-npm.js 3.0.0 [--out dist/npm] [--only linux-x64,darwin-arm64] [--scope @shunl12324]
//
// Needs Go (cross-compiles every platform). Writes OUT/csquad and
// OUT/csquad-<platform>; publish the platform packages before the main one.
'use strict';

const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const root = path.resolve(__dirname, '..');
const versionVar = 'github.com/ShunL12324/c-squad/internal/app.Version';

// npm platform → Go GOOS/GOARCH.
const targets = {
  'linux-x64': ['linux', 'amd64'],
  'linux-arm64': ['linux', 'arm64'],
  'darwin-x64': ['darwin', 'amd64'],
  'darwin-arm64': ['darwin', 'arm64'],
  'win32-x64': ['windows', 'amd64'],
  'win32-arm64': ['windows', 'arm64'],
};

const common = {
  license: 'MIT',
  author: 'ShunL12324',
  homepage: 'https://github.com/ShunL12324/c-squad#readme',
  repository: { type: 'git', url: 'git+https://github.com/ShunL12324/c-squad.git' },
  bugs: { url: 'https://github.com/ShunL12324/c-squad/issues' },
};

function usage(message) {
  if (message) console.error(`build-npm: ${message}`);
  console.error('usage: node scripts/build-npm.js VERSION [--out DIR] [--only PLATFORM,...] [--scope @SCOPE]');
  process.exit(2);
}

function parseArgs(argv) {
  const opts = { out: path.join(root, 'dist', 'npm'), only: Object.keys(targets), scope: '@shunl12324' };
  const rest = [];
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    const value = () => (i + 1 < argv.length ? argv[++i] : usage(`${a} needs a value`));
    if (a === '--out') opts.out = path.resolve(value());
    else if (a === '--only') opts.only = value().split(',').filter(Boolean);
    else if (a === '--scope') opts.scope = value();
    else if (a === '-h' || a === '--help') usage();
    else if (a.startsWith('-')) usage(`unknown flag ${a}`);
    else rest.push(a);
  }
  if (rest.length !== 1) usage();
  opts.version = rest[0].replace(/^v/, '');
  if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(opts.version)) usage(`${rest[0]} is not a version like 3.0.0`);
  for (const t of opts.only) if (!targets[t]) usage(`unknown platform ${t}; one of ${Object.keys(targets).join(', ')}`);
  if (!/^@[a-z0-9][a-z0-9._-]*$/.test(opts.scope)) usage(`${opts.scope} is not an npm scope like @name`);
  return opts;
}

function writeJSON(file, value) {
  fs.writeFileSync(file, JSON.stringify(value, null, 2) + '\n');
}

function platformPackage(opts, target) {
  const [goos, goarch] = targets[target];
  const [os, cpu] = target.split('-');
  const name = `${opts.scope}/csquad-${target}`;
  const dir = path.join(opts.out, `csquad-${target}`);
  const exe = goos === 'windows' ? 'csquad.exe' : 'csquad';
  fs.mkdirSync(path.join(dir, 'bin'), { recursive: true });
  console.log(`build-npm: ${name} (GOOS=${goos} GOARCH=${goarch})`);
  execFileSync(
    'go',
    ['build', '-trimpath', '-ldflags', `-s -w -X ${versionVar}=${opts.version}`, '-o', path.join(dir, 'bin', exe), './cmd/csquad'],
    { cwd: root, stdio: 'inherit', env: { ...process.env, CGO_ENABLED: '0', GOOS: goos, GOARCH: goarch } },
  );
  fs.chmodSync(path.join(dir, 'bin', exe), 0o755);
  writeJSON(path.join(dir, 'package.json'), {
    name,
    version: opts.version,
    description: `The csquad binary for ${os} ${cpu}; install the csquad package instead`,
    ...common,
    os: [os],
    cpu: [cpu],
    files: ['bin/'],
    preferUnplugged: true,
  });
  fs.writeFileSync(
    path.join(dir, 'README.md'),
    `# ${name}\n\nThe [csquad](https://www.npmjs.com/package/csquad) binary for ${os} ${cpu}. ` +
      'Do not install it directly: `npx csquad@latest install` picks the right one.\n',
  );
  fs.copyFileSync(path.join(root, 'LICENSE'), path.join(dir, 'LICENSE'));
  return { name, dir, exe, host: os === process.platform && cpu === process.arch };
}

function mainPackage(opts, platforms) {
  const dir = path.join(opts.out, 'csquad');
  fs.mkdirSync(path.join(dir, 'bin'), { recursive: true });
  fs.copyFileSync(path.join(root, 'npm', 'csquad', 'bin', 'csquad.js'), path.join(dir, 'bin', 'csquad.js'));
  fs.chmodSync(path.join(dir, 'bin', 'csquad.js'), 0o755);
  const optionalDependencies = {};
  // Always every platform, also with --only: the main package must not depend
  // on the build host.
  for (const t of Object.keys(targets)) optionalDependencies[`${opts.scope}/csquad-${t}`] = opts.version;
  writeJSON(path.join(dir, 'package.json'), {
    name: 'csquad',
    version: opts.version,
    description: 'Background coding tasks (T1, T2, ...) for Claude Code and Codex: dependencies, checks, merges and a status line',
    ...common,
    keywords: ['claude-code', 'codex', 'agents', 'skills', 'statusline'],
    bin: { csquad: 'bin/csquad.js' },
    files: ['bin/csquad.js'],
    engines: { node: '>=18' },
    optionalDependencies,
  });
  fs.copyFileSync(path.join(root, 'README.md'), path.join(dir, 'README.md'));
  fs.copyFileSync(path.join(root, 'LICENSE'), path.join(dir, 'LICENSE'));
  console.log(`build-npm: csquad -> ${platforms.length} of ${Object.keys(targets).length} platform packages built`);
}

function main() {
  const opts = parseArgs(process.argv.slice(2));
  // Only ever clear a directory this script writes.
  for (const d of ['csquad', ...Object.keys(targets).map((t) => `csquad-${t}`)]) {
    fs.rmSync(path.join(opts.out, d), { recursive: true, force: true });
  }
  const platforms = opts.only.map((t) => platformPackage(opts, t));
  mainPackage(opts, platforms);
  // The binary built for this machine must report the stamped version.
  const host = platforms.find((p) => p.host);
  if (host) {
    const got = execFileSync(path.join(host.dir, 'bin', host.exe), ['--version'], { encoding: 'utf8' }).trim();
    if (got !== opts.version) {
      console.error(`build-npm: ${host.name} reports ${JSON.stringify(got)}, expected ${opts.version}`);
      process.exit(1);
    }
    console.log(`build-npm: ${host.name} reports ${got}`);
  }
  console.log(`build-npm: packages in ${opts.out}`);
}

main();

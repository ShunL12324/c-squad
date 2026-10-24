#!/usr/bin/env node
// csquad's npm entry point: finds the binary of the platform package npm
// installed (one optional dependency per OS and CPU) and runs it with the
// same arguments, stdio and exit status. Nothing is downloaded.
'use strict';

const { spawn } = require('child_process');
const fs = require('fs');
const path = require('path');

const pkg = require('../package.json');
const target = `${process.platform}-${process.arch}`;
const name = Object.keys(pkg.optionalDependencies || {}).find((n) => n.endsWith(`/csquad-${target}`));
const exe = process.platform === 'win32' ? 'csquad.exe' : 'csquad';

function fail(message) {
  process.stderr.write(`csquad: ${message}\n`);
  process.exit(1);
}

function binary() {
  if (!name) {
    fail(`no prebuilt binary for ${target}; csquad ${pkg.version} ships for ${Object.keys(pkg.optionalDependencies || {}).map((n) => n.replace(/^.*\/csquad-/, '')).join(', ')}.
Build it from source instead: https://github.com/ShunL12324/c-squad#readme`);
  }
  try {
    return require.resolve(`${name}/bin/${exe}`);
  } catch {
    fail(`the package ${name} is missing, so there is no csquad binary for ${target}.
npm skips it when optional dependencies are turned off (--no-optional, --omit=optional) or when it was installed on another platform.
Reinstall with optional dependencies, for example: npx --yes csquad@${pkg.version} install`);
  }
}

function run(file, retried) {
  const child = spawn(file, process.argv.slice(2), {
    stdio: 'inherit',
    // Lets `csquad install` record that it came from npm, so `csquad update`
    // can fetch the next version the same way.
    env: { ...process.env, CSQUAD_NPM: pkg.version },
  });
  // POSIX: pass signals sent to this process on to the binary. On Windows a
  // console Ctrl+C reaches the binary directly, and kill() would end it
  // without letting it clean up, so the shim only waits.
  const signals = process.platform === 'win32' ? ['SIGINT', 'SIGBREAK'] : ['SIGINT', 'SIGTERM', 'SIGHUP', 'SIGQUIT'];
  const forward = (sig) => {
    if (process.platform !== 'win32') child.kill(sig);
  };
  for (const sig of signals) process.on(sig, forward);
  child.on('error', (err) => {
    // A tarball unpacked without the executable bit.
    if (err.code === 'EACCES' && !retried && process.platform !== 'win32') {
      try {
        fs.chmodSync(file, 0o755);
        for (const sig of signals) process.removeListener(sig, forward);
        return run(file, true);
      } catch {}
    }
    fail(`cannot run ${file}: ${err.message}`);
  });
  child.on('exit', (code, signal) => {
    for (const sig of signals) process.removeListener(sig, forward);
    if (signal) {
      // Die of the same signal, so the caller sees what the binary saw.
      process.kill(process.pid, signal);
      return;
    }
    process.exit(code === null ? 1 : code);
  });
}

run(path.resolve(binary()), false);

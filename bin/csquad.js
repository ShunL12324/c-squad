#!/usr/bin/env node
'use strict';

const installer = require('../lib/installer');

const HELP = `csquad ${installer.VERSION} - background Claude Code tasks (skills and a status line)

Usage: npx csquad@latest <command>

  install     copy the skills and tasks.js into the Claude config dir, wrap the status line
  update      same as install, for an existing installation
  uninstall   restore the status line and remove everything csquad installed
  status      show what is installed
  --version, --help

Commands also work as flags (--install). The config dir is $CLAUDE_CONFIG_DIR, or ~/.claude.`;

const major = Number(process.versions.node.split('.')[0]);
if (major < 18) {
  console.error(`csquad needs Node 18 or newer (this is ${process.version}).`);
  process.exit(1);
}

const arg = (process.argv[2] || '--help').replace(/^--?/, '');
const log = (m) => console.log(m);
const dir = installer.claudeDir();

try {
  switch (arg) {
    case 'install':
      installer.install(dir, log);
      break;
    case 'update':
      installer.install(dir, log, { update: true });
      break;
    case 'uninstall':
      installer.uninstall(dir, log);
      break;
    case 'status':
      installer.status(dir, log);
      break;
    case 'version':
    case 'v':
      console.log(installer.VERSION);
      break;
    case 'help':
    case 'h':
      console.log(HELP);
      break;
    default:
      console.error(`Unknown command: ${process.argv[2]}\n\n${HELP}`);
      process.exit(2);
  }
} catch (e) {
  console.error(`csquad: ${e.message}`);
  process.exit(1);
}

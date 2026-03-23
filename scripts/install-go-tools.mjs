#!/usr/bin/env node
import { spawnSync } from 'child_process';

function hasGo() {
  try {
    const r = spawnSync('go', ['version'], { stdio: 'ignore' });
    return r.status === 0;
  } catch (e) {
    return false;
  }
}

if (!hasGo()) {
  console.log('go not found in PATH');
  process.exit(1);
}

const cmds = [
  ['go', 'install', 'github.com/bokwoon95/wgo@latest'],
  ['go', 'install', 'github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.1.2']
];

for (const cmd of cmds) {
  const r = spawnSync(cmd[0], cmd.slice(1), { stdio: 'inherit' });
  if (r.status !== 0) {
    console.error(`warning: command failed: ${cmd.join(' ')} (exit ${r.status})`);
  }
}

process.exit(0);

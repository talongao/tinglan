import { appendFileSync } from 'node:fs';

export function targets(env) {
  const manual = env.EVENT === 'workflow_dispatch';
  if (manual && env.REF !== 'refs/heads/develop') throw new Error('Manual builds must select develop');
  const all = [
    { id: 'macos-arm64', os: 'macos-15', arch: 'arm64', platform: 'mac', directory: 'darwin-arm64', input: 'MAC_ARM64' },
    { id: 'macos-x64', os: 'macos-15-intel', arch: 'x64', platform: 'mac', directory: 'darwin-x64', input: 'MAC_X64' },
    { id: 'windows-x64', os: 'windows-2022', arch: 'x64', platform: 'win', directory: 'win32-x64', input: 'WINDOWS_X64' },
  ];
  const include = all.filter(target => !manual || env[target.input] === 'true').map(({ input, ...target }) => target);
  if (!include.length) throw new Error('Select at least one platform');
  return { include };
}
if (process.env.GITHUB_OUTPUT) appendFileSync(process.env.GITHUB_OUTPUT, `matrix=${JSON.stringify(targets(process.env))}\n`);

import { execFileSync } from 'node:child_process';
import { readFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

export const signingConfig = join(homedir(), 'Library/Application Support/huawei-music-desktop/local-signing.json');

export function macSigningIdentity() {
  if (process.platform !== 'darwin') return undefined;
  if (process.env.HWMUSIC_MAC_SIGN_IDENTITY) return process.env.HWMUSIC_MAC_SIGN_IDENTITY;
  try {
    const { fingerprint } = JSON.parse(readFileSync(signingConfig, 'utf8'));
    if (!/^[A-F0-9]{40}$/.test(fingerprint)) throw new Error('Invalid local signing fingerprint');
    return fingerprint;
  } catch (error) {
    if (error.code === 'ENOENT') return undefined;
    throw error;
  }
}

// Certificate creation is an explicit one-time setup, never a build side effect.
export function signMacOS(target) {
  if (process.platform !== 'darwin') return;
  // The linker's executable-only signature does not seal an .app bundle.
  // Local test builds still need a complete ad-hoc seal; this is not notarization.
  const identity = process.env.HWMUSIC_MAC_SIGN_IDENTITY || '-';
  execFileSync('codesign', ['--force', '--sign', identity, '--identifier', 'org.hwmusicdesktop.unofficial.internal.viva', target], { stdio: 'inherit' });
  execFileSync('codesign', ['--verify', '--deep', '--strict', target], { stdio: 'inherit' });
  return identity;
}

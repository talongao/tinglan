import { execFileSync } from 'node:child_process';
import { readdir, readFile, writeFile } from 'node:fs/promises';
import { fileURLToPath } from 'node:url';
import { join } from 'node:path';

export async function writeNotices(destination) {
  const cwd = fileURLToPath(new URL('../', import.meta.url));
  const required = JSON.parse(execFileSync(process.env.HWMUSIC_GO || 'go', ['mod','edit','-json'],{cwd,encoding:'utf8'})).Require;
  const cache = execFileSync(process.env.HWMUSIC_GO || 'go', ['env', 'GOMODCACHE'], { cwd, encoding: 'utf8' }).trim();
  const escape = value => value.replace(/[A-Z]/g, char => `!${char.toLowerCase()}`);
  // Build dependencies are already extracted. Reading their files avoids a
  // metadata lookup (and a network dependency) merely to collect licenses.
  const modules = required.map(dependency => ({ ...dependency, Dir: join(cache, `${escape(dependency.Path)}@${escape(dependency.Version)}`) }));
  const sections = ['Third-party notices for the native build. Source module versions are pinned in native/go.mod and go.sum.'];
  for (const module of modules) {
    if (module.Main || !module.Dir) continue;
    const files = (await readdir(module.Dir)).filter(name => /^(LICENSE|UNLICENSE|COPYING|NOTICE)(\..*)?$/i.test(name));
    if (!files.length) throw new Error(`No license file found for ${module.Path}`);
    for (const name of files) {
      sections.push(`${module.Path} ${module.Version}\n${name}\n${await readFile(join(module.Dir, name), 'utf8')}`);
    }
    if (module.Path === 'github.com/wailsapp/go-webview2') {
      sections.push(`${module.Path}/webviewloader\n${await readFile(join(module.Dir, 'webviewloader/LICENSE'), 'utf8')}`);
    }
  }
  sections.push(await readFile(fileURLToPath(new URL('../../build/icon.LICENSE.txt', import.meta.url)), 'utf8'));
  await writeFile(destination, sections.join('\n\n----------------------------------------\n\n'));
}

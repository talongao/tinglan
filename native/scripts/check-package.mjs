import { readFile, readdir, lstat } from 'node:fs/promises';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import assert from 'node:assert/strict';
const root=fileURLToPath(new URL('../../',import.meta.url));
const dir=join(root,'out/native/tinglan',`${process.platform}-${process.arch}`);
const binary=process.platform==='win32'?'tinglan.exe':'tinglan';
const expected=[binary,'LICENSE','THIRD_PARTY_NOTICES.txt','icon.LICENSE.txt','NOTICE.txt','SHA256SUMS'].sort();
assert.deepEqual((await readdir(dir)).sort(),expected,'Unexpected payload file');
for(const name of expected){assert.ok((await lstat(join(dir,name))).isFile(),'No links or directories in payload');}
const bytes=await readFile(join(dir,binary));
const hash=createHash('sha256').update(bytes).digest('hex');
assert.equal(await readFile(join(dir,'SHA256SUMS'),'utf8'),`${hash}  ${binary}\n`);
assert.ok(bytes.length>1000000,'Missing native binary');
if (process.platform === 'win32') {
  const pe = bytes.readUInt32LE(0x3c);
  assert.equal(bytes.toString('ascii', pe, pe + 4), 'PE\0\0', 'Missing PE header');
  const optional = pe + 24;
  assert.equal(bytes.readUInt16LE(optional + 68), 2, 'App must use GUI subsystem, not console');
  const sections = bytes.readUInt16LE(pe + 6);
  const start = optional + bytes.readUInt16LE(pe + 20);
  let resources = false;
  for (let i = 0; i < sections; i++) {
    const section = start + i * 40;
    if (bytes.toString('ascii', section, section + 8).replaceAll('\0', '') === '.rsrc') resources = bytes.readUInt32LE(section + 16) > 0;
  }
  assert.ok(resources, 'Missing embedded Windows icon resources');
}
const licenses=await readFile(join(dir,'THIRD_PARTY_NOTICES.txt'),'utf8');
for(const name of ['vivaui/viva','godbus/dbus','gorilla/websocket','media-winrt-go','go-ole'])assert.ok(licenses.includes(name),`Missing license ${name}`);
assert.ok((await readFile(join(dir,'NOTICE.txt'),'utf8')).includes('OAuth'));
console.log('Native payload allowlist, licenses and SHA256 verified');

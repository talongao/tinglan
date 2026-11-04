#!/usr/bin/env node
// Minimal Tinglan source plugin (Node). Speaks Content-Length framed JSON-RPC 2.0.
import { createInterface } from "node:readline";

let buffer = Buffer.alloc(0);
process.stdin.on("data", (chunk) => {
  buffer = Buffer.concat([buffer, chunk]);
  for (;;) {
    const head = buffer.indexOf("\r\n\r\n");
    if (head < 0) return;
    const header = buffer.subarray(0, head).toString("utf8");
    const match = /content-length:\s*(\d+)/i.exec(header);
    if (!match) { buffer = buffer.subarray(head + 4); continue; }
    const length = Number(match[1]);
    const start = head + 4;
    if (buffer.length < start + length) return;
    const body = buffer.subarray(start, start + length).toString("utf8");
    buffer = buffer.subarray(start + length);
    try { handle(JSON.parse(body)); } catch (e) { process.stderr.write(`bad frame: ${e}\n`); }
  }
});

function send(payload) {
  const body = JSON.stringify(payload);
  process.stdout.write(`Content-Length: ${Buffer.byteLength(body)}\r\n\r\n${body}`);
}
function reply(id, result) { send({ jsonrpc: "2.0", id, result }); }
function fail(id, code, message) { send({ jsonrpc: "2.0", id, error: { code, message } }); }

const descriptor = {
  schemaVersion: 1,
  id: "node-demo",
  name: "Node 示例源",
  shortName: "N",
  accent: "#3C873A",
  capabilities: { search: true, playlist: false, import: false, resolve: false, lyrics: false },
  resourceDomains: [],
};

const tracks = [
  { id: "n1", title: "脚本写的歌", artists: [{ id: "a1", name: "Node 歌手" }], album: { id: "al1", title: "脚本专辑" } },
  { id: "n2", title: "第二个结果", artists: [{ id: "a1", name: "Node 歌手" }] },
];

function handle(msg) {
  if (msg.method === undefined) return; // response from host, ignore
  const id = msg.id;
  switch (msg.method) {
    case "initialize": {
      const assigned = msg.params?.assignedId;
      const desc = { ...descriptor, id: assigned || descriptor.id };
      reply(id, { protocolVersion: 1, descriptor: desc, requiredHostServices: [] });
      return;
    }
    case "initialized":
      return;
    case "shutdown":
      reply(id, {});
      setTimeout(() => process.exit(0), 10);
      return;
    case "source.describe":
      reply(id, descriptor);
      return;
    case "source.auth.status":
      reply(id, { state: "signedOut" });
      return;
    case "source.auth.verify":
      reply(id, { state: "signedOut" });
      return;
    case "source.catalog.search": {
      const q = String(msg.params?.query ?? "");
      const items = tracks
        .filter((t) => q === "" || t.title.includes(q) || t.artists[0].name.includes(q))
        .map((t) => ({ ...t, source: descriptor.id }));
      reply(id, { items, hasMore: false });
      return;
    }
    case "source.catalog.history":
      reply(id, { items: [], hasMore: false });
      return;
    case "source.catalog.playlists":
      reply(id, { items: [], hasMore: false });
      return;
    default:
      fail(id, -32004, `unsupported: ${msg.method}`);
  }
}

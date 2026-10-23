import test from "node:test";
import assert from "node:assert/strict";
import { runSeed } from "./seed-demo-sermons.mjs";

const sample = [{
  title: "[DEMO] A test message",
  summary: "Fictional preview content, not a church recording or announcement.",
  speaker: "City4Christ Demo Library",
  series: "Preview Library",
  topic: "Faith",
  preachedAt: "2026-10-18T10:00:00Z",
  imageUrl: "https://images.unsplash.com/photo-1770097286875-0cbf4ca2f8c1?auto=format&fit=crop&w=1200&q=80",
  videoUrl: "",
}];

test("dry run describes demo content without making requests", async () => {
  let requests = 0;
  const output = [];
  const result = await runSeed({
    records: sample,
    apiURL: "https://city4christ-staging-api.onrender.com",
    appOrigin: "https://city4christ-pwa.vercel.app",
    apply: false,
    fetchImpl: async () => { requests += 1; },
    log: (line) => output.push(line),
  });
  assert.equal(requests, 0);
  assert.equal(result.created, 0);
  assert.match(output.join("\n"), /1 demo sermon/);
});

test("apply creates then publishes through the audited editor API", async () => {
  const calls = [];
  const fetchImpl = async (url, options) => {
    calls.push({ url: String(url), ...options, body: options.body ? JSON.parse(options.body) : null });
    if (calls.at(-1).method === "GET") return { ok: true, status: 200, json: async () => ({ data: [] }) };
    if (calls.at(-1).method === "POST") return { ok: true, status: 201, json: async () => ({ data: { id: "11111111-1111-4111-8111-111111111111", version: 1 } }) };
    return { ok: true, status: 200, json: async () => ({ data: { status: "published", version: 2 } }) };
  };
  const result = await runSeed({
    records: sample,
    apiURL: "https://city4christ-staging-api.onrender.com",
    appOrigin: "https://city4christ-pwa.vercel.app",
    accessToken: "test-access-token",
    apply: true,
    fetchImpl,
    log: () => {},
  });
  assert.equal(result.created, 1);
  assert.deepEqual(calls.map((call) => call.method), ["GET", "POST", "PATCH"]);
  assert.equal(calls[1].body.imageUrl, sample[0].imageUrl);
  assert.deepEqual(calls[2].body, { version: 1, status: "published" });
  assert.equal(calls[1].headers.Authorization, "Bearer test-access-token");
  assert.equal(calls[1].headers.Origin, "https://city4christ-pwa.vercel.app");
});

test("apply refuses production hosts and missing editor credentials", async () => {
  const base = {
    records: sample,
    appOrigin: "https://city4christ-pwa.vercel.app",
    apply: true,
    fetchImpl: async () => { throw new Error("must not send a request"); },
    log: () => {},
  };
  await assert.rejects(() => runSeed({ ...base, apiURL: "https://api.city4christ.org", accessToken: "secret" }), /staging or a local API/);
  await assert.rejects(() => runSeed({ ...base, apiURL: "https://city4christ-staging-api.onrender.com" }), /ACCESS_TOKEN/);
});

test("already published exact-title demos are skipped on rerun", async () => {
  let writes = 0;
  const fetchImpl = async (url) => String(url).includes("/sermons?")
    ? { ok: true, status: 200, json: async () => ({ data: sample }) }
    : (writes += 1, { ok: true, status: 200, json: async () => ({ data: {} }) });
  const result = await runSeed({
    records: sample,
    apiURL: "https://city4christ-staging-api.onrender.com",
    appOrigin: "https://city4christ-pwa.vercel.app",
    accessToken: "test-access-token",
    apply: true,
    fetchImpl,
    log: () => {},
  });
  assert.deepEqual(result, { created: 0, skipped: 1 });
  assert.equal(writes, 0);
});

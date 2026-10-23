import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { resolve } from "node:path";

const allowedStagingHost = "city4christ-staging-api.onrender.com";

function parseBase(value, label, { allowLocal = false } = {}) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`${label} must be an absolute URL.`);
  }
  const local = allowLocal && ["localhost", "127.0.0.1", "[::1]"].includes(url.hostname);
  if (
    !["https:", ...(local ? ["http:"] : [])].includes(url.protocol) ||
    url.username || url.password || (url.port && !local) || url.pathname !== "/" ||
    url.search || url.hash || (label === "API_URL" && !local && url.hostname !== allowedStagingHost)
  ) {
    throw new Error(`${label} must be the City4Christ staging or a local API origin.`);
  }
  return url.origin;
}

async function request(fetchImpl, api, origin, token, path, method = "GET", body) {
  const response = await fetchImpl(new URL(path, `${api}/`), {
    method,
    headers: {
      Accept: "application/json",
      ...(body ? { "Content-Type": "application/json" } : {}),
      Origin: origin,
      Authorization: `Bearer ${token}`,
    },
    ...(body ? { body: JSON.stringify(body) } : {}),
  });
  if (!response.ok) throw new Error(`${method} ${path} failed with HTTP ${response.status}.`);
  return response.json();
}

export async function runSeed({
  records,
  apiURL,
  appOrigin,
  accessToken = "",
  apply = false,
  fetchImpl = fetch,
  log = console.log,
}) {
  const api = parseBase(apiURL, "API_URL", { allowLocal: true });
  const origin = parseBase(appOrigin, "APP_ORIGIN", { allowLocal: true });
  if (!Array.isArray(records) || records.length === 0) throw new Error("The demo sermon list is empty.");
  if (!apply) {
    log(`Dry run: ${records.length} demo sermons are ready for ${api}. No requests were sent.`);
    for (const record of records) log(`- ${record.title}${record.videoUrl ? " (video)" : " (image)"}`);
    return { created: 0, skipped: 0 };
  }
  if (!accessToken.trim()) throw new Error("CITY4CHRIST_ACCESS_TOKEN is required with --apply.");

  let created = 0;
  let skipped = 0;
  for (const record of records) {
    const query = new URLSearchParams({ query: record.title, limit: "100" });
    const existing = await request(fetchImpl, api, origin, accessToken, `/api/v1/sermons?${query}`);
    if ((existing.data || []).some((item) => item.title === record.title)) {
      skipped += 1;
      log(`Skipped existing demo sermon: ${record.title}`);
      continue;
    }
    const draft = await request(fetchImpl, api, origin, accessToken, "/api/v1/admin/sermons", "POST", record);
    const id = draft?.data?.id;
    if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/.test(id || "")) {
      throw new Error("The API did not return a valid sermon ID; stopping without further writes.");
    }
    const published = await request(fetchImpl, api, origin, accessToken, `/api/v1/admin/sermons/${id}`, "PATCH", { version: 1, status: "published" });
    if (published?.data?.status !== "published") throw new Error(`The demo sermon ${id} was created but the API did not confirm publication.`);
    created += 1;
    log(`Published demo sermon: ${record.title}`);
  }
  return { created, skipped };
}

async function main() {
  const records = JSON.parse(await readFile(new URL("./demo-sermons.json", import.meta.url), "utf8"));
  const apply = process.argv.includes("--apply");
  const result = await runSeed({
    records,
    apiURL: process.env.CITY4CHRIST_API_URL,
    appOrigin: process.env.CITY4CHRIST_APP_ORIGIN,
    accessToken: process.env.CITY4CHRIST_ACCESS_TOKEN,
    apply,
  });
  if (apply) console.log(`Done. Added ${result.created}; skipped ${result.skipped}. Existing records were not changed or removed.`);
}

if (process.argv[1] && fileURLToPath(import.meta.url) === resolve(process.argv[1])) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}

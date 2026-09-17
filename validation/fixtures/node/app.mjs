// Validation fixture, not a production application.
import http from 'node:http';
import { timingSafeEqual } from 'node:crypto';
import { readFileSync } from 'node:fs';
import pg from 'pg';

const schema = 'CREATE TABLE IF NOT EXISTS validation_records (id bigint GENERATED ALWAYS AS IDENTITY UNIQUE, key text PRIMARY KEY, value text NOT NULL)';
const token = process.env.VALIDATION_TOKEN || '';
const mode = process.env.DATABASE_SSLMODE || 'verify-full';
const port = Number(process.env.PORT || 8080);
const version = process.env.APP_VERSION || 'v1';
let database;
try {
  const dsn = new URL(process.env.DATABASE_URL);
  if (!['postgres:', 'postgresql:'].includes(dsn.protocol) || !dsn.hostname ||
      Buffer.byteLength(token) < 32 || !['disable', 'verify-full'].includes(mode) ||
      !Number.isInteger(port) || port < 1 || port > 65535) throw new Error();
  for (const key of ['ssl', 'sslmode', 'sslrootcert', 'sslcert', 'sslkey', 'uselibpqcompat']) dsn.searchParams.delete(key);
  const ssl = mode === 'disable' ? false : {rejectUnauthorized: true};
  if (ssl && process.env.DATABASE_CA_CERT) ssl.ca = readFileSync(process.env.DATABASE_CA_CERT);
  database = {connectionString: dsn.toString(), ssl, connectionTimeoutMillis: 3000, statement_timeout: 5000, query_timeout: 6000};
} catch {
  console.error('{"error":"invalid_configuration"}'); process.exit(1);
}

// ponytail: one connection per request for this low-rate fixture; add a pool for production throughput.
async function withDatabase(fn) {
  const client = new pg.Client(database);
  client.on('error', () => {}); // Errors are returned by the request; never print connection details.
  try { await client.connect(); return await fn(client); }
  finally { await client.end().catch(() => {}); }
}
try { await withDatabase(db => db.query(schema)); }
catch { console.error('{"error":"database_unavailable"}'); process.exit(1); }

const server = http.createServer(async (req, res) => {
  function respond(status, body) {
    res.writeHead(status, {'Content-Type': 'application/json; charset=utf-8', 'Cache-Control': 'no-store'});
    res.end(JSON.stringify(body));
  }
  const path = (req.url || '/').split('?')[0];
  if (path === '/healthz' && req.method === 'GET') return respond(200, {status: 'ok', language: 'node', version});
  const ready = path === '/readyz' && req.method === 'GET';
  let key;
  if (!ready) {
    const actual = Buffer.from(req.headers.authorization || '');
    const expected = Buffer.from(`Bearer ${token}`);
    if (actual.length !== expected.length || !timingSafeEqual(actual, expected)) return respond(401, {error: 'unauthorized'});
    if (!path.startsWith('/records/')) return respond(404, {error: 'not_found'});
    key = path.slice('/records/'.length);
    if (!/^[A-Za-z0-9_-]{1,80}$/.test(key)) return respond(400, {error: 'invalid_key'});
    if (!['GET', 'PUT', 'DELETE'].includes(req.method)) return respond(405, {error: 'method_not_allowed'});
  }
  let value;
  if (req.method === 'PUT') {
    let size = 0;
    const chunks = [];
    try {
      for await (const chunk of req) {
        size += chunk.length;
        if (size > 4096) { req.resume(); return respond(413, {error: 'value_too_large'}); }
        chunks.push(chunk);
      }
      value = new TextDecoder('utf-8', {fatal: true}).decode(Buffer.concat(chunks));
      if (!value || value.includes('\0')) throw new Error();
    } catch { return respond(400, {error: 'invalid_value'}); }
  }
  try {
    const result = await withDatabase(async db => {
      if (ready) { await db.query('SELECT 1'); return {status: 'ready'}; }
      if (req.method === 'PUT') await db.query('INSERT INTO validation_records(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value', [key, value]);
      if (req.method === 'GET') {
        const result = await db.query('SELECT value FROM validation_records WHERE key=$1', [key]);
        if (!result.rows.length) return null;
        value = result.rows[0].value;
      }
      if (req.method === 'DELETE') { await db.query('DELETE FROM validation_records WHERE key=$1', [key]); return {deleted: true}; }
      return {key, value};
    });
    return result === null ? respond(404, {error: 'not_found'}) : respond(200, result);
  } catch { return respond(503, {error: 'database_unavailable'}); }
});
server.requestTimeout = 10000;
server.headersTimeout = 5000;
server.on('error', () => { console.error('{"error":"http_server_stopped"}'); process.exit(1); });
server.listen(port, '0.0.0.0', () => console.log(JSON.stringify({event: 'listening', language: 'node', port})));

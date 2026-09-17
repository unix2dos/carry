// Validation fixture, not a production application.
use native_tls::{Certificate, TlsConnector};
use postgres::{config::SslMode, Client, Config};
use postgres_native_tls::MakeTlsConnector;
use serde_json::{json, Value};
use std::{env, fs, io::Read, time::Duration};
use subtle::ConstantTimeEq;
use tiny_http::{Header, Request, Response, Server, StatusCode};
use url::Url;

const SCHEMA: &str =
    "CREATE TABLE IF NOT EXISTS validation_records (id bigint GENERATED ALWAYS AS IDENTITY UNIQUE, key text PRIMARY KEY, value text NOT NULL)";

struct App {
    db: Config,
    tls: TlsConnector,
    token: String,
    version: String,
}

impl App {
    // ponytail: one connection per request and a sequential server; use a pool/server framework for production load.
    fn connect(&self) -> Result<Client, postgres::Error> {
        self.db.connect(MakeTlsConnector::new(self.tls.clone()))
    }

    fn handle(&self, request: &mut Request) -> (u16, Value) {
        let path = request.url().split('?').next().unwrap_or("/").to_owned();
        let method = request.method().as_str().to_owned();
        if path == "/healthz" && method == "GET" {
            return (
                200,
                json!({"status":"ok", "language":"rust", "version":self.version}),
            );
        }
        let ready = path == "/readyz" && method == "GET";
        let key = path.strip_prefix("/records/").unwrap_or("");
        if !ready {
            let auth = request
                .headers()
                .iter()
                .find(|h| h.field.equiv("Authorization"))
                .map(|h| h.value.as_str())
                .unwrap_or("");
            if !bool::from(
                auth.as_bytes()
                    .ct_eq(format!("Bearer {}", self.token).as_bytes()),
            ) {
                return (401, json!({"error":"unauthorized"}));
            }
            if !path.starts_with("/records/") {
                return (404, json!({"error":"not_found"}));
            }
            if key.is_empty()
                || key.len() > 80
                || !key
                    .bytes()
                    .all(|c| c.is_ascii_alphanumeric() || c == b'-' || c == b'_')
            {
                return (400, json!({"error":"invalid_key"}));
            }
            if !["GET", "PUT", "DELETE"].contains(&method.as_str()) {
                return (405, json!({"error":"method_not_allowed"}));
            }
        }
        let mut value = String::new();
        if method == "PUT" {
            let mut body = Vec::new();
            if request
                .as_reader()
                .take(4097)
                .read_to_end(&mut body)
                .is_err()
            {
                return (400, json!({"error":"invalid_value"}));
            }
            if body.len() > 4096 {
                return (413, json!({"error":"value_too_large"}));
            }
            value = match String::from_utf8(body) {
                Ok(v) => v,
                Err(_) => return (400, json!({"error":"invalid_value"})),
            };
            if value.is_empty() || value.contains('\0') {
                return (400, json!({"error":"invalid_value"}));
            }
        }
        let result = (|| -> Result<Option<Value>, postgres::Error> {
            let mut db = self.connect()?;
            if ready {
                db.simple_query("SELECT 1")?;
                return Ok(Some(json!({"status":"ready"})));
            }
            match method.as_str() {
                "PUT" => {
                    db.execute("INSERT INTO validation_records(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value", &[&key, &value])?;
                }
                "GET" => {
                    match db
                        .query_opt("SELECT value FROM validation_records WHERE key=$1", &[&key])?
                    {
                        Some(row) => value = row.get(0),
                        None => return Ok(None),
                    }
                }
                "DELETE" => {
                    db.execute("DELETE FROM validation_records WHERE key=$1", &[&key])?;
                    return Ok(Some(json!({"deleted":true})));
                }
                _ => unreachable!(),
            }
            Ok(Some(json!({"key":key, "value":value})))
        })();
        match result {
            Ok(Some(body)) => (200, body),
            Ok(None) => (404, json!({"error":"not_found"})),
            Err(_) => (503, json!({"error":"database_unavailable"})),
        }
    }
}

fn run() -> Result<(), &'static str> {
    let token = env::var("VALIDATION_TOKEN").map_err(|_| "invalid_configuration")?;
    let port: u16 = env::var("PORT")
        .unwrap_or_else(|_| "8080".into())
        .parse()
        .map_err(|_| "invalid_configuration")?;
    if token.len() < 32 || port == 0 {
        return Err("invalid_configuration");
    }
    let mode = env::var("DATABASE_SSLMODE").unwrap_or_else(|_| "verify-full".into());
    if mode != "disable" && mode != "verify-full" {
        return Err("invalid_configuration");
    }
    let mut dsn = Url::parse(&env::var("DATABASE_URL").map_err(|_| "invalid_configuration")?)
        .map_err(|_| "invalid_configuration")?;
    if !["postgres", "postgresql"].contains(&dsn.scheme()) || dsn.host_str().is_none() {
        return Err("invalid_configuration");
    }
    let pairs: Vec<(String, String)> = dsn
        .query_pairs()
        .filter(|(k, _)| {
            ![
                "ssl",
                "sslmode",
                "sslrootcert",
                "sslcert",
                "sslkey",
                "uselibpqcompat",
            ]
            .contains(&k.as_ref())
        })
        .map(|(k, v)| (k.into_owned(), v.into_owned()))
        .collect();
    dsn.set_query(None);
    if !pairs.is_empty() {
        dsn.query_pairs_mut().extend_pairs(pairs);
    }
    let mut db: Config = dsn.as_str().parse().map_err(|_| "invalid_configuration")?;
    // Rust's Require mandates TLS; native_tls additionally verifies chain and hostname by default.
    db.ssl_mode(if mode == "disable" {
        SslMode::Disable
    } else {
        SslMode::Require
    });
    db.connect_timeout(Duration::from_secs(3));
    db.options("-c statement_timeout=5000");
    let mut tls = TlsConnector::builder();
    if let Ok(path) = env::var("DATABASE_CA_CERT") {
        let pem = fs::read(path).map_err(|_| "invalid_configuration")?;
        tls.add_root_certificate(Certificate::from_pem(&pem).map_err(|_| "invalid_configuration")?);
    }
    let app = App {
        db,
        tls: tls.build().map_err(|_| "invalid_configuration")?,
        token,
        version: env::var("APP_VERSION").unwrap_or_else(|_| "v1".into()),
    };
    app.connect()
        .and_then(|mut db| db.batch_execute(SCHEMA))
        .map_err(|_| "database_unavailable")?;
    let server = Server::http(format!("0.0.0.0:{port}")).map_err(|_| "http_server_stopped")?;
    println!(
        "{}",
        json!({"event":"listening", "language":"rust", "port":port})
    );
    for mut request in server.incoming_requests() {
        let (status, body) = app.handle(&mut request);
        let response = Response::from_string(body.to_string())
            .with_status_code(StatusCode(status))
            .with_header(
                Header::from_bytes("Content-Type", "application/json; charset=utf-8").unwrap(),
            )
            .with_header(Header::from_bytes("Cache-Control", "no-store").unwrap());
        let _ = request.respond(response);
    }
    Ok(())
}

fn main() {
    if let Err(code) = run() {
        eprintln!("{}", json!({"error":code}));
        std::process::exit(1);
    }
}

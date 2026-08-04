package bench

import (
	"os"
	"sort"
)

// This file is the data, written out three times on purpose: the expected Go
// value, the environment that must produce it, and the YAML document that must
// produce it. Deriving any one of the three from another would let the harness
// agree with itself, which is the failure mode an equivalence check exists to
// catch. Three independent spellings that have to agree is the check having
// something to check.

// WantSmall is the value every library must produce for the small scenario.
func WantSmall() Small {
	return Small{
		Name:   "checkout",
		Port:   8080,
		Debug:  true,
		Rate:   0.25,
		Region: "eu-west-1",
	}
}

// WantLarge is the value every library must produce for the large scenario.
//
//nolint:funlen // A literal is data, and splitting it would only hide the shape.
func WantLarge() Large {
	return Large{
		Name:     "checkout",
		Env:      "production",
		Version:  "1.24.3",
		Debug:    false,
		Replicas: 6,
		Server: Server{
			Host: "0.0.0.0",
			Port: 8080,
			HTTP: HTTP{
				Port:     8081,
				Read:     15,
				Write:    30,
				Idle:     120,
				Header:   4096,
				Compress: true,
			},
			TLS: TLS{
				Enabled: true,
				Cert:    "/etc/tls/server.crt",
				Key:     "/etc/tls/server.key",
				Min:     "1.3",
				Verify:  false,
			},
		},
		DB: DB{
			Host: "db.internal",
			Port: 5432,
			User: "checkout",
			Pass: "s3cret",
			Name: "checkout_prod",
			Pool: Pool{
				Max:      64,
				Idle:     8,
				Lifetime: 3600,
				Timeout:  5,
			},
			Retry: Retry{
				Attempts: 4,
				Backoff:  250,
				Jitter:   0.15,
			},
		},
		Cache: Cache{
			Host:  "cache.internal",
			Port:  6379,
			TTL:   300,
			Size:  1024,
			Evict: "lru",
		},
		Log: Log{
			Level:    "info",
			Format:   "json",
			Output:   "stdout",
			Sampling: true,
			Caller:   false,
		},
		Metrics: Metrics{
			Enabled:  true,
			Host:     "0.0.0.0",
			Port:     9090,
			Path:     "/metrics",
			Interval: 15,
		},
		Feature: Feature{
			Alpha: true,
			Beta:  false,
			Gamma: true,
			Ratio: 0.05,
		},
		Tags:   []string{"checkout", "payments", "eu"},
		Limits: map[string]string{"rps": "1000", "burst": "2000", "conn": "256"},
	}
}

// EnvSmall is the environment the small scenario runs against.
func EnvSmall() map[string]string {
	return map[string]string{
		"NAME":   "checkout",
		"PORT":   "8080",
		"DEBUG":  "true",
		"RATE":   "0.25",
		"REGION": "eu-west-1",
	}
}

// EnvLarge is the environment the large scenario runs against.
//
// The slice and the map appear twice, in the two spellings the field of
// libraries splits into, and that duplication is the honest way to run this
// comparison rather than a thumb on the scale:
//
//	TAGS_0, TAGS_1, TAGS_2       indexed - ferry, koanf
//	CSV_TAGS                     delimited - xload, go-envconfig,
//	                             kelseyhightower, viper
//	LIMITS_RPS, LIMITS_BURST,    one variable per key - ferry, koanf, viper
//	LIMITS_CONN
//	KV_LIMITS                    delimited pairs - xload, go-envconfig,
//	                             kelseyhightower
//
// The delimited spelling is not called TAGS, and that is not cosmetic, but the
// reason moved. It used to be ferry's: a plain string at the container address
// /tags was a load ferry refused outright. Core now asks a source that can list
// for a dynamic container's children before it asks the container's own
// address, so driver/env's TAGS_0.. win and the string at TAGS is never read.
// TestFerryReadsTheChildrenAndNotTheContainer pins that.
//
// What survives is the flat plane. TAGS and TAGS_0.. are one field spelled
// twice in one key space, and a library that scans the whole environ has to
// resolve the collision itself: koanf's transform already drops one of the two
// spellings, and the same clash at LIMITS would put a string and a mapping on
// the same koanf key. A name of its own keeps every library reading a variable
// it defines.
//
// Forcing one spelling on everybody would measure which library happened to
// agree with the harness author. Each library reads the spelling it defines,
// and the difference is stated in its notes rather than smoothed over. The
// cost of the four extra variables falls only on the two libraries that scan
// the whole environ, koanf and viper, and it is four out of sixty.
//
//nolint:funlen // A literal is data, and splitting it would only hide the shape.
func EnvLarge() map[string]string {
	return map[string]string{
		"NAME":     "checkout",
		"ENV":      "production",
		"VERSION":  "1.24.3",
		"DEBUG":    "false",
		"REPLICAS": "6",

		"SERVER_HOST":          "0.0.0.0",
		"SERVER_PORT":          "8080",
		"SERVER_HTTP_PORT":     "8081",
		"SERVER_HTTP_READ":     "15",
		"SERVER_HTTP_WRITE":    "30",
		"SERVER_HTTP_IDLE":     "120",
		"SERVER_HTTP_HEADER":   "4096",
		"SERVER_HTTP_COMPRESS": "true",
		"SERVER_TLS_ENABLED":   "true",
		"SERVER_TLS_CERT":      "/etc/tls/server.crt",
		"SERVER_TLS_KEY":       "/etc/tls/server.key",
		"SERVER_TLS_MIN":       "1.3",
		"SERVER_TLS_VERIFY":    "false",

		"DB_HOST":           "db.internal",
		"DB_PORT":           "5432",
		"DB_USER":           "checkout",
		"DB_PASS":           "s3cret",
		"DB_NAME":           "checkout_prod",
		"DB_POOL_MAX":       "64",
		"DB_POOL_IDLE":      "8",
		"DB_POOL_LIFETIME":  "3600",
		"DB_POOL_TIMEOUT":   "5",
		"DB_RETRY_ATTEMPTS": "4",
		"DB_RETRY_BACKOFF":  "250",
		"DB_RETRY_JITTER":   "0.15",

		"CACHE_HOST":  "cache.internal",
		"CACHE_PORT":  "6379",
		"CACHE_TTL":   "300",
		"CACHE_SIZE":  "1024",
		"CACHE_EVICT": "lru",

		"LOG_LEVEL":    "info",
		"LOG_FORMAT":   "json",
		"LOG_OUTPUT":   "stdout",
		"LOG_SAMPLING": "true",
		"LOG_CALLER":   "false",

		"METRICS_ENABLED":  "true",
		"METRICS_HOST":     "0.0.0.0",
		"METRICS_PORT":     "9090",
		"METRICS_PATH":     "/metrics",
		"METRICS_INTERVAL": "15",

		"FEATURE_ALPHA": "true",
		"FEATURE_BETA":  "false",
		"FEATURE_GAMMA": "true",
		"FEATURE_RATIO": "0.05",

		"TAGS_0":   "checkout",
		"TAGS_1":   "payments",
		"TAGS_2":   "eu",
		"CSV_TAGS": "checkout,payments,eu",

		"LIMITS_RPS":   "1000",
		"LIMITS_BURST": "2000",
		"LIMITS_CONN":  "256",
		"KV_LIMITS":    "rps:1000,burst:2000,conn:256",
	}
}

// YAMLSmall is the document the small YAML scenario runs against.
const YAMLSmall = `name: checkout
port: 8080
debug: true
rate: 0.25
region: eu-west-1
`

// YAMLLarge is the document the large YAML scenario runs against.
const YAMLLarge = `name: checkout
env: production
version: "1.24.3"
debug: false
replicas: 6
server:
  host: 0.0.0.0
  port: 8080
  http:
    port: 8081
    read: 15
    write: 30
    idle: 120
    header: 4096
    compress: true
  tls:
    enabled: true
    cert: /etc/tls/server.crt
    key: /etc/tls/server.key
    min: "1.3"
    verify: false
db:
  host: db.internal
  port: 5432
  user: checkout
  pass: s3cret
  name: checkout_prod
  pool:
    max: 64
    idle: 8
    lifetime: 3600
    timeout: 5
  retry:
    attempts: 4
    backoff: 250
    jitter: 0.15
cache:
  host: cache.internal
  port: 6379
  ttl: 300
  size: 1024
  evict: lru
log:
  level: info
  format: json
  output: stdout
  sampling: true
  caller: false
metrics:
  enabled: true
  host: 0.0.0.0
  port: 9090
  path: /metrics
  interval: 15
feature:
  alpha: true
  beta: false
  gamma: true
  ratio: 0.05
tags:
  - checkout
  - payments
  - eu
limits:
  rps: "1000"
  burst: "2000"
  conn: "256"
`

// LargeKeys is the dotted key of every leaf address of [Large], in the
// spelling viper wants, and it is a hand-written list rather than a reflective
// walk on purpose.
//
// Viper resolves an environment variable only for a key it has already been
// told about, so a viper user writes this list out. Deriving it here with
// reflection would charge viper for work its users do once at authoring time
// and never at run time, which would be the harness inventing a cost rather
// than measuring one.
//
// The map's three keys are registered individually, because that is what makes
// viper build a nested map that decodes into a map[string]string. The slice is
// one key, because viper's decode hook splits a delimited string.
//
//nolint:funlen // A literal is data, and splitting it would only hide the shape.
func LargeKeys() []string {
	return []string{
		"name", "env", "version", "debug", "replicas",

		"server.host", "server.port",
		"server.http.port", "server.http.read", "server.http.write",
		"server.http.idle", "server.http.header", "server.http.compress",
		"server.tls.enabled", "server.tls.cert", "server.tls.key",
		"server.tls.min", "server.tls.verify",

		"db.host", "db.port", "db.user", "db.pass", "db.name",
		"db.pool.max", "db.pool.idle", "db.pool.lifetime", "db.pool.timeout",
		"db.retry.attempts", "db.retry.backoff", "db.retry.jitter",

		"cache.host", "cache.port", "cache.ttl", "cache.size", "cache.evict",

		"log.level", "log.format", "log.output", "log.sampling", "log.caller",

		"metrics.enabled", "metrics.host", "metrics.port",
		"metrics.path", "metrics.interval",

		"feature.alpha", "feature.beta", "feature.gamma", "feature.ratio",

		"tags",
		"limits.rps", "limits.burst", "limits.conn",
	}
}

// SmallKeys is [LargeKeys] for [Small].
func SmallKeys() []string {
	return []string{"name", "port", "debug", "rate", "region"}
}

// ApplyEnv replaces the process environment with exactly vars.
//
// Clearing first is what makes the measurement reproducible: koanf and viper
// read the whole environ, so a run under a shell with forty exported variables
// and a run under CI with four would not be the same benchmark. Every library
// then reads one identical environment through the ordinary os package, which
// is also the only way viper can be measured at all - it has no hook for an
// injected environment.
func ApplyEnv(vars map[string]string) {
	os.Clearenv()

	for _, k := range sortedKeys(vars) {
		_ = os.Setenv(k, vars[k])
	}
}

// sortedKeys keeps the order the environment is built in deterministic, so
// that anything downstream which happens to depend on os.Environ's order is
// at least stable between runs.
func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

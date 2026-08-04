// Package bench is ferry's performance harness: one struct per scenario, one
// adapter per library, and an equivalence check that refuses to benchmark
// anything until every library has produced the identical populated value.
//
// It is a module of its own and it never ships. It lives on the perf/
// branch so that viper's and koanf's dependency trees stay out of the
// repository people clone, out of Renovate's scope, and out of the supply
// chain of a library whose core has zero non-stdlib dependencies.
//
// # The struct tags, and why there are only three of them
//
// Every library under test reads a struct tag, and their grammars are not the
// same. Giving each one its own tag key would be the obvious answer and it is
// the wrong one: reflect.StructTag.Get scans the whole tag string linearly on
// every lookup, so a fat tag is a per-field cost charged to exactly the
// libraries that reflect on every call and not to the one that compiles once.
// That confound is measured rather than assumed, in BenchmarkStructTagCost.
//
// So the tags are pooled by grammar, and every library that can be told which
// key to read is told to share one:
//
//	yaml:"seg"      ferry (ferry.TagKey), yaml.v3, koanf (TagName),
//	                viper (DecoderConfigOption)
//	env:"SEG"       xload, go-envconfig - their grammars are identical for
//	                the name, the prefix= option and the separator= option
//	(none)          kelseyhightower/envconfig derives its key from the Go
//	                field name, which is already the segment name
//
// caarlos0/env is the one library that cannot join either pool: it rejects
// both `prefix=` and `separator=` as unsupported tag options, so it would need
// a third key on every field. It is reported as not measured rather than given
// a tag nobody else pays for.
package bench

// Small is the flat five-field scenario: no nesting, no composites, the shape
// a mapping layer has the least room to hide in.
type Small struct {
	Name   string  `yaml:"name" env:"NAME"`
	Port   int     `yaml:"port" env:"PORT"`
	Debug  bool    `yaml:"debug" env:"DEBUG"`
	Rate   float64 `yaml:"rate" env:"RATE"`
	Region string  `yaml:"region" env:"REGION"`
}

// Large is the fifty-one-leaf scenario: six sub-structs, two of them nesting a
// third level, a slice and a map. The slice and the map are the point - a
// mapping layer's per-address cost is invisible at five fields and dominant at
// fifty, and a dynamic composite is the one shape that forces a plane to be
// enumerated rather than read at known addresses.
type Large struct {
	Name     string `yaml:"name" env:"NAME"`
	Env      string `yaml:"env" env:"ENV"`
	Version  string `yaml:"version" env:"VERSION"`
	Debug    bool   `yaml:"debug" env:"DEBUG"`
	Replicas int    `yaml:"replicas" env:"REPLICAS"`

	Server  Server  `yaml:"server" env:",prefix=SERVER_"`
	DB      DB      `yaml:"db" env:",prefix=DB_"`
	Cache   Cache   `yaml:"cache" env:",prefix=CACHE_"`
	Log     Log     `yaml:"log" env:",prefix=LOG_"`
	Metrics Metrics `yaml:"metrics" env:",prefix=METRICS_"`
	Feature Feature `yaml:"feature" env:",prefix=FEATURE_"`

	// The two composites carry a third tag key, and only these two fields do.
	//
	// ferry addresses a slice element and a map entry at TAGS_0 and
	// LIMITS_RPS, so the address /tags is a container and holds nothing
	// itself. Setting TAGS as well - the delimited spelling the other three
	// env libraries want - puts a string at that container address, which core
	// no longer reads at all where the plane holds children under it; see the
	// note on EnvLarge in fixture.go for what that leaves of the reason, and
	// TestFerryReadsTheChildrenAndNotTheContainer for the measurement.
	//
	// So the delimited spelling gets a name of its own, out of the way of the
	// indexed one. kelseyhightower/envconfig derives its key from the field
	// name and is the one library that then needs to be told otherwise, which
	// is what its tag is doing here and nowhere else in the fixture.
	Tags   []string          `yaml:"tags" env:"CSV_TAGS" envconfig:"CSV_TAGS"`
	Limits map[string]string `yaml:"limits" env:"KV_LIMITS,separator=:" envconfig:"KV_LIMITS"`
}

// Server is the listener, and the first of the two three-level branches.
type Server struct {
	Host string `yaml:"host" env:"HOST"`
	Port int    `yaml:"port" env:"PORT"`
	HTTP HTTP   `yaml:"http" env:",prefix=HTTP_"`
	TLS  TLS    `yaml:"tls" env:",prefix=TLS_"`
}

// HTTP is the server's protocol settings.
type HTTP struct {
	Port     int  `yaml:"port" env:"PORT"`
	Read     int  `yaml:"read" env:"READ"`
	Write    int  `yaml:"write" env:"WRITE"`
	Idle     int  `yaml:"idle" env:"IDLE"`
	Header   int  `yaml:"header" env:"HEADER"`
	Compress bool `yaml:"compress" env:"COMPRESS"`
}

// TLS is the server's transport security settings.
type TLS struct {
	Enabled bool   `yaml:"enabled" env:"ENABLED"`
	Cert    string `yaml:"cert" env:"CERT"`
	Key     string `yaml:"key" env:"KEY"`
	Min     string `yaml:"min" env:"MIN"`
	Verify  bool   `yaml:"verify" env:"VERIFY"`
}

// DB is the database, and the second three-level branch.
type DB struct {
	Host  string `yaml:"host" env:"HOST"`
	Port  int    `yaml:"port" env:"PORT"`
	User  string `yaml:"user" env:"USER"`
	Pass  string `yaml:"pass" env:"PASS"`
	Name  string `yaml:"name" env:"NAME"`
	Pool  Pool   `yaml:"pool" env:",prefix=POOL_"`
	Retry Retry  `yaml:"retry" env:",prefix=RETRY_"`
}

// Pool is the database connection pool.
type Pool struct {
	Max      int `yaml:"max" env:"MAX"`
	Idle     int `yaml:"idle" env:"IDLE"`
	Lifetime int `yaml:"lifetime" env:"LIFETIME"`
	Timeout  int `yaml:"timeout" env:"TIMEOUT"`
}

// Retry is the database retry policy, and carries the only float in the branch.
type Retry struct {
	Attempts int     `yaml:"attempts" env:"ATTEMPTS"`
	Backoff  int     `yaml:"backoff" env:"BACKOFF"`
	Jitter   float64 `yaml:"jitter" env:"JITTER"`
}

// Cache is the cache tier.
type Cache struct {
	Host  string `yaml:"host" env:"HOST"`
	Port  int    `yaml:"port" env:"PORT"`
	TTL   int    `yaml:"ttl" env:"TTL"`
	Size  int    `yaml:"size" env:"SIZE"`
	Evict string `yaml:"evict" env:"EVICT"`
}

// Log is the logging configuration.
type Log struct {
	Level    string `yaml:"level" env:"LEVEL"`
	Format   string `yaml:"format" env:"FORMAT"`
	Output   string `yaml:"output" env:"OUTPUT"`
	Sampling bool   `yaml:"sampling" env:"SAMPLING"`
	Caller   bool   `yaml:"caller" env:"CALLER"`
}

// Metrics is the telemetry endpoint.
type Metrics struct {
	Enabled  bool   `yaml:"enabled" env:"ENABLED"`
	Host     string `yaml:"host" env:"HOST"`
	Port     int    `yaml:"port" env:"PORT"`
	Path     string `yaml:"path" env:"PATH"`
	Interval int    `yaml:"interval" env:"INTERVAL"`
}

// Feature is the feature-flag block.
type Feature struct {
	Alpha bool    `yaml:"alpha" env:"ALPHA"`
	Beta  bool    `yaml:"beta" env:"BETA"`
	Gamma bool    `yaml:"gamma" env:"GAMMA"`
	Ratio float64 `yaml:"ratio" env:"RATIO"`
}

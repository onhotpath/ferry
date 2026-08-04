package bench

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	yamlv3 "go.yaml.in/yaml/v3"
)

// The honest baseline. No mapping layer at all: os.Getenv and strconv by hand
// for the environment, and a direct yaml.Unmarshal into the struct for the
// file, which is the closest thing the standard library has to a YAML case
// since it has no YAML at all.
//
// This is the column that says what the abstraction costs. It is also the
// column nobody would actually write for fifty-one fields, which is the other
// half of the same finding.
const (
	stdlibNotesEnv = "Hand-rolled os.Getenv plus strconv, one line per field, no reflection " +
		"anywhere. Nothing to cache and nothing to configure. Absence is indistinguishable " +
		"from empty, there is no required, no default, and no error naming the field that " +
		"failed - the things the other columns are paying for."
	stdlibNotesYAML = "go.yaml.in/yaml/v3 Unmarshal straight into the struct: no mapping layer, no " +
		"intermediate map. yaml.v3 keeps a per-type field cache of its own that no " +
		"caller can defeat, so its cold column is not a true cold measurement and is " +
		"the same number as its warm one for that reason rather than for the others'."
	stdlibNotesDump = "yaml.Marshal plus os.WriteFile: the document is replaced whole. Comments, " +
		"key order, quoting and any key no field maps are lost, and the write is not " +
		"atomic - a crash mid-write truncates the operator's file."
)

func stdlibEnvSmall() Impl {
	return Impl{
		Name: "stdlib", Notes: stdlibNotesEnv, Baseline: true,
		New: func(*Fixture) (Loader, error) {
			return func(dst any) error {
				p, err := dstOf[Small](dst)
				if err != nil {
					return err
				}

				v, err := loadSmallByHand()
				if err != nil {
					return err
				}

				*p = v

				return nil
			}, nil
		}}
}

func stdlibEnvLarge() Impl {
	return Impl{
		Name: "stdlib", Notes: stdlibNotesEnv, Baseline: true,
		New: func(*Fixture) (Loader, error) {
			return func(dst any) error {
				p, err := dstOf[Large](dst)
				if err != nil {
					return err
				}

				v, err := loadLargeByHand()
				if err != nil {
					return err
				}

				*p = v

				return nil
			}, nil
		}}
}

func loadSmallByHand() (Small, error) {
	var (
		out Small
		err error
	)

	out.Name = os.Getenv("NAME")
	out.Region = os.Getenv("REGION")

	if out.Port, err = envInt("PORT"); err != nil {
		return Small{}, err
	}

	if out.Debug, err = envBool("DEBUG"); err != nil {
		return Small{}, err
	}

	if out.Rate, err = envFloat("RATE"); err != nil {
		return Small{}, err
	}

	return out, nil
}

// loadLargeByHand is split by branch, one function per sub-struct, because
// fifty-one assignments in one body is what the linter's function-length limit
// exists to prevent and because it is how anybody would actually write it.
func loadLargeByHand() (Large, error) {
	var (
		out Large
		err error
	)

	out.Name = os.Getenv("NAME")
	out.Env = os.Getenv("ENV")
	out.Version = os.Getenv("VERSION")

	if out.Debug, err = envBool("DEBUG"); err != nil {
		return Large{}, err
	}

	if out.Replicas, err = envInt("REPLICAS"); err != nil {
		return Large{}, err
	}

	if err := loadLargeBranches(&out); err != nil {
		return Large{}, err
	}

	out.Tags = strings.Split(os.Getenv("CSV_TAGS"), ",")

	if out.Limits, err = envMap("KV_LIMITS"); err != nil {
		return Large{}, err
	}

	return out, nil
}

func loadLargeBranches(out *Large) error {
	if err := loadServerByHand(out); err != nil {
		return err
	}

	if err := loadDBByHand(out); err != nil {
		return err
	}

	if err := loadCacheByHand(out); err != nil {
		return err
	}

	if err := loadLogByHand(out); err != nil {
		return err
	}

	if err := loadMetricsByHand(out); err != nil {
		return err
	}

	return loadFeatureByHand(out)
}

func loadServerByHand(out *Large) error {
	var err error

	out.Server.Host = os.Getenv("SERVER_HOST")
	out.Server.TLS.Cert = os.Getenv("SERVER_TLS_CERT")
	out.Server.TLS.Key = os.Getenv("SERVER_TLS_KEY")
	out.Server.TLS.Min = os.Getenv("SERVER_TLS_MIN")

	for _, f := range []intField{
		{&out.Server.Port, "SERVER_PORT"},
		{&out.Server.HTTP.Port, "SERVER_HTTP_PORT"},
		{&out.Server.HTTP.Read, "SERVER_HTTP_READ"},
		{&out.Server.HTTP.Write, "SERVER_HTTP_WRITE"},
		{&out.Server.HTTP.Idle, "SERVER_HTTP_IDLE"},
		{&out.Server.HTTP.Header, "SERVER_HTTP_HEADER"},
	} {
		if *f.dst, err = envInt(f.key); err != nil {
			return err
		}
	}

	return eachBool([]boolField{
		{&out.Server.HTTP.Compress, "SERVER_HTTP_COMPRESS"},
		{&out.Server.TLS.Enabled, "SERVER_TLS_ENABLED"},
		{&out.Server.TLS.Verify, "SERVER_TLS_VERIFY"},
	})
}

func loadDBByHand(out *Large) error {
	var err error

	out.DB.Host = os.Getenv("DB_HOST")
	out.DB.User = os.Getenv("DB_USER")
	out.DB.Pass = os.Getenv("DB_PASS")
	out.DB.Name = os.Getenv("DB_NAME")

	for _, f := range []intField{
		{&out.DB.Port, "DB_PORT"},
		{&out.DB.Pool.Max, "DB_POOL_MAX"},
		{&out.DB.Pool.Idle, "DB_POOL_IDLE"},
		{&out.DB.Pool.Lifetime, "DB_POOL_LIFETIME"},
		{&out.DB.Pool.Timeout, "DB_POOL_TIMEOUT"},
		{&out.DB.Retry.Attempts, "DB_RETRY_ATTEMPTS"},
		{&out.DB.Retry.Backoff, "DB_RETRY_BACKOFF"},
	} {
		if *f.dst, err = envInt(f.key); err != nil {
			return err
		}
	}

	out.DB.Retry.Jitter, err = envFloat("DB_RETRY_JITTER")

	return err
}

func loadCacheByHand(out *Large) error {
	var err error

	out.Cache.Host = os.Getenv("CACHE_HOST")
	out.Cache.Evict = os.Getenv("CACHE_EVICT")

	for _, f := range []intField{
		{&out.Cache.Port, "CACHE_PORT"},
		{&out.Cache.TTL, "CACHE_TTL"},
		{&out.Cache.Size, "CACHE_SIZE"},
	} {
		if *f.dst, err = envInt(f.key); err != nil {
			return err
		}
	}

	return nil
}

func loadLogByHand(out *Large) error {
	out.Log.Level = os.Getenv("LOG_LEVEL")
	out.Log.Format = os.Getenv("LOG_FORMAT")
	out.Log.Output = os.Getenv("LOG_OUTPUT")

	return eachBool([]boolField{
		{&out.Log.Sampling, "LOG_SAMPLING"},
		{&out.Log.Caller, "LOG_CALLER"},
	})
}

func loadMetricsByHand(out *Large) error {
	var err error

	out.Metrics.Host = os.Getenv("METRICS_HOST")
	out.Metrics.Path = os.Getenv("METRICS_PATH")

	if out.Metrics.Enabled, err = envBool("METRICS_ENABLED"); err != nil {
		return err
	}

	if out.Metrics.Port, err = envInt("METRICS_PORT"); err != nil {
		return err
	}

	out.Metrics.Interval, err = envInt("METRICS_INTERVAL")

	return err
}

func loadFeatureByHand(out *Large) error {
	if err := eachBool([]boolField{
		{&out.Feature.Alpha, "FEATURE_ALPHA"},
		{&out.Feature.Beta, "FEATURE_BETA"},
		{&out.Feature.Gamma, "FEATURE_GAMMA"},
	}); err != nil {
		return err
	}

	var err error
	out.Feature.Ratio, err = envFloat("FEATURE_RATIO")

	return err
}

// intField is boolField for the integer leaves.
type intField struct {
	dst *int
	key string
}

// boolField pairs a destination with its variable name. A slice rather than a
// map, because a map literal per call would put an allocation in the baseline
// column that the baseline does not actually need, and a baseline carrying an
// allocation nobody would write is a baseline that flatters everything else.
type boolField struct {
	dst *bool
	key string
}

func eachBool(fields []boolField) error {
	for _, f := range fields {
		v, err := envBool(f.key)
		if err != nil {
			return err
		}

		*f.dst = v
	}

	return nil
}

func envInt(key string) (int, error) {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil {
		return 0, fmt.Errorf("bench: stdlib %s: %w", key, err)
	}

	return n, nil
}

func envBool(key string) (bool, error) {
	b, err := strconv.ParseBool(os.Getenv(key))
	if err != nil {
		return false, fmt.Errorf("bench: stdlib %s: %w", key, err)
	}

	return b, nil
}

func envFloat(key string) (float64, error) {
	f, err := strconv.ParseFloat(os.Getenv(key), 64)
	if err != nil {
		return 0, fmt.Errorf("bench: stdlib %s: %w", key, err)
	}

	return f, nil
}

func envMap(key string) (map[string]string, error) {
	raw := os.Getenv(key)
	out := make(map[string]string)

	for _, pair := range strings.Split(raw, ",") {
		k, v, ok := strings.Cut(pair, ":")
		if !ok {
			return nil, fmt.Errorf("bench: stdlib %s: %q is not a key:value pair", key, pair)
		}

		out[k] = v
	}

	return out, nil
}

func stdlibYAML[T any](path func(*Fixture) string) Impl {
	return Impl{
		Name: "stdlib", Module: "go.yaml.in/yaml/v3", Notes: stdlibNotesYAML, Baseline: true,
		New: func(f *Fixture) (Loader, error) {
			p := path(f)

			return func(dst any) error {
				if _, err := dstOf[T](dst); err != nil {
					return err
				}

				b, err := os.ReadFile(p) //nolint:gosec // the path is the harness's own fixture
				if err != nil {
					return fmt.Errorf("bench: stdlib read: %w", err)
				}

				if err := yamlv3.Unmarshal(b, dst); err != nil {
					return fmt.Errorf("bench: stdlib unmarshal: %w", err)
				}

				return nil
			}, nil
		}}
}

func stdlibYAMLSmall() Impl { return stdlibYAML[Small](func(f *Fixture) string { return f.YAMLSmall }) }
func stdlibYAMLLarge() Impl { return stdlibYAML[Large](func(f *Fixture) string { return f.YAMLLarge }) }

func stdlibDumpLarge() Impl {
	return Impl{
		Name: "stdlib", Module: "go.yaml.in/yaml/v3", Notes: stdlibNotesDump, Baseline: true,
		New: func(f *Fixture) (Loader, error) {
			path, err := f.Seed("stdlib", YAMLLarge)
			if err != nil {
				return nil, err
			}

			want := WantLarge()

			return func(dst any) error {
				b, err := yamlv3.Marshal(want)
				if err != nil {
					return fmt.Errorf("bench: stdlib marshal: %w", err)
				}

				if err := os.WriteFile(path, b, 0o600); err != nil {
					return fmt.Errorf("bench: stdlib write: %w", err)
				}

				return readBackLarge(path, dst)
			}, nil
		}}
}

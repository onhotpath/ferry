module github.com/onhotpath/ferry/bench

// The performance harness. It never ships, it is never merged to main, and it
// is deliberately outside the repository root's go.work.
//
// ci.yml discovers modules by globbing driver/*/go.mod and prepending ".", and
// its "go.work uses every discovered module" step requires go.work's use list
// to equal that discovery exactly. Adding ./bench to the root workspace would
// therefore fail CI by design. So this module carries a go.work of its own -
// see go.work beside this file - which lists .., ../driver/env and
// ../driver/yaml, and everything run from this directory resolves core and the
// two drivers sibling-on-disk from the surrounding checkout.
//
// There is consequently no `replace` directive here and no `require` on core.
// The replace assertion in ci.yml does not reach this module, but ADR-0002's
// rule is worth keeping anyway: a checked-in replace would mean this harness
// never once built against the layout a real tree has. The missing require is
// the same reason driver/yaml has none - core carries no v* tag, so
// github.com/onhotpath/ferry@v0.0.0 cannot be resolved from the proxy, and a
// module with third-party requirements loads the full module graph and reads
// core's go.mod at the named version rather than taking the workspace copy.
//
// `go mod tidy` re-adds those requires. Drop them again afterwards; the recipe
// is in README.md in this directory.
go 1.27

require (
	github.com/fatih/structs v1.1.0
	github.com/go-viper/mapstructure/v2 v2.4.0
	github.com/gojekfarm/xtools/xload v0.10.0
	github.com/gojekfarm/xtools/xload/providers/yaml v0.10.0
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/knadh/koanf/parsers/yaml v1.1.1
	github.com/knadh/koanf/providers/env/v2 v2.0.1
	github.com/knadh/koanf/providers/file v1.2.1
	github.com/knadh/koanf/providers/structs v1.0.1
	github.com/knadh/koanf/v2 v2.3.6
	github.com/sethvargo/go-envconfig v1.4.3
	github.com/spf13/viper v1.21.0
	go.yaml.in/yaml/v3 v3.0.5
)

require (
	github.com/fsnotify/fsnotify v1.9.0 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/sagikazarmark/locafero v0.11.0 // indirect
	github.com/sourcegraph/conc v0.3.1-0.20240121214520-5f936abd7ae8 // indirect
	github.com/spf13/afero v1.15.0 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	golang.org/x/sys v0.32.0 // indirect
	golang.org/x/text v0.28.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

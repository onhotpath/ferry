
Measured, not claimed.
The table is machine-generated from a benchmark run; the harness refuses to run at
all unless every library produces the identical struct from the identical source.

| scenario | ferry (warm) | fastest other | |
| --- | --- | --- | --- |
| `env_small` | 2.75µs | 166ns (stdlib) | ferry 16.60x slower |
| `yaml_small` | 17.4µs | 16.6µs (stdlib) | ferry 1.05x slower |
| `no_such_scenario` | not measured | not measured | |

Left out of the comparison above because its warm figure measures a different job:
`xload` in `yaml_small`.
The results file says what the difference is, and gives the column where those
rows are comparable.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/perf/perf-dark.svg">
  <img alt="Time and allocations per load for every library in every scenario, cold and warm, with benchstat's confidence interval. Time is a log scale; allocations are linear from zero." src="docs/perf/perf-light.svg">
</picture>

Full results, the machine, the toolchain, the competitor versions, what each library
actually did and what was not measured: [docs/perf/results.md](docs/perf/results.md).

Run on a workstation, not a runner, `-count 10`, `-benchtime 1s`, Go go1.27rc2.

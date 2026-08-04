
Measured, not claimed.
The table is machine-generated from a benchmark run; the harness refuses to run at
all unless every library produces the identical struct from the identical source.

| scenario | ferry (warm) | fastest other | |
| --- | --- | --- | --- |
| `env_small` | 2.80µs | 174ns (stdlib) | ferry 16.10x slower |
| `no_such_scenario` | not measured | not measured | |

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/perf/perf-dark.svg">
  <img alt="Time and allocations per load for every library in every scenario, cold and warm, with benchstat's confidence interval. Time is a log scale; allocations are linear from zero." src="docs/perf/perf-light.svg">
</picture>

Full results, the machine, the toolchain, the competitor versions, what each library
actually did and what was not measured: [docs/perf/results.md](docs/perf/results.md).

Run on a workstation, not a runner, `-count 10`, `-benchtime 1s`, Go go1.27rc2.

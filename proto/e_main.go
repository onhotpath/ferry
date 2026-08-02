package main

var e9Hooks []func()

func runIssue9() {
	runCensus()
	for _, h := range e9Hooks {
		h()
	}
}

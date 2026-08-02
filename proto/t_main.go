package main

var t11Hooks []func()

func runT11() {
	for _, h := range t11Hooks {
		h()
	}
}

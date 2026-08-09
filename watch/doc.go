// Package watch turns a driver's change callback into a stream of freshly
// loaded values.
//
// A watchable driver announces a change by calling a func(context.Context)
// that carries no payload: it says the plane may have changed and nothing
// more. [Signal] is a value that records such a call, and [Values] is the loop
// that reloads through a [ferry.Binding] and yields a brand-new T each time.
//
// Wiring it up is a signal, the driver's own watch option and a range, here
// against driver/env:
//
//	s := watch.New()
//
//	src := env.New(env.DotEnv(".env"), env.WatchFiles(ctx, s.Changed))
//	b, err := ferry.Bind[Config](src)
//	if err != nil {
//		return err
//	}
//
//	cfg, err := b.Load(ctx) // the value to start from
//	if err != nil {
//		return err
//	}
//	publish(cfg)
//
//	seq, errf := watch.Values(ctx, s, b)
//	for cfg := range seq {
//		publish(cfg) // replace the pointer, never mutate the old value
//	}
//	return errf()
//
// The ordering is the reason this package exists. A driver opens its watch when
// the source is built, which is before [ferry.Bind] has returned and long before
// there is a stream to range, so a change can land when there is nothing yet to
// load through. Nothing is lost: the Signal records it, and the stream opens
// with that reload.
//
// The helper starts no goroutines. The reload runs on the goroutine doing the
// ranging, and there is nothing to stop and nothing to close.
//
// One sharp edge belongs to the driver rather than to this package: a watch the
// driver loses - a watched directory removed, say - fires the callback one last
// time and then goes quiet, so the stream keeps ranging and stops reloading. The
// driver's own documentation says which endings it can and cannot announce.
//
// The design records behind these decisions are in docs/adr/.
package watch

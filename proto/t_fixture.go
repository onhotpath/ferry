package main

// The struct every #14 probe runs against.
//
// It is deliberately NOT a happy fixture. The handoff's stated trap for a
// prototype ticket is "a template with no secret in it", and the three shapes
// that decide this ticket are all here:
//
//   - a `required` field with no default, which is what a template exists to
//     tell the user about, and which ADR-0006 measured as unreachable by Load
//     from an empty plane;
//   - a `default=` holding a credential, which is the #10 x #14 interaction
//     the handoff names;
//   - an `omitzero` field at its zero value, a dynamic composite, and an
//     optional subtree, which are the three positions a zero-value dump does
//     not reach.

import "time"

type TDB struct {
	Host     string `ferry:"host,required"`
	Port     int    `ferry:"port,default=5432"`
	User     string `ferry:"user,default=app"`
	Password string `ferry:"password,default=hunter2"`
}

type TTLS struct {
	Cert string `ferry:"cert,required"`
	Key  string `ferry:"key"`
}

// The doc comments below exist for T6, which measures whether they are
// reachable at run time at all. They are ordinary Go doc comments and nothing
// in ferry knows about them.
type TConf struct {
	// The service name. Appears in logs and in metrics labels.
	Name string `ferry:"name,required"`
	// Address to listen on. A bare :port listens on every interface.
	Listen  string        `ferry:"listen,default=:8080"`
	Debug   bool          `ferry:"debug,omitzero"`
	Timeout time.Duration `ferry:"timeout,default=30s"`
	Region  string        `ferry:"region,default='us-east-1,us-west-2'"`
	DB      TDB           `ferry:"db"`
	// TLS is optional. Omit this whole section to serve plaintext.
	TLS  *TTLS    `ferry:"tls"`
	Tags []string `ferry:"tags"`
	// Per-route request ceilings, keyed by route name.
	Limits map[string]int `ferry:"limits"`
}

// TWithEmbed is T4's divergence fixture. It is at package level and not inside
// the probe because instantiating a generic function over a FUNCTION-LOCAL
// type crashes the go1.27rc2 linker: `R_USEIFACE in main.runT4 references
// type:.eqfunc.M1K7S which is not a type or itab`. Recorded rather than
// worked around silently; it is a toolchain bug, not a ferry one, and
// ADR-0005 already notes that 1.27 was still rc2 when this design ran.
type TCommon struct {
	Env string `ferry:"env,default=prod"`
}

type TWithEmbed struct {
	TCommon
	Port int `ferry:"port,default=8080"`
}

// --- T8's audit fixtures. Package level, per the linker note above. ---

type TServer struct {
	Host string `ferry:"host,required"`
	Port int    `ferry:"port,default=8080"`
}

// TDyn puts `required` on a leaf under a DYNAMIC address shape.
type TDyn struct {
	Servers map[string]TServer `ferry:"servers"`
}

type TReqOmit struct {
	Name  string `ferry:"name,required,omitzero"`
	Other string `ferry:"other,default=x"`
}

type TReqOmitInt struct {
	Port  int    `ferry:"port,required,omitzero"`
	Other string `ferry:"other,default=x"`
}

type TBad struct {
	Name string `ferry:"name,requird"`
}

// TOpaque maps no address without a registered codec, which is T7(d)'s case.
type TOpaque struct{ v int }

type TOpaqueConf struct {
	Listen string  `ferry:"listen,default=:8080"`
	Handle TOpaque `ferry:"handle"`
}

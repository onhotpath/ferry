module github.com/onhotpath/ferry/proto/httpdecisions

// Throwaway prototype for issue #210. Never merges.
//
// It is deliberately outside the root go.work so that nothing in the shipped
// tree sees it; the go.work beside this file is what resolves core
// sibling-on-disk. It carries no third-party dependency: the prior-art survey
// #193 needed is already recorded, and every question here is answered against
// core, ferrytest and the standard library alone.
go 1.27

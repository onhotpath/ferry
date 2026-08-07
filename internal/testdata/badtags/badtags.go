// Package badtags holds the struct tags a source file cannot carry safely.
//
// It lives under testdata, and that is the whole reason it exists as a package.
// Two of the tags below make `go vet` report the file that declares them: a
// bare double quote and an escape Go does not define are exactly what the
// structtag analyser is for. ferry's own scanner is what turns them into a
// diagnosis rather than a silence (ADR-0008), so a test has to be able to
// present them to Compile, and a fixture that fails `go vet` in the module
// under test is not a fixture anyone can keep.
//
// The rest are tags the linter refuses rather than the compiler: a tag on an
// unexported field, an option beside "-", and a misspelling the spell checker
// knows. Every one of them is a mistake ferry has to diagnose, and every one of
// them is a mistake a linter is right to report where it is written on purpose.
//
// The path carries both of the properties this needs. The go command never
// matches a directory named testdata against ./... at any depth, so nothing
// here is built, vetted or linted with the module while an explicit import
// still resolves it; and the internal element above it means no importer
// outside ferry can reach it, so a package of deliberately broken struct tags
// is not part of what ferry publishes.
package badtags

// BareQuote carries a bare double quote inside its ferry tag, which truncates
// the value reflect.StructTag.Get returns and destroys the json tag beside it.
type BareQuote struct {
	Origins string `ferry:"origins,default=["value"]" json:"origins"`
}

// BadEscape carries an escape sequence Go does not define, which makes the tag
// invisible to Lookup rather than wrong.
type BadEscape struct {
	Host string `ferry:"a\,b"`
}

// Unterminated carries a quoted value with no closing quote.
type Unterminated struct {
	Host string `ferry:"a\"`
}

// Duplicate carries two ferry tags, which Get resolves by returning the first
// and `go vet` does not check at all.
type Duplicate struct {
	Host string `ferry:"first" ferry:"second"`
}

// ForeignBroken carries a clean ferry tag beside another library's malformed
// one, which is `go vet`'s problem and not ferry's.
type ForeignBroken struct {
	Host string `ferry:"host" json:"host\,x"`
}

// ForeignBareQuote carries a clean ferry tag beside another library's bare
// double quote, which destroys the tags after it without ferry's help.
type ForeignBareQuote struct {
	Host string `ferry:"host" json:"a["b]"`
}

// TrailingWord carries a clean ferry tag followed by text that is not a
// key:"value" pair at all, and never reaches a byte a key may not contain.
type TrailingWord struct {
	Host string `ferry:"host" notatag`
}

// ForeignKeySuffix carries one malformed tag under a key that merely ends in
// ferry's own, which a substring match claims as ferry's (#261).
type ForeignKeySuffix struct {
	Host string `xferry:"oops`
}

// ForeignKeySuffixAtEnd is the suffix case with nothing after the opening
// quote, so the scan for ferry's own key runs off the end of the tag rather
// than failing to find a second occurrence of it.
type ForeignKeySuffixAtEnd struct {
	Host string `xferry:"`
}

// ForeignKeyJSON is the same field under json, which is the answer every key
// ferry does not own has to get.
type ForeignKeyJSON struct {
	Host string `json:"oops`
}

// ForeignEnvSuffix is the suffix case under TagKey("env"), where the keys in
// use are short enough that the collision is common rather than contrived.
type ForeignEnvSuffix struct {
	Host string `myenv:"oops`
}

// ForeignDBSuffix is the same under TagKey("db").
type ForeignDBSuffix struct {
	Host string `sqldb:"oops`
}

// ForeignKeyPromoted carries the suffix key on an embedded field, which
// promotes because ferry finds no tag of its own on it.
type ForeignKeyPromoted struct {
	Promoted `xferry:"oops`
}

// Promoted is the struct ForeignKeyPromoted promotes.
type Promoted struct {
	Host string `ferry:"host"`
}

// UnexportedForeign carries another library's malformed tag on a field reflect
// can never set.
type UnexportedForeign struct {
	broken string `xferry:"oops`
	OK     string `ferry:"ok"`
}

// UnexportedMalformed carries a malformed tag under ferry's own key on a field
// reflect can never set, which is the one place ferry's own key is not ferry's
// to refuse.
type UnexportedMalformed struct {
	broken string `ferry:"oops`
	OK     string `ferry:"ok"`
}

// DashOption asks for an option beside "-", which names no segment for the
// option to be about.
type DashOption struct {
	Host string `ferry:"-,required"`
}

// UnexportedTagged carries a tag reflect can never act on.
type UnexportedTagged struct {
	host string `ferry:"host"`
	OK   string `ferry:"ok"`
}

// UnexportedSkipped carries the one tag an unexported field may have, which is
// redundant and accepted.
type UnexportedSkipped struct {
	hidden  string
	ignored string `ferry:"-"`
	OK      string `ferry:"ok"`
}

// Touch is what stops the two unexported fields above being unused. They exist
// to be looked at through reflect, which no compiler can see.
func (u *UnexportedSkipped) Touch() { u.hidden, u.ignored = "", "" }

// Transposed and TransposedAgain are two of the four misspellings ADR-0008
// names, both of them a transposition of the same word and neither of them
// something json/v2's normalisation would catch.
type Transposed struct {
	F string `ferry:"f,defualt=x"`
}

// TransposedAgain is the other way round.
type TransposedAgain struct {
	F string `ferry:"f,deafult=x"`
}

// DuplicateExtension carries two tags under one declared extension key, which
// is the same silence Duplicate is and is diagnosed by the same scanner.
type DuplicateExtension struct {
	Host string `ferry:"host" mylib:"node=a" mylib:"node=b"`
}

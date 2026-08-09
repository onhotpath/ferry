# ferry/windows/protect

Encrypt the values a struct marks as secret on their way into a plane, and decrypt them on the way back out, with Windows DPAPI-NG.

```go
import "github.com/onhotpath/ferry/driver/windows/protect"
```

It is a decorator and not a plane.
It wraps somebody else's `ferry.Source` and `ferry.Sink` - the registry, a key-value store, a file - and changes what is stored at the addresses a field marked, leaving every other address and everything else about that plane alone.
`NCryptProtectSecret` maps bytes to bytes: it has no namespace, nothing to enumerate and nothing to address, so there is no plane here to be.

## Read this part first

**This is a large improvement over plaintext. It is not a vault.**

Decrypting a value requires being the principal the descriptor names, on the machine that encrypted it.
That stops another account on the machine reading the store, and it stops a copy of the store being read anywhere else.

**An attacker who takes the machine's own key material recovers the value offline, at their leisure** - for `LOCAL=` descriptors that is the DPAPI master key material under the SYSTEM registry hive and the boot key.
Anyone with administrator rights on a live machine can also simply run as the principal the descriptor names and ask for the plaintext, which is what the descriptor says they may do.
If your threat model includes either of those, you want a key you hold somewhere else and this package is not it.

What it retires is the common Go-on-Windows mistake: classic DPAPI at `CRYPTPROTECT_LOCAL_MACHINE` with no entropy, written to a file whose access control list grants read to everyone.
Machine-scope DPAPI grants decryption to *every* principal on the machine, so the ACL is the only thing keeping the value in - and that list is usually inherited from a parent directory rather than chosen.
`protect.CurrentUser` says what classic machine scope cannot: this value is for the account that wrote it and for nothing else.

## Which descriptor works where

**A descriptor that names a security principal needs a domain.**
`SID=` and `SDDL=` rules are resolved by the Microsoft Key Protection Provider through Active Directory's key distribution service, so they work on a domain-joined machine and, on a machine that is not joined to a domain, `NCryptProtectSecret` fails at the first save with `NTE_ENCRYPTION_FAILURE` (`0x80090034`).
`LOCAL=` rules are resolved by the machine itself and need no domain at all.

This package ships three constants, and the third is not the default:

| constant | rule string | who can decrypt | needs a domain |
| --- | --- | --- | --- |
| `protect.CurrentUser` | `LOCAL=user` | the account the process runs as, on this machine | no |
| `protect.LocalMachine` | `LOCAL=machine` | every account on this machine | no |
| `protect.LocalSystem` | `SID=S-1-5-18` | the local system account, on this machine | **yes** |

**Start from `protect.CurrentUser`.**
A service running as the local system account and protecting under `LOCAL=user` gets exactly what `SID=S-1-5-18` promises - the value is for SYSTEM on this machine and for nothing else - and it gets it on a standalone machine too.
The sharp edge is that the principal is *whoever runs the process*: run the same program by hand as an ordinary user and the value is protected to that user, and the service will not be able to read it back.

**`protect.LocalMachine` is not an improvement on classic machine scope in access-control terms.**
Windows documents `LOCAL=machine` as protecting content to the local computer so that all users on it can decrypt, which is the same grant `CRYPTPROTECT_LOCAL_MACHINE` gives.
Reach for it only where more than one account genuinely has to read the value, and know that the store's ACL is then the only thing narrowing it down.

**`protect.LocalSystem` survives as a constant because it is the right answer on a domain-joined machine**, where the blob is bound to a key the domain issued and the principal is named rather than implied.
It is not the default, because most unattended services this package is aimed at do not run on a domain-joined machine, and there it fails every `Dump` - loudly, naming the status and the reason, but every one.

There is also `LOCAL=logon`, which this package does not ship a constant for: it protects to the current logon session and Windows documents the value as undecryptable after logoff or reboot, which is not what a configuration store is for.
Any DPAPI-NG rule string can be passed as a `protect.Descriptor`, including certificate and web-credential rules.

## Which addresses are protected, and why the tag

Selection is a struct tag key, `protect`, with one word, `secret`, and the caller declares the key on the registry:

```go
type Config struct {
    Auth struct {
        RefreshToken string `ferry:"refresh_token" protect:"secret"`
    } `ferry:"auth"`
}

var Registry = ferry.MustRegistry(ferry.WithTagKeys(protect.Extension()))

src := protect.Over(store, protect.CurrentUser, protect.FromTags())
cfg, err := ferry.Load[Config](ctx, src, ferry.WithRegistry(Registry))
```

Secrecy is a property of the field.
A refresh token is a secret on every plane, in every environment, forever, which is author-side knowledge and belongs where author-side knowledge goes.

A list of addresses handed to the constructor instead would go stale twice over.
A rename of the `ferry` tag breaks it, which can at least be caught at `Bind`.
A secret field *added* to the struct and not to the list is written in plaintext, and nothing anywhere can detect that.
The tag travels on the field, so both problems are structurally absent.

**There is no word that takes protection away.** No `protect:"plaintext"`, and there must never be one: two sources of truth that can contradict each other about whether a value is a secret can only be reconciled safely in one direction, so this vocabulary has nothing to contradict.

## The one mistake it refuses to let you make

A tag key nobody declared is another library's business and parses to nothing.
So a caller who wrapped the source with `protect` and forgot `ferry.WithTagKeys(protect.Extension())` would get an empty view, every `protect:"secret"` would be inert, and every marked value would be written **in the clear** - indistinguishable, from inside this package, from a struct that marks nothing.

`FromTags` refuses at `Bind` instead, before any read or write, with `protect.ErrNotDeclared` wrapping `ferry.ErrPlane`:

```go
func ExampleFromTags() {
	store, keeper := newStore(), newKeeper()

	// No ferry.WithTagKeys(protect.Extension()) anywhere, so every protect tag
	// in the struct is another library's business and parses to nothing.
	dst := protect.OverSink(storeSink{s: store}, protect.CurrentUser, protect.FromTags(), protect.Using(keeper))

	err := ferry.Dump(context.Background(), Settings{Auth: Credentials{RefreshToken: "s3cr3t"}}, dst)

	fmt.Println("refused:", errors.Is(err, protect.ErrNotDeclared))
	fmt.Println("and the store is untouched:", store.empty())

	// Output:
	// refused: true
	// and the store is untouched: true
}
```

A registry that *did* declare the key over a struct that marks nothing is not a refusal.
That is a schema with no secrets in it, which is a perfectly ordinary thing to run a protected source over, and telling the two apart is the whole reason `AddressSet.Extension` reports whether the key was declared.

## Loading and saving

```go
func Example() {
	store, keeper := newStore(), newKeeper()
	registry := ferry.MustRegistry(ferry.WithTagKeys(protect.Extension()))

	src := protect.Over(storeSource{s: store}, protect.CurrentUser, protect.FromTags(), protect.Using(keeper))
	dst := protect.OverSink(storeSink{s: store}, protect.CurrentUser, protect.FromTags(), protect.Using(keeper))

	want := Settings{Auth: Credentials{RefreshToken: "s3cr3t"}, Host: "example.internal"}

	if err := ferry.Dump(context.Background(), want, dst, ferry.WithRegistry(registry)); err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println("the store holds the token in the clear:", store.holds("s3cr3t"))
	fmt.Println("the host is stored as it always was:", rendered(store.at(ferry.At("host"))))

	got, err := ferry.Load[Settings](context.Background(), src, ferry.WithRegistry(registry))
	if err != nil {
		fmt.Println(err)

		return
	}

	fmt.Println("and it loads back as:", got.Auth.RefreshToken)

	// Output:
	// the store holds the token in the clear: false
	// the host is stored as it always was: example.internal
	// and it loads back as: s3cr3t
}
```

Both halves take the same descriptor, the same selector and the same `protect.Using`.
A sink protecting under one descriptor and a source unprotecting through another never meet, and nothing checks that the two agree.

`Over` and `OverSink` are two constructors rather than one, for the reason a plane's own driver ships two types: a `ferry.Source` and a `ferry.Sink` are two interfaces with one method name, and one value cannot have two `Bind` methods.
A plane with a read half and no honest write half is decorated by calling `OverSink` not at all.

## What is stored, and what survives the round trip

A protected value is stored as a **string**: a marker, then the ciphertext, base64 encoded.

```
ferry-protect:1:<base64>
```

A string rather than bytes, because this decorator composes with a plane it has never heard of: every plane that carries anything carries text, and a plane may hand text back as bytes where the field is a `[]byte`.
So the marker is looked for in a `String` and in a `Bytes`, and what is written is always a `String`.

The kind the value had travels **inside** the ciphertext, as one tag byte in front of the payload.
That is what makes the round trip exact rather than approximate: a `ferry.Number` comes back as the same number in the plane's own spelling, a bool comes back a bool, and bytes come back byte for byte.
Storing only the payload would return every protected value as a string, which is a lost bool and a renumbered number.

A **null is not protected**.
There is nothing at one to encrypt, and a plane's spelling of a null is how it says the field is nil; a ciphertext in its place would turn "nothing is here" into "something is here".

Two saves of one secret store two different ciphertexts, because DPAPI-NG is randomised.
Nothing here compares two ciphertexts and nothing may be built on their being equal.

## Migration, and what it costs

A value at a marked address that carries no marker is read back **as it stands**, and the next save writes it protected.
So an existing deployment migrates itself the first time it saves, with no migration step and no flag day.

**The accepted cost, stated plainly: anything that can write plaintext into the store can downgrade a field, and ferry accepts it silently.**
An attacker with write access to the store replaces a ciphertext with a plaintext of their choosing, the next load reads it, and nothing anywhere reports that the value used to be protected.
If write access to your store is part of your threat model, this decorator does not defend against it - and neither, note, does encryption without an integrity check over the whole store.

## How "not protected" is told from "could not be decrypted"

By the marker, and not by looking at the ciphertext.

DPAPI-NG's blob has a structure, and sniffing it would be a guess in both directions.
The direction that matters is that a value which *is* protected and cannot be decrypted must be **loud**, never passed through as plaintext, because a silent pass-through is how a load succeeds with a secret nobody could read.
A marker this package writes settles that exactly for everything this package wrote.

The one case a marker cannot settle: a value nobody ever protected that happens to begin with `ferry-protect:1:`.
That is reported as a failure rather than passed through, which is the safe direction of the two, and it is the honest limit of the scheme.

So, at a marked address:

| what the plane holds | what happens |
| --- | --- |
| a value this package wrote | decrypted, back at the kind and the exact text it was saved from |
| anything else | returned as it stands, and the next save writes it protected |
| the marker, and it will not decrypt | `protect.ErrCiphertext`, naming the address |

## What it keeps of the plane underneath

Everything.

A `ferry.Reader` and a `ferry.Writer` are one method each, and every other thing a driver can do - probing a container, enumerating one, releasing a resource, committing, forgetting a composite, being handed a dump's realised addresses, naming an address in the plane's own spelling, tolerating overlapping calls - is discovered by type assertion on the instance core was handed.
Go has no way to implement an interface conditionally, so a wrapper is exactly the set of methods its type declares, for every plane it is ever put in front of, and both mistakes are silent:

- declare **less** than the plane has and a capability disappears. A dropped `Unsetter` is refused at the open for every schema holding a slice or a map; a dropped `Enumerator` loads one as empty.
- declare **more** and the wrapper answers a question the plane cannot. A shell claiming `Committer` over a sink that has none reports that the sink stages when it does not.

So the shells are exhaustive: one type per combination, thirty-two on the read side and sixty-four on the write side, looked up by a bitmask.
Every entry is asserted to declare exactly what the plane it was handed declared, and the driver conformance suite is run over this decorator in front of a plane to prove the whole of it end to end.

## The seam

Protection reaches this package through one interface:

```go
type Protector interface {
	Protect(ctx context.Context, descriptor string, plaintext []byte) ([]byte, error)
	Unprotect(ctx context.Context, ciphertext []byte) ([]byte, error)
}
```

`protect.Using` is where one is handed over.
With no `Using`, a source or a sink reaches DPAPI-NG, which exists on Windows and nowhere else: everywhere else it refuses at `Bind` with `protect.ErrNoProtection`.

The real implementation sits behind `//go:build windows` and declares the four `ncrypt.dll` entry points itself, because `golang.org/x/sys/windows` wraps the classic `CryptProtectData` family and nothing from DPAPI-NG.
It takes no dependency beyond the one this module already has.

## Options

| option | what it does |
| --- | --- |
| `protect.Using(p)` | the protection to encrypt and decrypt through. Nil is DPAPI-NG. |

The descriptor and the selector are positional arguments rather than options, because a decorator with neither is not a decorator with a default: `protect.Over(store, "", nil)` is refused at `Bind`, and there is no descriptor safe enough to be assumed.

## Errors

Every one of these wraps `ferry.ErrPlane` and stays reachable under ferry's own wrapper, so `errors.Is` answers for them on what `ferry.Load` and `ferry.Dump` returned.

| error | when |
| --- | --- |
| `protect.ErrNotDeclared` | `FromTags` over a schema whose registry was never given `protect.Extension()`. At `Bind`. |
| `protect.ErrNoProtection` | no `Using`, and no DPAPI-NG on this operating system. At `Bind`. |
| `protect.ErrOption` | no plane to decorate, no selector, or an empty descriptor. At `Bind`. |
| `protect.ErrCiphertext` | a value that could not be encrypted, or a marked value that could not be decrypted. Named at the address. |

A mark at the address of a struct, a slice or a map is refused at `Bind` naming the address, with no sentinel of its own: a mark says how one value is stored, and those are places rather than values.

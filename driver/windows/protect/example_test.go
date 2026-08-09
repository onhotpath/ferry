package protect_test

import (
	"context"
	"errors"
	"fmt"

	"github.com/onhotpath/ferry"
	"github.com/onhotpath/ferry/driver/windows/protect"
)

// Settings is the struct both examples below are written over: one field marked
// as a secret, one field beside it that is not.
type Settings struct {
	Auth Credentials `ferry:"auth"`
	Host string      `ferry:"host"`
}

// Credentials holds the one marked field.
type Credentials struct {
	RefreshToken string `ferry:"refresh_token" protect:"secret"`
}

// Example saves a struct through a protected sink and loads it back, over a
// plane that is an ordinary address-keyed store rather than anything Windows.
//
// The protector here is this package's test double, which is what [protect.Using]
// is for; leave that option off and a real deployment reaches DPAPI-NG.
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

// ExampleFromTags shows the one mistake this package refuses to let you make: a
// registry that was never given [protect.Extension], which would leave every
// marked value written in the clear.
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

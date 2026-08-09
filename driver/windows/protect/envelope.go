package protect

import (
	"encoding/base64"
	"fmt"
	"strconv"

	"github.com/onhotpath/ferry"
)

// This file is what a protected value looks like where it comes to rest, and it
// is the whole of the round-trip obligation ADR-0021 puts on a consumer that
// changes a plane's bytes.
//
// # The wire kind is a string
//
// Protection turns a value into bytes, and bytes are the tempting kind to store.
// They are the wrong one here. This decorator composes with a plane it has never
// heard of, every plane that carries anything carries text, and a plane may hand
// text back as bytes where the field is a []byte - which driver/kv does, off the
// kind gate #340 added. So the marker is looked for in a String and in a Bytes,
// and what is written is always a String: one kind on the way in, two accepted
// on the way out, and no plane needed to be asked whether it has a bytes kind at
// all.
//
// # The original kind travels inside the ciphertext
//
// A [ferry.Number] is the plane's own spelling of a number and core compares
// what comes back with what went in, so encrypt-then-decrypt has to return the
// identical text and the identical kind. Storing only the payload would return
// every protected value as a String, which is a lost bool and a renumbered
// Number. So the plaintext is one tag byte and then the payload, the tag is
// inside the encryption rather than beside it, and what comes back is the value
// that was written.
//
// # A null is not protected
//
// There is nothing at a null to encrypt, and a plane's spelling of one is how it
// says the field is nil. Replacing it with a ciphertext would turn "nothing is
// here" into "something is here", which is a different observation on reload.

// marker is what says a stored value is one this package wrote.
//
// Detection is this marker and not a property of the ciphertext. DPAPI-NG's own
// blob has a structure, but sniffing it would be a guess in both directions -
// and the direction that matters is that a value which *is* a blob and cannot be
// decrypted must be loud rather than passed through as plaintext. A marker this
// package writes settles that exactly for everything this package wrote, and the
// one case it cannot settle is a value nobody protected that begins with these
// bytes: that is reported as a failure rather than passed through, which is the
// safe direction of the two.
const marker = "ferry-protect:1:"

// The tag byte one plaintext opens with: which kind the value was before it was
// protected.
const (
	tagString = 's'
	tagNumber = 'n'
	tagBool   = 'b'
	tagBytes  = 'y'
)

// wire is the base64 alphabet a ciphertext is stored under. Raw, because the
// marker already delimits the value and padding would only make two spellings of
// one blob.
var wire = base64.RawStdEncoding

// plainOf is one boundary value as the bytes to be protected: a tag byte for the
// kind, then the payload the kind carries.
//
// The second result is whether there is anything to protect. A null and an
// absence have no payload, so they are stored as they arrived - which is how a
// nil pointer at a protected address stays a nil pointer rather than becoming a
// ciphertext of nothing.
func plainOf(v ferry.Value) (plain []byte, has bool, err error) {
	switch v.Kind() {
	case ferry.KindString:
		s, err := v.AsString()

		return tagged(tagString, []byte(s), err)
	case ferry.KindNumber:
		s, err := v.AsNumber()

		return tagged(tagNumber, []byte(s), err)
	case ferry.KindBool:
		b, err := v.AsBool()

		return tagged(tagBool, []byte(strconv.FormatBool(b)), err)
	case ferry.KindBytes:
		b, err := v.AsBytes()

		return tagged(tagBytes, b, err)
	default:
		return nil, false, nil
	}
}

// tagged is the one line every arm of [plainOf] ends with, so that the switch
// reads as one decision rather than four copies of the same error check.
func tagged(tag byte, payload []byte, err error) (plain []byte, has bool, failed error) {
	if err != nil {
		return nil, false, err
	}

	return append([]byte{tag}, payload...), true, nil
}

// valueOf is [plainOf] backwards: the value a decrypted plaintext was written
// from, at the kind it was written at.
func valueOf(plain []byte) (ferry.Value, error) {
	if len(plain) == 0 {
		return ferry.Value{}, corrupt("it decrypted to nothing at all")
	}

	payload := string(plain[1:])

	switch plain[0] {
	case tagString:
		return ferry.String(payload), nil
	case tagNumber:
		return ferry.Number(payload), nil
	case tagBool:
		return boolValue(payload)
	case tagBytes:
		return ferry.Bytes(plain[1:]), nil
	default:
		return ferry.Value{}, corrupt(fmt.Sprintf("it decrypted to something opening with %q, which is not a "+
			"kind this package writes", plain[0]))
	}
}

// boolValue is the bool arm, which is the one kind whose payload can be wrong
// without the tag being wrong.
func boolValue(payload string) (ferry.Value, error) {
	b, err := strconv.ParseBool(payload)
	if err != nil {
		return ferry.Value{}, corrupt("it decrypted to " + strconv.Quote(payload) + ", which is not a boolean")
	}

	return ferry.Bool(b), nil
}

// stored is a ciphertext as one plane value: the marker, then the blob.
func stored(blob []byte) ferry.Value { return ferry.String(marker + wire.EncodeToString(blob)) }

// markedText is the text a stored value carries, and whether it is one this
// package wrote.
//
// A String and a Bytes are both looked at, for the reason at the top of this
// file: what is written is always a String, and a plane may hand it back as
// bytes where the field it is loading into is a []byte.
func markedText(v ferry.Value) (string, bool) {
	var (
		text string
		err  error
	)

	switch v.Kind() {
	case ferry.KindString:
		text, err = v.AsString()
	case ferry.KindBytes:
		var b []byte

		b, err = v.AsBytes()
		text = string(b)
	default:
		return "", false
	}

	if err != nil || len(text) < len(marker) || text[:len(marker)] != marker {
		return "", false
	}

	return text[len(marker):], true
}

// corrupt states the class this package has an opinion about and keeps
// [ErrCiphertext] reachable underneath it. The caller adds the address.
func corrupt(why string) error {
	return fmt.Errorf("%w: %w: %s", ferry.ErrPlane, ErrCiphertext, why)
}

// corruptBy is [corrupt] over a failure something else reported, keeping that
// failure's own class and text reachable: a protector's sentinel has to survive
// to the caller, exactly as a plane's does.
func corruptBy(why string, err error) error {
	return fmt.Errorf("%w: %w: %s: %w", ferry.ErrPlane, ErrCiphertext, why, err)
}

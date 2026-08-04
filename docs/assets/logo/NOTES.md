# ferry logo

| file | what it is for |
| --- | --- |
| `ferry.svg` | the mark, everywhere |
| `favicon-32.png`, `favicon-180.png`, `favicon-512.png` | raster renderings of it, for the places that cannot take an SVG |

One mark, used at every size.

## The mark

A vessel with a bow at each end and no stern, mirror symmetric about its centreline, sitting *in* a channel that carries an arrowhead at each end.

Double-ended ferries are a real class of ship: they never turn round, because both ends are the front.
That is `Load` and `Dump` off one struct, drawn.

Every configuration library in Go can load.
The one thing ferry claims that its neighbours do not is that the same annotated struct goes back out the way it came in, so the mark says that and nothing else.

The channel is drawn over the hull rather than under it, so the vessel sits in the water instead of floating above a graphic.
That costs the lower hull, and it is what makes the arrow read as a route rather than as a dimension line.
The thing to check with fresh eyes is that it keeps reading that way: a two-headed arrow is a common interface glyph and could be taken for a measurement annotation.

At 32 pixels the orange hull band, the white deck and both arrowheads still register, so the mark survives as an icon.
The window row does not, and it stops being visible somewhere around 60 pixels.
It costs nothing to keep and it is decoration below that size rather than information.

## Using it as an icon

There is no ferry website today, so nothing consumes this as a favicon yet.
When there is one, that is a single line:

```html
<link rel="icon" href="/ferry.svg" type="image/svg+xml">
```

The PNGs are for the places that still cannot take an SVG.
`favicon-32.png` is the classic tab-icon fallback, `favicon-180.png` is the Apple touch icon size, and `favicon-512.png` is the size a GitHub organisation or repository avatar wants.

A GitHub avatar cannot be set from a file in the repository; it is uploaded in settings.
The PNG is here so that upload has something to take.

Regenerate them from the SVG rather than editing them:

```
for s in 32 180 512; do resvg -w $s -h $s ferry.svg favicon-$s.png; done
```

## The house style it is matching

The owner's other projects, Darkroom and Heimdall: a single subject, centred, on a solid pale circle that fills the square.
Flat vector, a little tonal shading, a long soft shadow at 45 degrees to the lower right in a darker tint of the circle.
Two hues plus ink and near-white, no more.

| role | hex |
| --- | --- |
| circle | `#D9E7ED` |
| cast shadow | `#C2D7E0` |
| channel | `#1E6F8E` |
| hull | `#F0A93B` |
| boot-top | `#14536B` |
| superstructure | `#F4F8FA` |
| glass, funnel cap | `#0E2733` |

Marine and warm accent are the two hues.
Red was Darkroom's and gold was Heimdall's, so ferry takes the remaining obvious one and keeps the same warm accent trick Heimdall uses for its trim.

## Technical shape of the file

No `width` or `height` on the root, so it scales.
No style blocks, no CSS custom properties, no filters, no gradients, no `<text>`, no `<image>`, no external reference of any kind.
Every colour is an inline presentation attribute and every element is one of `svg`, `circle`, `g`, `path`.

That is the set GitHub's sanitiser cannot object to, and it is also the set that renders identically whether the file is inlined into markdown or served through camo as an image.

The long shadow is a plain filled shape at a slightly darker tint rather than a `<filter>`, so nothing can strip it.
Its far end is closed with an `A` arc on the circle's own radius rather than a `clipPath`, which is why there is no clipping machinery in the file.

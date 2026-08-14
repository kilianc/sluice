// Sluice counts codepoints; a JavaScript string — and therefore a CodeMirror
// document position — counts UTF-16 code units. Every offset crossing between
// them goes through this file.
//
// This is a copy of the same twenty lines in @sluice/monaco, deliberately. A
// binding that imports nothing can be dropped into any build, and two small
// copies pinned by the same test in both packages are cheaper than a dependency
// between two packages that otherwise never need to know about each other.

/** Number of codepoints in the first `utf16` code units of `text`. */
export function toCodepoint(text, utf16) {
  if (utf16 <= 0) return 0
  let count = 0
  for (let i = 0; i < text.length && i < utf16; i++) {
    const code = text.charCodeAt(i)
    if (code >= 0xd800 && code <= 0xdbff && i + 1 < text.length) {
      const next = text.charCodeAt(i + 1)
      if (next >= 0xdc00 && next <= 0xdfff) {
        if (i + 1 >= utf16) return count + 1 // cursor split the pair
        i++
      }
    }
    count++
  }
  return count
}

/** Number of UTF-16 code units in the first `codepoint` codepoints of `text`. */
export function toUTF16(text, codepoint) {
  if (codepoint <= 0) return 0
  let count = 0
  let i = 0
  for (const ch of text) {
    if (count >= codepoint) break
    i += ch.length
    count++
  }
  return i
}

// Sluice counts codepoints; Monaco counts UTF-16 code units. Every offset
// crossing between them goes through this file.
//
// The two agree for every ASCII filter anyone will type, which is exactly what
// makes the bug hard to find: it appears the first time a value contains an
// emoji or an astral character, and then every marker and every completion
// range after that character is off by one per astral character.

/** Number of codepoints in the first `utf16` code units of `text`. */
export function toCodepoint(text, utf16) {
  if (utf16 <= 0) return 0
  let count = 0
  for (let i = 0; i < text.length && i < utf16; i++) {
    // A high surrogate followed by a low surrogate is one codepoint. Counting
    // the pair on the low surrogate keeps a cursor sitting between the two —
    // which Monaco permits — from counting the character twice.
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

/** Convert a Sluice codepoint span to a Monaco range on the given model. */
export function spanToRange(monaco, model, text, span) {
  const start = model.getPositionAt(toUTF16(text, span[0]))
  const end = model.getPositionAt(toUTF16(text, span[1]))
  return new monaco.Range(
    start.lineNumber,
    start.column,
    end.lineNumber,
    end.column,
  )
}

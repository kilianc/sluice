// Syntax highlighting, derived from the same schema that drives everything else.
//
// The useful part is not colouring keywords — it is that a field name the schema
// does not declare is highlighted as invalid the moment it is typed, before
// validation has said anything. The editor tells you `stat` is wrong while you
// are still on the word.

/**
 * Build a Monarch tokenizer for a schema.
 * @param {{fields?: Array<{name: string}>}} schema
 */
export function monarchTokens(schema) {
  const fields = (schema.fields ?? []).map((f) => f.name.toLowerCase())
  return {
    ignoreCase: true,
    keywords: ['and', 'or', 'not', 'true', 'false'],
    fields,
    brackets: [{ open: '(', close: ')', token: 'delimiter.parenthesis' }],
    tokenizer: {
      root: [
        [
          /[A-Za-z_][A-Za-z0-9_.]*/,
          {
            cases: {
              '@keywords': 'keyword',
              '@fields': 'type.identifier',
              '@default': 'invalid',
            },
          },
        ],
        [/-?\d+(\.\d+)?/, 'number'],
        [/"([^"\\]|\\.)*$/, 'string.invalid'], // unterminated
        [/"/, { token: 'string.quote', next: '@string' }],
        [/!=|!~|<=|>=|[=~<>]/, 'operator'],
        [/[()]/, '@brackets'],
        [/\s+/, 'white'],
        [/./, 'invalid'],
      ],
      string: [
        [/[^"\\]+/, 'string'],
        [/\\["\\ntr]/, 'string.escape'],
        [/\\./, 'string.escape.invalid'],
        [/"/, { token: 'string.quote', next: '@pop' }],
      ],
    },
  }
}

/** The language configuration: brackets, auto-closing, and no comment syntax. */
export const languageConfiguration = {
  brackets: [['(', ')']],
  autoClosingPairs: [
    { open: '(', close: ')' },
    { open: '"', close: '"' },
  ],
  surroundingPairs: [
    { open: '(', close: ')' },
    { open: '"', close: '"' },
  ],
  // The language has no comments. Saying so stops Monaco from offering to
  // toggle one with the usual keybinding.
  comments: {},
}

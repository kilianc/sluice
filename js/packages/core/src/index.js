/**
 * @sluice/core — a configurable filter query language that compiles to SQL.
 *
 * Zero runtime dependencies, no build step, ESM only. This package never
 * imports an editor: bindings for Monaco and CodeMirror are separate packages,
 * so a headless client compiling for DuckDB-WASM pays nothing for them.
 */
export { createLanguage } from './compile.js'
export { codes, SchemaError, SluiceError } from './diagnostic.js'
export { decodeNode, encodeNode, isOperator, nodeDepth } from './ast.js'
export { Builder, likePattern } from './emit.js'
export { lex, kinds, tokenValue } from './lex.js'
export { parseString } from './parse.js'
export { asciiLower, defaultOperators, publicSchema } from './schema.js'
export { parseDuration } from './value.js'
export { dialects, duckdb, postgres } from './dialects.js'

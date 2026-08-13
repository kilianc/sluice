#!/usr/bin/env node
/**
 * The conformance adapter (AGENTS.md §11): JSON Lines on stdin/stdout, one
 * request per line, one response per line, in order. No banner output; anything
 * for humans goes to stderr. Exit 0 when stdin closes.
 */
import { createInterface } from 'node:readline'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'

import { createLanguage } from '../src/compile.js'
import { dialects } from '../src/dialects.js'
import { kinds, lex, tokenValue } from '../src/lex.js'

const languages = new Map()

function languageFor(request) {
  const key = JSON.stringify([request.schema, request.dynamic ?? null])
  const cached = languages.get(key)
  if (cached) return cached

  let schema = request.schema
  if (typeof schema === 'string') {
    const dir = process.env.SLUICE_CONFORMANCE_SCHEMAS ?? join('conformance', 'schemas')
    schema = JSON.parse(readFileSync(join(dir, `${schema}.json`), 'utf8'))
  }
  if (!schema || typeof schema !== 'object') throw new Error('request has no schema')

  const lang = createLanguage(schema, { dynamic: request.dynamic ?? {} })
  languages.set(key, lang)
  return lang
}

function dialectFor(request) {
  const dialect = dialects[request.dialect ?? 'postgres']
  if (!dialect) throw new Error(`unknown dialect ${request.dialect}`)
  return dialect
}

function handle(request) {
  switch (request.op) {
    case 'lex': {
      // Lexing needs no schema.
      const { tokens, diagnostics } = lex(request.input ?? '')
      return {
        tokens: tokens
          .filter((t) => t.kind !== kinds.EOF)
          .map((t) => ({ kind: t.kind, value: tokenValue(t), span: t.span })),
        diagnostics,
      }
    }
    case 'parse': {
      const { ast, diagnostics } = languageFor(request).parse(request.input ?? '')
      return { ast, hasAST: true, diagnostics }
    }
    case 'compile': {
      const lang = languageFor(request)
      const dialect = dialectFor(request)
      try {
        const out =
          request.ast !== undefined && request.ast !== null
            ? lang.compileAST(request.ast, dialect)
            : lang.compile(request.input ?? '', dialect)
        return { sql: out.sql, args: out.args, fields: out.fields, diagnostics: [] }
      } catch (err) {
        if (!err.diagnostic) throw err
        return { diagnostics: [err.diagnostic] }
      }
    }
    case 'validate':
      return { diagnostics: languageFor(request).validate(request.input ?? '').diagnostics }
    case 'suggest':
      return { suggestions: languageFor(request).suggest(request.input ?? '', request.cursor ?? 0) }
    case 'schema':
      return { schema: languageFor(request).publicSchema(request.dynamic ?? {}) }
    default:
      return { error: `unknown op ${request.op}` }
  }
}

/**
 * Applies the protocol's presence rules: keys irrelevant to the op are absent,
 * empty arrays that carry meaning are present, and sql is never emitted
 * alongside diagnostics.
 */
function respond(id, result) {
  const out = { id }
  if (result.tokens) out.tokens = result.tokens
  if (result.hasAST) out.ast = result.ast ?? null
  if ((result.diagnostics ?? []).length === 0 && result.sql !== undefined) {
    out.sql = result.sql
    out.args = result.args
    out.fields = result.fields
  }
  if (result.suggestions) out.suggestions = result.suggestions
  if (result.diagnostics) out.diagnostics = result.diagnostics
  if (result.schema) out.schema = result.schema
  if (result.error) out.error = result.error
  return out
}

const lines = createInterface({ input: process.stdin, crlfDelay: Infinity })
for await (const line of lines) {
  if (line.trim() === '') continue
  let request = {}
  let result
  try {
    request = JSON.parse(line)
    result = handle(request)
  } catch (err) {
    result = { error: `${err.message}` }
  }
  process.stdout.write(`${JSON.stringify(respond(request.id, result))}\n`)
}

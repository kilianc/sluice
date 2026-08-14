// A fake Monaco, so the binding can be tested without a browser.
//
// It implements only what the binding touches, but it implements the offset
// arithmetic honestly — UTF-16 code units, 1-based lines and columns — because
// that arithmetic is the thing most likely to be wrong.

export class Range {
  constructor(startLineNumber, startColumn, endLineNumber, endColumn) {
    this.startLineNumber = startLineNumber
    this.startColumn = startColumn
    this.endLineNumber = endLineNumber
    this.endColumn = endColumn
  }
}

export class FakeModel {
  constructor(text, languageId = 'sluice') {
    this.text = text
    this.languageId = languageId
    this.listeners = []
    this.disposed = false
  }

  getValue() {
    return this.text
  }

  getLanguageId() {
    return this.languageId
  }

  isDisposed() {
    return this.disposed
  }

  setValue(text) {
    this.text = text
    for (const fn of this.listeners) fn()
  }

  onDidChangeContent(fn) {
    this.listeners.push(fn)
    return { dispose: () => this.listeners.splice(this.listeners.indexOf(fn), 1) }
  }

  /** UTF-16 offset → { lineNumber, column }, both 1-based. */
  getPositionAt(offset) {
    let remaining = Math.max(0, Math.min(offset, this.text.length))
    const lines = this.text.split('\n')
    for (let i = 0; i < lines.length; i++) {
      if (remaining <= lines[i].length) {
        return { lineNumber: i + 1, column: remaining + 1 }
      }
      remaining -= lines[i].length + 1 // the newline
    }
    const last = lines[lines.length - 1]
    return { lineNumber: lines.length, column: last.length + 1 }
  }

  /** { lineNumber, column } → UTF-16 offset. */
  getOffsetAt({ lineNumber, column }) {
    const lines = this.text.split('\n')
    let offset = 0
    for (let i = 0; i < lineNumber - 1 && i < lines.length; i++) offset += lines[i].length + 1
    return offset + column - 1
  }

  getValueInRange(range) {
    const start = this.getOffsetAt({
      lineNumber: range.startLineNumber,
      column: range.startColumn,
    })
    const end = this.getOffsetAt({
      lineNumber: range.endLineNumber,
      column: range.endColumn,
    })
    return this.text.slice(start, end)
  }
}

export function createFakeMonaco() {
  const languages = []
  const registered = { completion: [], hover: [], monarch: new Map(), config: new Map() }
  const models = []
  const modelListeners = []
  const markers = new Map()

  const disposable = (fn) => ({ dispose: fn ?? (() => {}) })

  const monaco = {
    Range,
    MarkerSeverity: { Error: 8, Warning: 4, Info: 2, Hint: 1 },
    languages: {
      CompletionItemKind: {
        Field: 3,
        Operator: 11,
        Value: 12,
        Keyword: 17,
        Snippet: 27,
        Text: 18,
      },
      getLanguages: () => languages,
      register: (lang) => languages.push(lang),
      setLanguageConfiguration: (id, config) => {
        registered.config.set(id, config)
        return disposable()
      },
      setMonarchTokensProvider: (id, tokens) => {
        registered.monarch.set(id, tokens)
        return disposable()
      },
      registerCompletionItemProvider: (id, provider) => {
        const entry = { id, provider }
        registered.completion.push(entry)
        return disposable(() => {
          registered.completion.splice(registered.completion.indexOf(entry), 1)
        })
      },
      registerHoverProvider: (id, provider) => {
        const entry = { id, provider }
        registered.hover.push(entry)
        return disposable(() => {
          registered.hover.splice(registered.hover.indexOf(entry), 1)
        })
      },
    },
    editor: {
      getModels: () => models,
      onDidCreateModel: (fn) => {
        modelListeners.push(fn)
        return disposable(() => modelListeners.splice(modelListeners.indexOf(fn), 1))
      },
      setModelMarkers: (model, owner, list) => markers.set(model, list),
    },
  }

  return {
    monaco,
    registered,
    markersFor: (model) => markers.get(model) ?? [],
    /** Create a model and announce it, the way monaco.editor.createModel does. */
    createModel(text, languageId = 'sluice') {
      const model = new FakeModel(text, languageId)
      models.push(model)
      for (const fn of modelListeners) fn(model)
      return model
    },
    /** Ask the registered provider for completions, as the editor would. */
    complete(model, offset) {
      const position = model.getPositionAt(offset)
      return registered.completion[0].provider.provideCompletionItems(model, position)
    },
    hover(model, offset) {
      return registered.hover[0].provider.provideHover(model, model.getPositionAt(offset))
    },
  }
}

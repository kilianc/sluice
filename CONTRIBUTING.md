# Contributing

Sluice is two implementations of one language, kept honest by a corpus that
belongs to neither. Most of what follows is about that arrangement.

## Getting set up

You need Go 1.23 or newer. You do **not** need Node: the JavaScript package runs
in a pinned image built from [`tools/Dockerfile`](tools/Dockerfile), which is
also how the conformance runner drives the JS adapter on a host that has no
JavaScript runtime — or on one whose owner would rather not hand a repository to
one.

```bash
make test          # Go, then the JS package in the image
make test-go
make test-js
make conformance   # the corpus against every implementation that can run here
make race
```

If you do have Node installed, the runner uses it directly and the image is never
built. It checks by asking the adapter the cheapest possible question, because a
runtime can be on `PATH` and still refuse to run.

## The rule

**Every behaviour change lands with a corpus case in the same commit.**

This is the whole reason [`conformance/`](conformance/) is data rather than
tests. Two implementations that both "look right" drift within a release; two
implementations that both run `005-suggest.json` do not. A behaviour change
without a case is the bug re-introducing itself, and it will be asked for in
review.

Cases assert language facts, never implementation facts. If a case would fail on
a correct implementation in another language, it is wrong. See
[`conformance/README.md`](conformance/README.md) for the format and
[`docs/porting.md`](docs/porting.md) for the protocol.

## The spec

[`AGENTS.md`](AGENTS.md) is normative and is the artifact; the code is its
projection. A change to behaviour changes that file in the same pull request.

Where the spec and the Go implementation disagree, the implementation wins in
practice and is still a bug: fix the code and add the case, rather than amending
the spec to match what happens to be there. Where the spec is *silent* — as it was
about fractional durations and about case folding for `~` — say so explicitly,
decide, and pin the decision with a case.

## Code

- **No interpolation.** The only way a value reaches SQL is `Builder.Bind`. A
  `grep` for `Sprintf` in `dialect/` should turn up nothing, and a new one had
  better not be formatting a value.
- **No passthrough.** No branch may copy an unrecognized token into output.
  Unknown input is a diagnostic.
- Go: `gofmt`, no `unsafe`, no reflection-based SQL building. CI checks the
  first and reviewers check the rest.
- JS: zero runtime dependencies in `@sluice/core`, ESM, no build step, and no
  editor imports — bindings are separate packages so a headless client pays
  nothing for Monaco.
- Diagnostic **codes** are API and messages are not. Improve wording freely; a
  corpus case can never assert it.

Comments explain why something is the way it is, especially when it looks
excessive. The `ESCAPE '\'` clause and the unconditional parentheses both look
like overkill until you know what they prevent.

## Pull requests

Say what changed and what it breaks. If it changes emitted SQL for any existing
schema, say that in the first paragraph — it is the thing most likely to break a
caller quietly.

New dialects need their own corpus file and an entry in the adapter registry's
`dialects` list. New implementations need an adapter and one registry entry;
declaring only the ops you have implemented is fine, so a lexer-only pull request
is welcome.

## Versioning

The Go module and `@sluice/core` share a version and are cut from the same
commit, so that "Sluice 0.2.1" identifies one language rather than two.

Publishing can lag on one side — a registry outage, an account you cannot get
into. When it does, the package is published later **from the same tag and at
the same version** rather than skipping ahead to the next one. The number keeps
identifying a commit; only the moment it appeared in a registry moves. v0.1.0
went out this way: the Go module on the tag, npm afterwards.

While the version is `0.x`, **the minor bumps for a breaking change** and the
patch for everything else. Once it reaches `1.0`, ordinary semver.

Breaking means any of:

- a change to the SQL emitted for an existing schema, dialect and input — this is
  API, because callers have it in their own tests;
- a change to a diagnostic **code**, or to the span a code is reported at;
- a change to the AST wire format, which two versions may have to agree on across
  a network;
- a change to the adapter protocol;
- a removal or signature change in the exported API of either package;
- a schema that used to load and now does not.

Not breaking: diagnostic wording, new fields on a response that consumers may
ignore, new dialects, new suggestion kinds, performance, and anything behind a
new opt-in schema option.

## Releasing

Releases are cut by a maintainer, in this order. Steps 3 and 4 need not happen on
the same day, but they must describe the same commit:

```bash
# 1. everything green, including both adapters
make test && make conformance

# 2. version the JS package (the Go module takes its version from the tag)
#    js/packages/core/package.json → "version": "0.1.0"

# 3. tag and push
git tag -a v0.1.0 -m "v0.1.0"
git push origin main --follow-tags

# 4. publish the npm package from the pinned image, never from the host. npm
#    reads its token from a config file rather than the environment, so hand it
#    one that exists only for this command.
printf '//registry.npmjs.org/:_authToken=%s\n' "$NPM_TOKEN" > /tmp/sluice-npmrc
docker run --rm -u "$(id -u):$(id -g)" \
  -v "$PWD:/work" -v /tmp/sluice-npmrc:/tmp/npmrc:ro \
  -w /work/js/packages/core \
  -e NPM_CONFIG_USERCONFIG=/tmp/npmrc -e npm_config_cache=/tmp/npm-cache \
  sluice-tools npm publish --access public
rm /tmp/sluice-npmrc
```

`npm pack --dry-run` in the same image shows exactly what would be uploaded;
`--access public` is required the first time a scoped package is published.

pkg.go.dev needs nothing beyond the pushed tag; it indexes from the public
repository on first request for the version.

Step 3 is the irreversible one. proxy.golang.org caches a tag permanently the
first time anything fetches it, so a tag cannot be moved or taken back — check
`make test && make conformance` before it, not after. Nothing else in this list
has that property.

# The site

A landing page and a live demo, static and buildless. It loads Monaco and PGlite
from a pinned CDN and Sluice from this repository, so the demo always exercises
the code it ships beside rather than a published copy that may have drifted.

```bash
make site        # assemble and serve at http://localhost:8903/
make site-build  # assemble only, into .site/
```

"Assembling" is copying: the page, the `js/` packages it imports, and the
markdown it links to, side by side in one directory. That is the whole build,
and the Pages workflow runs the same target — so what you preview locally is
what deploys.

| file | |
|---|---|
| `index.html` | the page; all copy lives here |
| `style.css` | one accent, a lot of air, light and dark |
| `app.js` | the demo: schema, seed data, compile, execute, render |

The demo compiles for all four dialects and executes only the Postgres one,
because the database in the page *is* Postgres. Switching the dialect shows what
a dialect is allowed to change — placeholders, casts, the duration form, and
MySQL's `ORDER BY`, which is the one place a dialect changes more than spelling.

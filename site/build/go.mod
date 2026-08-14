// A separate module: the library depends on nothing outside the standard
// library, and rendering markdown honestly needs a real parser.
module github.com/kilianc/sluice/site/build

go 1.23

require github.com/yuin/goldmark v1.7.8

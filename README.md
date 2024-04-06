# `sturdyc`: a caching library for building sturdy systems
[![Go Reference](https://pkg.go.dev/badge/github.com/creativecreature/sturdyc.svg)](https://pkg.go.dev/github.com/creativecreature/sturdyc)
[![Go Report Card](https://goreportcard.com/badge/github.com/creativecreature/sturdyc)](https://goreportcard.com/report/github.com/creativecreature/sturdyc)
[![codecov](https://codecov.io/gh/creativecreature/sturdyc/graph/badge.svg?token=CYSKW3Z7E6)](https://codecov.io/gh/creativecreature/sturdyc)

`Sturdyc` offers all the features you would expect from a caching library,
along with additional functionality designed to help you build robust
applications, such as:
- **stampede protection**
- **permutated cache keys**
- **request deduplication**
- **refresh buffering**

```sh
go get github.com/creativecreature/sturdyc
```

# At a glance
The package exports the following functions:
- Use [`sturdyc.Set`](https://pkg.go.dev/github.com/creativecreature/sturdyc#Set) to write a record to the cache.
- Use [`sturdyc.Get`](https://pkg.go.dev/github.com/creativecreature/sturdyc#Get) to retrieve a record from the cache.
- Use [`sturdyc.GetFetch`](https://pkg.go.dev/github.com/creativecreature/sturdyc#GetFetch) to have the cache fetch and store a record if it doesn't exist.
- Use [`sturdyc.GetFetchBatch`](https://pkg.go.dev/github.com/creativecreature/sturdyc#GetFetchBatch) to have the cache fetch and store each record in a batch if any of them doesn't exist.

To utilize these functions, we will first have to set up a client to manage our
configuration:

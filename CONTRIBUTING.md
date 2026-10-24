# Contributing

Issues and pull requests are welcome.

- `go test -short ./...` must pass; `go test ./...` runs the end-to-end test with real Chrome (CI runs both).
- `gofmt` and `go vet` clean.
- Changes to what gets recorded or published need a test, ideally in `e2e_test.go`, that proves nothing private leaks.
- If BirdNET-Go's UI changed and broke something, include the BirdNET-Go version in the issue or PR.

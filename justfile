set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

default:
    just --list

test:
    go test ./...

test-unit:
    go test ./...

test-integration:
    go test ./...

vet:
    go vet ./...

fmt:
    gofmt -w $(git ls-files '*.go')

fmt-check:
    files="$(gofmt -l .)"; \
    test -z "$files" || { printf '%s\n' "$files"; exit 1; }

tidy:
    go mod tidy

tidy-check:
    go mod tidy -diff

check: fmt-check tidy-check vet test

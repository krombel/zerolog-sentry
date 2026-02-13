GO?=go

modules:
	@$(GO) mod tidy -v

test:
	@$(GO) test -v -race -cover

lint:
	golangci-lint run -v

benchmarks:
	@$(GO) test -bench=. -benchmem

coverage:
	@$(GO) test -race -covermode=atomic -coverprofile=cover.out

.PHONY: modules test lint benchmarks coverage

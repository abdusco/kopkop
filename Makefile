.PHONY: fmt vet test ci

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './tests/fixtures/*')

vet:
	go vet ./...

test:
	go test ./...

ci: vet test

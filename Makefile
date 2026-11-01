.PHONY: build test lint install publish clean

build:
	go build -o myskills .

test:
	go test -race ./...

lint:
	go vet ./...
	golangci-lint run

install:
	go install .

# publish replaces the rolling `latest` release with binaries built from HEAD.
publish:
	@test -z "$$(git status --porcelain)" || (echo "Commit your changes before publishing."; exit 1)
	goreleaser release --snapshot --clean
	git tag -f latest HEAD
	git push origin refs/tags/latest --force
	@commit="$$(git rev-parse --short HEAD)"; \
	if gh release view latest >/dev/null 2>&1; then \
		gh release upload latest dist/*.tar.gz dist/checksums.txt --clobber; \
		gh release edit latest --title "Latest" --notes "Built from commit $$commit." --latest; \
	else \
		gh release create latest dist/*.tar.gz dist/checksums.txt --title "Latest" --notes "Built from commit $$commit." --latest; \
	fi

clean:
	rm -f myskills
	rm -rf dist

.PHONY: build app test lint install release clean

build:
	go build -o myskills .

# app builds the desktop app for this machine's platform into dist/app.
app:
	go tool mygo build app

test:
	go test -race ./...

lint:
	go vet ./...
	go tool mygo vet ./app
	golangci-lint run

install:
	go install .

# release sets the app's version, commits it, tags it v$(VERSION) with the
# release notes (the file NOTES, or written in the editor) and pushes both;
# the Release workflow then builds and publishes the release. It releases
# origin/main, once its tests have passed.
release:
	@echo "$(VERSION)" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$$' || (echo "Usage: make release VERSION=x.y.z [NOTES=file]"; exit 1)
	@test "$$(git branch --show-current)" = main || (echo "Release from main."; exit 1)
	@test -z "$$(git status --porcelain)" || (echo "Commit your changes before releasing."; exit 1)
	@git fetch -q --tags origin
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || (echo "HEAD is not origin/main."; exit 1)
	@! git rev-parse -q --verify "refs/tags/v$(VERSION)" >/dev/null || (echo "v$(VERSION) exists."; exit 1)
	@test "$$(gh run list --workflow test.yml --commit "$$(git rev-parse HEAD)" --limit 1 --json conclusion --jq '.[0].conclusion')" = success || (echo "The tests have not passed on HEAD."; exit 1)
	perl -pi -e 's/"version": "[^"]*"/"version": "$(VERSION)"/' app/mygo.json
	git commit -qm "Release v$(VERSION)" app/mygo.json
	git tag -a "v$(VERSION)" $(if $(NOTES),--cleanup=whitespace -F "$(NOTES)")
	git push --atomic origin main "v$(VERSION)"

clean:
	rm -f myskills
	rm -rf dist

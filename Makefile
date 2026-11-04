.PHONY: build app test lint install publish clean

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

ASSETS = dist/*.tar.gz dist/*.dmg dist/*.deb dist/MySkills-install.sh dist/checksums.txt

# publish replaces the rolling `latest` release with the CLI and the desktop
# app built from HEAD. The app's files get names without its version.
publish:
	@test -z "$$(git status --porcelain)" || (echo "Commit your changes before publishing."; exit 1)
	@git fetch -q origin && git merge-base --is-ancestor HEAD origin/main || (echo "Push HEAD to origin/main before publishing."; exit 1)
	goreleaser release --snapshot --clean
	go tool mygo build -platform darwin/universal,linux/amd64,linux/arm64 app
	cp dist/app/darwin-universal/*.dmg dist/MySkills.dmg
	for arch in amd64 arm64; do \
		cp dist/app/linux-$$arch/*.tar.gz dist/MySkills-linux-$$arch.tar.gz; \
		cp dist/app/linux-$$arch/*.deb dist/MySkills-linux-$$arch.deb; \
	done
	cp dist/app/linux-amd64/install.sh dist/MySkills-install.sh
	cd dist && shasum -a 256 *.tar.gz *.dmg *.deb MySkills-install.sh > checksums.txt
	git tag -f latest HEAD
	git push origin refs/tags/latest --force
	@commit="$$(git rev-parse --short HEAD)"; \
	if gh release view latest >/dev/null 2>&1; then \
		gh release upload latest $(ASSETS) --clobber; \
		gh release edit latest --title "Latest" --notes "Built from commit $$commit." --latest; \
	else \
		gh release create latest $(ASSETS) --title "Latest" --notes "Built from commit $$commit." --latest; \
	fi

clean:
	rm -f myskills
	rm -rf dist

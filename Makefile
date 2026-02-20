.PHONY: release-check release-dry-run release

release-check:
	./scripts/release-check.sh "$(VERSION)"

release-dry-run:
	./scripts/release.sh --dry-run "$(VERSION)"

release:
	./scripts/release.sh "$(VERSION)"

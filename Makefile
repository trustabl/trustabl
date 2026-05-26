# Trustabl release Makefile.
# Wraps GoReleaser + git-tag plumbing. Local-only; CI uses GoReleaser directly.

VERSION ?=

.PHONY: help
help:
	@echo "Release targets:"
	@echo "  make release-check                    Validate .goreleaser.yaml"
	@echo "  make release-snapshot                 Local dry-run; produces dist/ without publishing"
	@echo "  make release-clean                    Remove dist/"
	@echo "  make tag VERSION=v0.1.0               Create annotated release tag (does NOT push)"
	@echo "  make tag-rc VERSION=v0.1.0-rc.1       Create pre-release tag (does NOT push)"
	@echo "  make tag-push                         Push the most recent tag to origin"
	@echo "  make tag-delete-local VERSION=v0.1.0  Delete a tag locally only"

# `goreleaser check` exits non-zero on ANY deprecation. We intentionally keep
# the deprecated `brews:` block (homebrew_casks is macOS-only and would drop
# Linux brew support) until GoReleaser v3 forces the move. `build`/`release`
# only warn on it, so the release pipeline is unaffected — but plain `check`
# would fail. This target passes when the config is structurally valid even if
# the only problem is a deprecation, and still fails on real schema errors.
.PHONY: release-check
release-check:
	@out=$$(goreleaser check 2>&1); status=$$?; echo "$$out"; \
	 if [ $$status -eq 0 ]; then exit 0; fi; \
	 if echo "$$out" | grep -q "configuration is valid"; then \
	   echo ">> config is structurally valid; only deprecation warnings (brews kept for Linux on purpose). Release is unaffected."; \
	   exit 0; \
	 fi; \
	 echo ">> goreleaser check found real configuration errors (see above)."; exit 2

.PHONY: release-snapshot
release-snapshot:
	goreleaser release --snapshot --clean --skip=publish

.PHONY: release-clean
release-clean:
	rm -rf dist/

.PHONY: tag
tag: _require-version _require-clean-tree _validate-version _require-tag-absent
	@git tag -a -m "Release $(VERSION)" $(VERSION)
	@echo "Created tag $(VERSION). Push it with:  make tag-push"

.PHONY: tag-rc
tag-rc: _require-version _require-clean-tree _validate-version-rc _require-tag-absent
	@git tag -a -m "Pre-release $(VERSION)" $(VERSION)
	@echo "Created pre-release tag $(VERSION). Push it with:  make tag-push"

.PHONY: tag-push
tag-push:
	@latest=$$(git describe --tags --abbrev=0); \
	 echo "Pushing $$latest to origin"; \
	 git push origin $$latest

.PHONY: tag-delete-local
tag-delete-local: _require-version
	@git tag -d $(VERSION)

# --- guards (private) ---

_require-version:
	@[ -n "$(VERSION)" ] || { echo "ERROR: VERSION is required, e.g. 'make tag VERSION=v0.1.0'"; exit 2; }

_require-clean-tree:
	@git diff --quiet --exit-code || { echo "ERROR: working tree has uncommitted changes"; exit 2; }
	@git diff --cached --quiet --exit-code || { echo "ERROR: index has staged changes"; exit 2; }

_require-tag-absent:
	@! git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null || { echo "ERROR: tag $(VERSION) already exists locally"; exit 2; }

_validate-version:
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+$$' || { echo "ERROR: VERSION must match vX.Y.Z, got '$(VERSION)'"; exit 2; }

_validate-version-rc:
	@echo "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+-(rc|alpha|beta)\.[0-9]+$$' || { echo "ERROR: VERSION must match vX.Y.Z-(rc|alpha|beta).N, got '$(VERSION)'"; exit 2; }

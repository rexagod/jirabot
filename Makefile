# Variables are declared in the order in which they occur.
ASSETS_DIR ?=
GO ?= $(shell which go)
MARKDOWNFMT_VERSION ?= v3.1.0
GOLANGCI_LINT_VERSION ?= v1.54.2
ifeq ($(ASSETS_DIR),)
    MD_FILES = $(shell find . \( -type d -name '.vale' \) -prune -o -type f -name "*.md" -print)
else
    MD_FILES = $(shell find . \( -type d -name '.vale' -o -type d -name $(patsubst %/,%,$(patsubst ./%,%,$(ASSETS_DIR))) \) -prune -o -type f -name "*.md" -print)
endif
GO_FILES = $(shell find . -type f -name "*.go")
OS ?= $(shell uname -s | tr '[:upper:]' '[:lower:]')
ARCH ?= $(shell $(GO) env GOARCH)

all: lint jirabot

.PHONY: setup-dependencies
setup-dependencies:
	@# Setup vale. The lazy check prevents querying the API if the binary is already present, since raw GitHub URLs rarely work on my ISP.
	@if [ ! -f "./assets/vale" ]; then \
		wget https://github.com/errata-ai/vale/releases/download/v$(VALE_VERSION)/vale_$(VALE_VERSION)_Linux_64-bit.tar.gz && \
		mkdir -p $(ASSETS_DIR) && tar -xvzf vale_$(VALE_VERSION)_Linux_64-bit.tar.gz -C assets && \
		chmod +x $(ASSETS_DIR)vale; \
	fi; \
	$(ASSETS_DIR)vale sync
	@# Setup markdownfmt.
	@GOOS=$(OS) GOARCH=$(ARCH) $(GO) install github.com/Kunde21/markdownfmt/v3/cmd/markdownfmt@$(MARKDOWNFMT_VERSION)
	@# Setup golangci-lint.
	@GOOS=$(OS) GOARCH=$(ARCH) $(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

.PHONY: clean
clean:
	@git clean -fxd

.make/vale: .vale.ini $(wildcard .vale/*) $(MD_FILES)
	@$(ASSETS_DIR)vale $(MD_FILES)
	@if [ $$? -eq 0 ]; then touch $@; fi

.make/markdownfmt: $(MD_FILES)
	@test -z "$(shell markdownfmt -l $(MD_FILES))" || (echo "\033[0;31mThe following files need to be formatted with 'markdownfmt -w -gofmt':" $(shell markdownfmt -l $(MD_FILES)) "\033[0m" && exit 1)
	@touch $@

.PHONY: lint-md
lint-md: .make/vale .make/markdownfmt

.make/gofmt: $(GO_FILES)
	@test -z "$(shell gofmt -l $(GO_FILES))" || (echo "\033[0;31mThe following files need to be formatted with 'gofmt -w':" $(shell gofmt -l $(GO_FILES)) "\033[0m" && exit 1)
	@if [ $$? -eq 0 ]; then touch $@; fi

.make/golangci-lint: $(GO_FILES)
	@golangci-lint run
	@if [ $$? -eq 0 ]; then touch $@; fi

.PHONY: lint-go
lint-go: .make/gofmt .make/golangci-lint

.PHONY: lint
lint: lint-md lint-go

.make/markdownfmt-fix: $(MD_FILES)
	@for file in $(MD_FILES); do markdownfmt -w -gofmt $$file || exit 1; done
	@touch $@

.PHONY: lint-md-fix
lint-md-fix: .make/vale .make/markdownfmt-fix

.make/gofmt-fix: $(GO_FILES)
	@gofmt -w . || exit 1
	@touch $@

.PHONY: lint-go-fix
lint-go-fix: .make/gofmt-fix .make/golangci-lint

.PHONY: lint-fix
lint-fix: lint-md-fix lint-go-fix

jirabot: $(GO_FILES)
	@cd cmd && GOOS=$(OS) GOARCH=$(ARCH) $(GO) build -o ../$@ && cd -

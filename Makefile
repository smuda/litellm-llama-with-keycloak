# Flags ride in SHELL: macOS ships make 3.81, which has no .SHELLFLAGS.
SHELL := bash -euo pipefail

MODULES := key-broker login-helper

.PHONY: build lint lint-md test hooks e2e llama-playground llama-production $(MODULES)

build: $(MODULES)

$(MODULES):
	cd $@ && go build -o bin/$@ ./cmd/$@

lint: lint-md
	@for m in $(MODULES); do (cd $$m && golangci-lint run); done

# Only staged additions are checked, so untouched long lines pass.
# Lines before the first @@ of each file are headers, which is how an
# added line starting with "++" is told apart from a "+++" header.
# perl -CSD counts characters; macOS awk counts bytes.
lint-md:
	@git -c core.quotePath=false diff --cached --no-color \
	  --no-ext-diff --no-textconv --src-prefix=a/ --dst-prefix=b/ -U0 -- '*.md' | \
	  perl -CSD -ne \
	  '$$hdr = 1 if /^diff /; $$hdr = 0 if /^@@/; s/\r?\n\z//; \
	   $$file = $$1 if $$hdr && m{^\+\+\+ b/(.*?)\t?$$}; next if $$hdr || !s/^\+//; \
	   if (length > 72) { print "$$file: $$_\n"; $$bad = 1 } \
	   END { exit $$bad }'

test:
	@./scripts/lint-md-test.sh
	@for m in $(MODULES); do (cd $$m && go test ./...); done

hooks:
	git config core.hooksPath .githooks

e2e:
	docker compose -f docker-compose.yml up --build -d --wait
	./e2e/proxy.sh

# -c comes from limit.context in the opencode config the broker serves,
# so opencode compacts before llama-server refuses the request.
CTX = jq -er .config.provider.litellm.models.medium.limit.context
LLAMA = llama-server --alias qwen3.8-27b --host 127.0.0.1 --port 9931

llama-playground:
	ctx=$$($(CTX) key-broker/config/opencode.json) && \
	$(LLAMA) -c $$ctx -np 1 -fa on -ctk q8_0 -ctv q8_0 \
	  -hf unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF:IQ4_XS \
	  --temp 0.7 --top-p 0.8 --top-k 20 --repeat-penalty 1.05

llama-production:
	ctx=$$($(CTX) key-broker/config/opencode.production.json) && \
	$(LLAMA) -c $$ctx -hf ggml-org/Qwen3.8-27B-GGUF:Q8_0

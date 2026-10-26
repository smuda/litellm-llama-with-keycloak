MODULES := key-broker login-helper

.PHONY: build e2e llama-playground llama-production $(MODULES)

build: $(MODULES)

$(MODULES):
	cd $@ && go build -o bin/$@ .

e2e:
	./e2e/proxy.sh

# -c comes from limit.context in the opencode config the broker serves,
# so opencode compacts before llama-server refuses the request.
CTX = jq -er .config.provider.litellm.models.medium.limit.context
LLAMA = llama-server --alias qwen3.8-27b --host 127.0.0.1 --port 9931

llama-playground:
	ctx=$$($(CTX) key-broker/opencode.json) && \
	$(LLAMA) -c $$ctx -np 1 -fa on -ctk q8_0 -ctv q8_0 \
	  -hf unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF:IQ4_XS \
	  --temp 0.7 --top-p 0.8 --top-k 20 --repeat-penalty 1.05

llama-production:
	ctx=$$($(CTX) key-broker/opencode.production.json) && \
	$(LLAMA) -c $$ctx -hf ggml-org/Qwen3.8-27B-GGUF:Q8_0

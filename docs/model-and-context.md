# Playground model and context choice

The playground serves Qwen3-Coder-30B-A3B-Instruct as
`unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF:Q4_K_M` with a 32k context
and one slot. Production serves Qwen3.8-27B.
opencode reaches either under the LiteLLM model name `medium`.

| Setting           | 32 GB Mac (playground) | 512 GB machine (production) |
| ----------------- | ---------------------- | --------------------------- |
| Model             | Qwen3-Coder-30B-A3B    | Qwen3.8-27B                 |
| Quantisation      | Q4_K_M                 | Q8_0                        |
| Context (`-c`)    | 32768                  | 262144 (model maximum)      |
| Slots (`-np`)     | 1                      | to decide                   |
| Metal wired limit | default is enough      | default is enough           |
| LiteLLM name      | `medium`               | `medium`                    |

The playground exists to test the auth path: Keycloak login, the
virtual key, LiteLLM and spend. Any model that answers and makes tool
calls is enough for that. Qwen3-Coder-30B-A3B is chosen so the
playground can also take simple coding tasks: it is a
mixture-of-experts model with about 3B active parameters, so it runs
fast on the Mac, and it is tuned for agentic tool use. The general
Qwen3-30B-A3B gave up on a multi-step edit and told the user what to
do instead.

## Model and name

opencode asks for `medium`. LiteLLM maps `medium` to whatever
llama-server currently serves, so swapping the model never touches
opencode config. Both machines run llama-server with
`--alias qwen3.8-27b` so `litellm/config.yaml` stays the same; on the
playground the alias does not name the model it serves.

- The production model is
  [Qwen/Qwen3.8-27B](https://huggingface.co/Qwen/Qwen3.8-27B): a dense
  27B vision-language model with a 262k context window.
- Qwen publishes no GGUF of its own. The
  [ggml-org build](https://huggingface.co/ggml-org/Qwen3.8-27B-GGUF)
  comes from the llama.cpp maintainers. unsloth and bartowski builds
  also exist.
- llama.cpp supports its `qwen35` architecture, and its Qwen3-Coder
  XML tool-call parser matches the chat template, so opencode tool
  calls work.
- Thinking is on by default on Qwen3.8-27B and returns as
  `reasoning_content`. `--reasoning off` disables it.
  Qwen3-Coder-30B-A3B-Instruct does not think.

## Quantisation

Use Q8_0 on the 512 GB production machine.

| Quant  | Approx. size | Quality vs BF16        | Fits 512 GB machine |
| ------ | ------------ | ---------------------- | ------------------- |
| Q4_K_M | 19.0 GB      | small, noticeable loss | yes                 |
| Q6_K   | ~25 GB       | close to lossless      | yes                 |
| Q8_0   | ~32 GB       | practically lossless   | yes, recommended    |
| BF16   | ~60 GB       | reference              | yes                 |

Only the Q4_K_M size is read from the repo. The others are estimated
from bits per weight, so check the
[ggml-org file list](https://huggingface.co/ggml-org/Qwen3.8-27B-GGUF)
for the exact files.

Memory stops being the constraint at 512 GB. Pick Q8_0 over BF16: the
quality gap is negligible, and token generation is bound by memory
bandwidth, so the roughly half-size Q8_0 generates noticeably faster.
Pick BF16 only as a reference to measure the quantised builds
against.

## Context size and slots

- llama-server shares `-c` across all its slots as one KV cache. With
  the default of 4 slots, opencode's parallel requests (main prompt,
  title, retries) overflowed 32k and failed with "Context size has
  been exceeded". `-np 1` gives one request the whole context and
  queues the rest.
- `limit.context` in the served opencode config must match `-c`, or
  opencode does not compact before the server refuses. The Makefile
  targets read `-c` from it.
- opencode's system prompt and tool definitions use roughly 10-15k
  tokens before the first message, so 32k is tight but enough to test
  the auth path.
- On the 512 GB machine the default wired limit covers Q8_0 plus the
  full 262k context. With several users, decide how many parallel
  slots (`-np`) llama-server runs, keeping in mind that they share
  `-c`.

## Commands and config

The Makefile starts llama-server with the flags for each machine.
It takes `-c` from `limit.context` in the opencode config the broker
serves, so the two cannot drift. Change the context there, not on the
command line.

Playground (32 GB Mac), reading `key-broker/opencode.json`:

```sh
curl -L -C - -o ~/Library/Caches/llama.cpp/Qwen3-Coder-30B-A3B-Instruct-Q4_K_M.gguf \
  https://huggingface.co/unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF/resolve/main/Qwen3-Coder-30B-A3B-Instruct-Q4_K_M.gguf
make llama-playground
```

With Homebrew llama.cpp 0.6.0 (build 11429), `-hf` fails with
`get_repo_commit: HTTPLIB failed: SSL connection failed` even though
curl reaches huggingface.co, so the playground target loads the file
downloaded above with `-m`.

The sampling flags are Qwen's recommended settings for Qwen3-Coder.
The model's native context is 256k, so it needs no YaRN flags.

At `-c 65536` with the default f16 KV cache, llama-server wired about
28.9 GB of the 32 GB Mac. With Docker running, the rest went to swap,
prompt processing fell from 385 to about 30 tokens/s, and opencode
dropped requests after about 6 minutes without a response. A q8_0 KV
cache at `-c 32768` wires about 24.2 GB and keeps memory pressure
green.

Production (512 GB machine), reading
`key-broker/opencode.production.json`:

```sh
make llama-production
```

LiteLLM `model_list`, the same on both machines:

```yaml
model_list:
  - model_name: medium
    litellm_params:
      model: hosted_vllm/qwen3.8-27b
      api_base: http://host.docker.internal:9931/v1
```

## To verify

On the playground:

- [x] opencode handles streamed tool-call deltas through LiteLLM.
- [x] llama-server returns thinking as `reasoning_content`, separate
      from `content`.
- [x] opencode shows `reasoning_content` as reasoning, not as raw text
      in the answer.
- [x] A `127.0.0.1` bind on the host is reachable from Docker Desktop
      via `host.docker.internal`.

On the production machine:

- [ ] The exact Q8_0 file and size in the ggml-org repo.
- [ ] Qwen3.8-27B works for real coding sessions in opencode.

Source for the llama.cpp and LiteLLM facts:
[How does llama-server serve models, and what LiteLLM model_list entry reaches it?](https://github.com/smuda/litellm-llama-with-keycloak/issues/7)

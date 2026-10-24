# Playground model and context choice

The playground serves Qwen3.8-27B as
`ggml-org/Qwen3.8-27B-GGUF:Q4_K_M` with a 64k context, and opencode
reaches it under the LiteLLM model name `medium`.

| Setting           | 32 GB Mac (playground) | 512 GB machine (work)  |
| ----------------- | ---------------------- | ---------------------- |
| Model             | Qwen3.8-27B            | Qwen3.8-27B            |
| Quantisation      | Q4_K_M (19.0 GB)       | Q8_0                   |
| Context (`-c`)    | 65536                  | 262144 (model maximum) |
| Metal wired limit | raised to about 26 GB  | default is enough      |
| LiteLLM name      | `medium`               | `medium`               |

## Model and name

opencode asks for `medium`. LiteLLM maps `medium` to whatever
llama-server currently serves, so swapping the model never touches
opencode config.

- The model is
  [Qwen/Qwen3.8-27B](https://huggingface.co/Qwen/Qwen3.8-27B): a dense
  27B vision-language model with a 262k context window.
- Qwen publishes no GGUF of its own. The
  [ggml-org build](https://huggingface.co/ggml-org/Qwen3.8-27B-GGUF)
  comes from the llama.cpp maintainers. unsloth and bartowski builds
  also exist.
- llama.cpp supports its `qwen35` architecture, and its Qwen3-Coder
  XML tool-call parser matches the chat template, so opencode tool
  calls work.
- Thinking is on by default and returns as `reasoning_content`.
  `--reasoning off` disables it.

## Quantisation

Use Q4_K_M on the 32 GB Mac and Q8_0 on the 512 GB work machine.

| Quant  | Approx. size | Quality vs BF16        | Fits 32 GB Mac        | Fits 512 GB machine |
| ------ | ------------ | ---------------------- | --------------------- | ------------------- |
| Q4_K_M | 19.0 GB      | small, noticeable loss | yes, tight            | yes                 |
| Q6_K   | ~25 GB       | close to lossless      | no room for KV/Docker | yes                 |
| Q8_0   | ~32 GB       | practically lossless   | no                    | yes, recommended    |
| BF16   | ~60 GB       | reference              | no                    | yes                 |

Only the Q4_K_M size is read from the repo. The others are estimated
from bits per weight, so check the
[ggml-org file list](https://huggingface.co/ggml-org/Qwen3.8-27B-GGUF)
for the exact files.

On the 32 GB Mac, Q4_K_M is the largest quant that leaves room for
the KV cache, macOS and Docker running Keycloak, LiteLLM and
Postgres.

On the 512 GB machine, memory stops being the constraint. Pick Q8_0
over BF16: the quality gap is negligible, and token generation is
bound by memory bandwidth, so the roughly half-size Q8_0 generates
noticeably faster. Pick BF16 only as a reference to measure the
quantised builds against.

## Context size and memory

On the 32 GB Mac, run `-c 65536` and raise the Metal wired limit to
about 26 GB. On the 512 GB machine, run the full 262144.

- opencode's system prompt and tool definitions use roughly 10-15k
  tokens before the first message, so 32k fills fast in a coding
  session. 64k gives real working room.
- The `qwen35` architecture is hybrid: most layers keep no KV cache,
  so the cache is far smaller than for a classic 27B model. It still
  grows with `-c`.
- By default macOS lets Metal wire only about two thirds of RAM on a
  32 GB Mac (approximate, from memory; verify in the build). That is
  about 21 GB, barely above the 19 GB of weights.
- Raise it with `sudo sysctl iogpu.wired_limit_mb=26624`. The setting
  resets at reboot, so it is part of the startup steps.
- If 64k still does not fit, drop to `-c 32768`. Nothing else depends
  on this number.

On the 512 GB machine the default wired limit already covers Q8_0
plus the full 262k context, so no sysctl is needed. With several
users at work, also decide how many parallel slots (`-np`)
llama-server runs, and check how the build shares `-c` across them.

## Commands and config

Playground (32 GB Mac):

```sh
sudo sysctl iogpu.wired_limit_mb=26624
llama-server -hf ggml-org/Qwen3.8-27B-GGUF:Q4_K_M --alias qwen3.8-27b \
  -c 65536 --host 127.0.0.1 --port 9931
```

Work (512 GB machine):

```sh
llama-server -hf ggml-org/Qwen3.8-27B-GGUF:Q8_0 --alias qwen3.8-27b \
  -c 262144 --host 127.0.0.1 --port 9931
```

LiteLLM `model_list`, the same on both machines:

```yaml
model_list:
  - model_name: medium
    litellm_params:
      model: hosted_vllm/qwen3.8-27b
      api_base: http://host.docker.internal:9931/v1
```

## To verify in the build

None of this has been run end to end yet.

- [ ] Q4_K_M plus a 64k context fits under a 26 GB wired limit
      alongside Docker.
- [ ] The default Metal wired limit on the 32 GB Mac is really about
      21 GB.
- [ ] The exact Q8_0 file and size in the ggml-org repo.
- [ ] opencode handles streamed tool-call deltas and
      `reasoning_content` from this model.
- [ ] A `127.0.0.1` bind on the host is reachable from Docker Desktop
      via `host.docker.internal`.

Source for the llama.cpp and LiteLLM facts:
[How does llama-server serve models, and what LiteLLM model_list entry reaches it?](https://github.com/smuda/litellm-llama-with-keycloak/issues/7)

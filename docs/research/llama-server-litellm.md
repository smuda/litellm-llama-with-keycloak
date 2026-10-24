# llama-server behind LiteLLM

Research for issue #7: how llama.cpp `llama-server` serves models,
and what LiteLLM `model_list` entry reaches it from inside Docker via
`host.docker.internal`. Also covers the requested Qwen 3.8 27B model.

Researched 2026-10-24 against llama.cpp `master` at commit
`8e5a58df87ab59dfceb5b961ae26691e39f30c24` and LiteLLM tag `v1.106.2`.

## Answer

- Run one `llama-server` process with one model (`-hf` or `-m`) and
  give it a stable `--alias`. Router mode exists for several models
  in one process, but one model is enough here.
- In LiteLLM use the `hosted_vllm/` prefix (or `openai/`). LiteLLM
  1.106.2 has no native llama.cpp provider.
- `api_base: http://host.docker.internal:<port>/v1`.
- No `api_key` is needed with `hosted_vllm/`: LiteLLM sends
  `fake-api-key` when none is set. With `openai/` a dummy key is
  needed. If `llama-server` runs with `--api-key`, put that key in
  `api_key`.
- Streaming and OpenAI-style tool calls are supported on both sides.
  On the llama.cpp side tool calls need `--jinja`, which is on by
  default in current builds.
- The name "Qwen 3.8 27B" matches a real model: `Qwen/Qwen3.8-27B`.
  Use the GGUF build `ggml-org/Qwen3.8-27B-GGUF:Q4_K_M` (19.0 GB).

## Example

llama-server on the macOS host:

```sh
llama-server \
  -hf ggml-org/Qwen3.8-27B-GGUF:Q4_K_M \
  --alias qwen3.8-27b \
  -c 65536 \
  --host 127.0.0.1 --port 9931
```

LiteLLM `config.yaml`:

```yaml
model_list:
  - model_name: qwen3.8-27b
    litellm_params:
      model: hosted_vllm/qwen3.8-27b
      api_base: http://host.docker.internal:9931/v1
```

opencode then asks LiteLLM for `qwen3.8-27b`.

`-c 65536` keeps the KV cache small enough for a Mac. Without `-c`
the server takes the context size from the model, which is 262,144
tokens for this model. See "Mac fit" below.

## llama-server: one model or many

Single-model mode, the classic form: start with `-m FNAME` or
`-hf <user>/<model>[:quant]`. `-hf` downloads from Hugging Face and
also fetches the `mmproj` file when one exists. The `id` in
`/v1/models` is the model path unless `--alias` sets it ([README][srv]
lines 103, 190, 1373). In single-model mode the request's `model`
field is ignored ([README][srv] line 1577).

Router mode: start `llama-server` with no model. It finds models in
the Hugging Face cache (`LLAMA_CACHE`), in `--models-dir`, or in an
INI file from `--models-preset`. It loads a model on demand, picked by
the `model` field of the JSON body. `--models-max` caps how many are
loaded at once (default 4) and `--no-models-autoload` turns off
on-demand loading. Each model runs as a child instance that inherits
the router's arguments. The router controls host, port, API key, HF
repo and alias ([README][srv] "Using multiple models", lines
1966-2147; flags at lines 230-233).

`--sleep-idle-seconds` unloads the model after a period with no
requests and reloads it on the next one. It works in both modes
([README][srv] lines 247, 2456-2460).

Recommendation: single-model mode. It gives one stable name, and the
`model` value LiteLLM sends does not matter. Router mode is worth it
only if several models should share one port. In that case each
LiteLLM entry's model name after the prefix must match the router's
model id exactly, for example `ggml-org/Qwen3.8-27B-GGUF:Q4_K_M`, or
the name set by a preset section.

## llama-server: endpoints, defaults, auth

| Item              | Value                                          | Source                    |
| ----------------- | ---------------------------------------------- | ------------------------- |
| Chat completions  | `/v1/chat/completions` and `/chat/completions` | [server.cpp][cpp] 290-291 |
| Model list        | `/v1/models` and `/models`                     | [server.cpp][cpp] 285-286 |
| Health (no auth)  | `/health`                                      | [server.cpp][cpp] 280     |
| Default `--host`  | `127.0.0.1`                                    | [README][srv] 193         |
| Default `--port`  | `9931` (not 8080 as in older docs)             | [README][srv] 194         |
| `--api-key`       | off by default; comma-separated list           | [README][srv] 215         |
| `--jinja`         | on by default                                  | [README][srv] 234         |
| `-np, --parallel` | `-1` = auto                                    | [README][srv] flag table  |

Streaming: `/v1/chat/completions` supports both synchronous and
streaming (SSE) responses ([README][srv] section `POST
/v1/chat/completions`).

Tool calls: "OpenAI-style function calling is supported with the
`--jinja` flag" ([README][srv] line 1513; [function-calling.md][fc]).
Parallel tool calls are off by default and are turned on per request
with `"parallel_tool_calls": true` ([function-calling.md][fc] line 25).

Reasoning output: `--reasoning-format` defaults to `auto`. Format
`deepseek` puts thoughts in `message.reasoning_content`, and `none`
leaves them in `content`. `--reasoning on|off|auto` and
`--reasoning-effort` are server flags. Per request, the body can carry
`reasoning_effort` and `chat_template_kwargs` (for example
`{"enable_thinking": false}`) ([README][srv] lines 219, 235-240,
1444-1448).

## LiteLLM side

Provider choice. LiteLLM 1.106.2's provider enum has `hosted_vllm`,
`llamafile` and `openai_like` (embeddings only), but no llama.cpp
entry ([types/utils.py][lt-types] lines 4139-4232). All three of the
options below are subclasses of the OpenAI chat config:

| Prefix         | api_base default            | api_key when unset                               | Source                            |
| -------------- | --------------------------- | ------------------------------------------------ | --------------------------------- |
| `hosted_vllm/` | none (must set)             | `fake-api-key`                                   | [hosted_vllm][lt-vllm] 108-113    |
| `llamafile/`   | `http://127.0.0.1:8080/v1`  | `fake-api-key`                                   | [llamafile][lt-lf]                |
| `openai/`      | `https://api.openai.com/v1` | `OPENAI_API_KEY`; the docs say a key is required | [main.py][lt-main], [docs][lt-oc] |

The LiteLLM docs recommend `hosted_vllm` or `llamafile` over
`openai/` so that a fake key is not needed ([docs][lt-oc]).
`hosted_vllm` also cleans tool schemas: it removes
`additionalProperties` and `strict`, and turns `custom` tools into
`function` tools. It adds `reasoning_effort` and `thinking` to the
supported params ([hosted_vllm][lt-vllm] lines 31-96).

`api_base` form. The URL is `api_base` with trailing `/` removed,
plus `/chat/completions` (`get_complete_url`,
[gpt_transformation.py][lt-gpt] lines 734-759). So `.../v1` hits `/v1/chat/completions`, and a base
without `/v1` hits `/chat/completions`. llama-server serves both
paths. Use `/v1` to follow the LiteLLM docs' advice ("make sure your
`api_base` has the `/v1` postfix", [docs][lt-oc]).

Model name. LiteLLM removes the provider prefix and sends the rest as
`model`. In single-model mode llama-server ignores it. In router mode
it picks the model.

Streaming and tools. The OpenAI chat config lists `stream`,
`stream_options`, `tools`, `tool_choice` and `parallel_tool_calls` as
supported params ([gpt_transformation.py][lt-gpt] lines 163-173), so
LiteLLM forwards them.

## Qwen 3.8 27B

Name check. The Qwen org on Hugging Face has `Qwen/Qwen3.8-27B`
(created 2026-08-05) and `Qwen/Qwen3.8-27B-FP8`. "Qwen 3.8 27B" is
that model; only the spacing differs ([HF API][hf-search]). It is a
dense vision-language model (`image-text-to-text`), Apache-2.0, with
262,144 tokens of native context ([model card][qwen]).

GGUF builds that fit a Mac:

| Repo                                  | Kind                   | Picks (file size)                                             |
| ------------------------------------- | ---------------------- | ------------------------------------------------------------- |
| `ggml-org/Qwen3.8-27B-GGUF`           | llama.cpp org, auto    | Q4_K_M 19.0 GB, Q8_0 28.6 GB, BF16 53.8 GB, mmproj, MTP draft |
| `unsloth/Qwen3.8-27B-GGUF`            | well known, most pulls | UD-IQ3_XXS 10.9 GB to UD-Q8_K_XL 31.5 GB; UD-Q4_K_XL 17.6 GB  |
| `bartowski/Qwen3.8-27B-GGUF`          | well known, imatrix    | IQ2_XXS 8.9 GB to Q8_0 29.1 GB; Q4_K_M 17.4 GB                |
| `lmstudio-community/Qwen3.8-27B-GGUF` | LM Studio              | not inspected                                                 |

Qwen itself publishes no GGUF. Sizes come from the HF tree API
([ggml-org][hf-ggml], [unsloth][hf-unsloth], [bartowski][hf-bart]).
Many other repos are uncensored or merged variants; avoid them for
this playground.

Mac fit (estimate, not measured). On Apple Silicon the weights live
in unified memory, so add the KV cache and the OS to the file size.
Q4_K_M (17-19 GB) is comfortable with 32 GB RAM and tight with 24 GB.
Q8_0 (about 29 GB) needs 48 GB or more. Set `-c`: by default `-c 0`
loads the model's full 262,144-token context, and that KV cache adds a
lot of memory.

Template and tool calls. The `ggml-org` GGUF reports architecture
`qwen35`, and its embedded chat template is byte-identical to
`Qwen/Qwen3.8-27B`'s `chat_template.jinja` (checked through the HF
API). llama.cpp knows `qwen35` ([llama-arch.cpp][arch] line 43). The
template uses `<tool_call><function=...><parameter=...>` XML tool
calls. llama.cpp's chat code detects that form and uses its
"Qwen3-Coder" handler, which its comment says "also used by ...
Qwen3.5" ([chat.cpp][chat] lines 1245-1252). The template has none of
the excluded no-newline form, so the handler applies. Both need
`--jinja`, which is on by default.

Thinking. Qwen3.8 thinks by default (`<think>...</think>`). It
supports `reasoning_effort` (`xhigh` by default, also `medium` and
`low`) and `preserve_thinking` (on by default). Thinking is turned off
per request with `chat_template_kwargs: {"enable_thinking": false}`
([model card][qwen] lines 28, 246-300, 459). llama-server's default
`--reasoning-format auto` should return the thoughts as
`reasoning_content`. To switch thinking off for everything, start the
server with `--reasoning off`, or with
`--chat-template-kwargs '{"enable_thinking":false}'`. Qwen's
recommended sampling for thinking mode is `temperature=1.0`,
`top_p=0.95`, `top_k=20` (model card line 252).

## Uncertain / verify in the build

- Not run. Nothing here was tested against a live llama-server or
  LiteLLM.
- Whether llama-server streams tool-call deltas incrementally, and how
  opencode handles `reasoning_content` that comes through LiteLLM, is
  not stated in the docs read. Test it with one streamed tool call
  through the proxy.
- Whether LiteLLM's proxy forwards `chat_template_kwargs` in a client
  request body unchanged was not traced in the source. A server-side
  flag (`--reasoning off`) avoids depending on it.
- Docker Desktop on macOS normally routes `host.docker.internal` to
  the host's loopback, so `--host 127.0.0.1` should be enough. If the
  connection is refused, bind `0.0.0.0` and set `--api-key`, because
  that exposes the port on the LAN.
- The ggml-org model card shows `llama serve -hf ...`, a newer front
  end. `llama-server` is what the server README documents and is
  used here.
- The memory figures are estimates from file sizes.

[srv]: https://github.com/ggml-org/llama.cpp/blob/8e5a58df87ab59dfceb5b961ae26691e39f30c24/tools/server/README.md
[cpp]: https://github.com/ggml-org/llama.cpp/blob/8e5a58df87ab59dfceb5b961ae26691e39f30c24/tools/server/server.cpp
[fc]: https://github.com/ggml-org/llama.cpp/blob/8e5a58df87ab59dfceb5b961ae26691e39f30c24/docs/function-calling.md
[chat]: https://github.com/ggml-org/llama.cpp/blob/8e5a58df87ab59dfceb5b961ae26691e39f30c24/common/chat.cpp
[arch]: https://github.com/ggml-org/llama.cpp/blob/8e5a58df87ab59dfceb5b961ae26691e39f30c24/src/llama-arch.cpp
[lt-types]: https://github.com/BerriAI/litellm/blob/v1.106.2/litellm/types/utils.py
[lt-vllm]: https://github.com/BerriAI/litellm/blob/v1.106.2/litellm/llms/hosted_vllm/chat/transformation.py
[lt-lf]: https://github.com/BerriAI/litellm/blob/v1.106.2/litellm/llms/llamafile/chat/transformation.py
[lt-main]: https://github.com/BerriAI/litellm/blob/v1.106.2/litellm/main.py
[lt-gpt]: https://github.com/BerriAI/litellm/blob/v1.106.2/litellm/llms/openai/chat/gpt_transformation.py
[lt-oc]: https://docs.litellm.ai/docs/providers/openai_compatible
[hf-search]: https://huggingface.co/api/models?author=Qwen&search=Qwen3.8
[qwen]: https://huggingface.co/Qwen/Qwen3.8-27B
[hf-ggml]: https://huggingface.co/ggml-org/Qwen3.8-27B-GGUF
[hf-unsloth]: https://huggingface.co/unsloth/Qwen3.8-27B-GGUF
[hf-bart]: https://huggingface.co/bartowski/Qwen3.8-27B-GGUF

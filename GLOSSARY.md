# litellm-llama-with-keycloak

A playground where each Keycloak user reaches llama.cpp from opencode
through LiteLLM community edition under their own LiteLLM identity.

## Language

**Virtual key**:
A LiteLLM API key that belongs to exactly one LiteLLM user.
_Avoid_: API key, token, master key

**Login helper**:
The program on the developer's machine that opencode runs during
`opencode auth login`. It starts a login through the key broker and
prints the virtual key it receives.
_Avoid_: auth command, CLI

**Key broker**:
The service that logs a user in to Keycloak and turns that login into
a virtual key, creating the LiteLLM user if it does not exist yet.
_Avoid_: shim, bridge, key page

**Spend**:
The dollar amount LiteLLM charges a LiteLLM user for their requests.
The model is self-hosted, so the per-token price behind it is nominal,
chosen to sit between Claude Haiku and Claude Sonnet.
_Avoid_: cost, usage, billing

# Key broker under the /broker path prefix

One nginx fronts LiteLLM and the key broker on a single port. LiteLLM
keeps the root, and the key broker lives under `/broker/`, with nginx
stripping the prefix. The broker cannot share the root because
LiteLLM already serves `/callback` (MCP OAuth) and `/login` (admin
UI), both of which the broker also uses. A prefix works because
opencode appends `/.well-known/opencode` to the URL given to
`opencode auth login` rather than resolving it against the host root.

## Considered Options

- Broker at the root next to LiteLLM: rejected, the paths collide.
- One hostname per service: no collisions and no rewriting, but two
  names and two certificates from the internal CA in production, and
  a second name resolving to the developer's machine in the
  playground.

## Consequences

The `/broker` prefix is part of every developer's
`opencode auth login` URL and of the redirect URI registered in
Keycloak, so moving it later means changing both.

# Spend

Spend is the dollar amount LiteLLM charges each LiteLLM user (see
[GLOSSARY.md](../GLOSSARY.md)). The price on `medium` is nominal:
$1 per million input tokens and $5 per million output tokens.

The key broker creates each LiteLLM user with the Keycloak username
as `user_id`. So spend is per Keycloak user.

LiteLLM calculates spend per request, at request time. Changing the
price later does not change past spend.

## Read a user's spend

We rely on `GET /user/daily/activity` with the master key. With
`.env` loaded:

```sh
curl -s -H "Authorization: Bearer $LITELLM_MASTER_KEY" \
  "http://localhost:4000/user/daily/activity?user_id=alice&start_date=2026-10-01&end_date=2026-10-31"
```

It returns daily totals of spend and tokens per model. LiteLLM
stores them in Postgres. Each entry in `results` has a `date` and
`metrics` with `spend`, `prompt_tokens`, `completion_tokens` and
`api_requests`. `breakdown.models` splits the same numbers per model,
and `metadata.total_spend` sums the whole range.

## Prometheus

```sh
curl -s http://localhost:4000/metrics/ | grep -E '^litellm_(spend_metric|input_tokens_metric|output_tokens_metric|total_tokens_metric)'
```

Note the trailing slash: `/metrics` redirects to `/metrics/`. The
per-user label is `user`. Counters reset when LiteLLM restarts.

`/metrics` has no auth here. Besides `user` and the numbers, its
labels include `user_email`, `hashed_api_key`, `api_key_alias`,
`client_ip`, `user_agent` and the upstream `api_base`. LiteLLM also
sends `access-control-allow-origin: *`, so any web page open in a
browser on this machine can read it. In production, put nginx with
TLS and IP filtering in front of it and strip that CORS header on
`/metrics`.

## Sources

| Source                                                  | Status          | Note                                                                |
| ------------------------------------------------------- | --------------- | ------------------------------------------------------------------- |
| `/user/daily/activity`                                  | working         | Daily history per user. The one we rely on.                         |
| `/spend/logs`                                           | working         | Per request rows. Deprecated for `/spend/logs/v2`. Max 10,000 rows. |
| `/user/info`                                            | working         | Spend is the budget counter, not lifetime history.                  |
| `/key/info`                                             | working         | Spend per virtual key.                                              |
| Prometheus `/metrics`                                   | working         | Needs the `prometheus` callback. Resets on restart.                 |
| `/global/spend/report`, `/{user,key,team}/spend/report` | enterprise-only |                                                                     |
| Admin UI usage pages                                    | working         | Per-user spend, at http://localhost:4000/ui/.                       |

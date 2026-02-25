# TokenHub Integration

I don't manage LLM providers directly. All model routing, failover, and provider management is delegated to [TokenHub](https://github.com/jordanhubbard/tokenhub) -- an intelligent LLM proxy that speaks the OpenAI-compatible API.

You configure your physical providers (Anthropic, OpenAI, local vLLM servers, etc.) in TokenHub during its onboarding. Then you register TokenHub as my sole provider and I forward all LLM requests through it.

## Registering TokenHub

```bash
curl -X POST http://localhost:8080/api/v1/providers \
  -H "Content-Type: application/json" \
  -d '{
    "id": "tokenhub",
    "name": "TokenHub",
    "type": "openai",
    "endpoint": "http://localhost:8090/v1",
    "model": "anthropic/claude-sonnet-4-20250514",
    "api_key": "your-tokenhub-api-key"
  }'
```

For repeatable setup, use `bootstrap.local` or see the authoritative sample in the [TokenHub repo](https://github.com/jordanhubbard/tokenhub/blob/main/bootstrap.local.example).

## Health Monitoring

I health-check TokenHub every 30 seconds. Provider states:

- **pending** -- Just registered, first heartbeat not yet received
- **healthy** -- Responding to chat completions successfully
- **failed** -- Last heartbeat detected an error

Check health:

```bash
curl http://localhost:8080/api/v1/providers | jq '.[] | {id, status, last_heartbeat_error}'
```

## Managing Physical Providers

Physical LLM providers (Anthropic, OpenAI, vLLM, etc.) are configured entirely within TokenHub. Use `tokenhubctl` to manage them:

```bash
tokenhubctl provider list
tokenhubctl provider add --name anthropic --type anthropic --api-key "$ANTHROPIC_API_KEY"
tokenhubctl model list
tokenhubctl model enable anthropic/claude-sonnet-4-20250514
```

See the [TokenHub documentation](https://github.com/jordanhubbard/tokenhub) for full provider and model management.

## What Changed

I used to maintain my own multi-provider routing system with scoring, complexity estimation, GPU selection, and four routing policies (minimize_cost, minimize_latency, maximize_quality, balanced). That was ~6,000 lines of provider intelligence that duplicated what TokenHub already does better.

All of that is gone. TokenHub handles routing. I handle orchestration.

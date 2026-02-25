# Troubleshooting

## Common Issues

### Loom won't start

```bash
# Check container status
docker compose ps

# Check logs for errors
make logs
```

Common causes: PostgreSQL connection failure, port conflicts.

### Agents not picking up beads

1. Check provider health: `curl http://localhost:8080/api/v1/providers | jq '.[] | {id,status}'`
2. Check agent status: `curl http://localhost:8080/api/v1/agents | jq '.[] | {name,status}'`
3. Verify project git readiness: `curl http://localhost:8080/api/v1/projects/<id> | jq '.readiness_ok'`
4. Check for blocked beads: `curl http://localhost:8080/api/v1/beads?status=blocked`

### Provider shows "failed" status

```bash
# Check the error
curl http://localhost:8080/api/v1/providers | jq '.[] | {id, last_heartbeat_error}'
```

Common causes: Wrong endpoint URL, network unreachable, model not loaded, API key invalid.

### Git operations failing

```bash
# Check SSH key is deployed
curl -s http://localhost:8080/api/v1/projects/<id>/git-key | jq -r '.public_key'

# Test SSH access from inside the container
docker compose exec loom ssh -T git@github.com
```

### Bead stuck in in_progress

If a bead stays in `in_progress` indefinitely:

1. Check the agent working on it in the Agents tab
2. Use the **Redispatch** button in the bead modal
3. If repeated redispatches fail, the max_hops limit (20) triggers CEO escalation

### Out of memory

Agent containers may need more memory for large codebases:

```yaml
# docker-compose.yml
loom-agent-coder:
  deploy:
    resources:
      limits:
        memory: 4G
```

## Diagnostic Commands

```bash
make logs           # All container logs
make test           # Run test suite
docker compose ps   # Container status
```

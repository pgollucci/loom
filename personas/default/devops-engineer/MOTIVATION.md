# Motivation

## Primary Drive

Reliability. The system should deploy cleanly, recover automatically, and
never surprise you at 3 AM. I build the automation that makes shipping safe.

## Success Metrics

- Deploys succeed on first attempt
- Mean time to recovery under 5 minutes
- CI pipeline runs under 10 minutes
- Zero manual deployment steps

## Trade-off Priorities

1. Reliability — a system that's up beats a system that's fast
2. Automation — manual steps are bugs waiting to happen
3. Observability — you can't fix what you can't see
4. Security — infrastructure misconfigs are the biggest attack surface

## What Frustrates Me

Manual deployments. "It worked in CI" when it doesn't work in production.
Configuration that lives in someone's head instead of in code. Monitoring
gaps that let incidents go undetected.

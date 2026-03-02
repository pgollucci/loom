#!/usr/bin/env python3
import re

with open('internal/loom/loom_lifecycle.go', 'r') as f:
    content = f.read()

# Fix: _ = a.beadsManager.LoadProjectPrefixFromConfig
content = re.sub(
    r'\s+_ = a\.beadsManager\.LoadProjectPrefixFromConfig\(p\.ID, configPath\)',
    '''\n\t\t\tif err := a.beadsManager.LoadProjectPrefixFromConfig(p.ID, configPath); err != nil {
\t\t\t\tlog.Printf("[Loom] Warning: failed to load project prefix for %s: %v", p.ID, err)
\t\t\t}''',
    content
)

# Fix: _ = a.beadsManager.LoadBeadsFromFilesystem
content = re.sub(
    r'\s+_ = a\.beadsManager\.LoadBeadsFromFilesystem\(p\.ID, mainBeadsPath\)',
    '''\n\t\t\tif err := a.beadsManager.LoadBeadsFromFilesystem(p.ID, mainBeadsPath); err != nil {
\t\t\t\tlog.Printf("[Loom] Warning: failed to load beads from filesystem for %s: %v", p.ID, err)
\t\t\t}''',
    content
)

# Fix: _ = a.beadsManager.LoadBeadsFromGit
content = re.sub(
    r'\s+_ = a\.beadsManager\.LoadBeadsFromGit\(ctx, p\.ID, beadsPath\)',
    '''\n\t\t\tif err := a.beadsManager.LoadBeadsFromGit(ctx, p.ID, beadsPath); err != nil {
\t\t\t\tlog.Printf("[Loom] Warning: failed to load beads from git for %s: %v", p.ID, err)
\t\t\t}''',
    content
)

# Fix: providers, _ = a.database.ListProviders()
content = re.sub(
    r'providers, _ = a\.database\.ListProviders\(\)',
    '''providers, err := a.database.ListProviders()
\t\tif err != nil {
\t\t\tlog.Printf("[Loom] Warning: failed to list providers: %v", err)
\t\t\tproviders = []*internalmodels.Provider{}
\t\t}''',
    content
)

# Fix: apiKey, _ = a.keyManager.GetKey
content = re.sub(
    r'apiKey, _ = a\.keyManager\.GetKey\(p\.KeyID\)',
    '''apiKey, err := a.keyManager.GetKey(p.KeyID)
\t\t\t\tif err != nil {
\t\t\t\t\tlog.Printf("[Loom] Warning: failed to get key for provider %s: %v", p.ID, err)
\t\t\t\t}''',
    content
)

# Fix: _ = a.providerRegistry.Upsert
content = re.sub(
    r'\s+_ = a\.providerRegistry\.Upsert\(&provider\.ProviderConfig\{',
    '''\n\t\t\tif err := a.providerRegistry.Upsert(&provider.ProviderConfig{''',
    content
)

# Fix closing brace for Upsert
content = re.sub(
    r'(SelectedModel:\s+selected,\s+Status:\s+p\.Status,\s+LastHeartbeatAt:\s+p\.LastHeartbeatAt,\s+LastHeartbeatLatencyMs:\s+p\.LastHeartbeatLatencyMs,\s+})\)',
    r'\1; err != nil {\n\t\t\t\tlog.Printf("[Loom] Warning: failed to upsert provider %s: %v", p.ID, err)\n\t\t\t}',
    content
)

# Fix: _, _ = a.agentManager.RestoreAgentWorker
content = re.sub(
    r'_, _ = a\.agentManager\.RestoreAgentWorker\(ctx, ag\)',
    '''_, err := a.agentManager.RestoreAgentWorker(ctx, ag)
\t\t\tif err != nil {
\t\t\t\tlog.Printf("[Loom] Warning: failed to restore agent worker %s: %v", ag.ID, err)
\t\t\t}''',
    content
)

# Fix: _ = a.projectManager.AddAgentToProject
content = re.sub(
    r'\s+_ = a\.projectManager\.AddAgentToProject\(ag\.ProjectID, ag\.ID\)',
    '''\n\t\t\tif err := a.projectManager.AddAgentToProject(ag.ProjectID, ag.ID); err != nil {
\t\t\t\tlog.Printf("[Loom] Warning: failed to add agent %s to project %s: %v", ag.ID, ag.ProjectID, err)
\t\t\t}''',
    content
)

# Fix: _ = a.ensureDefaultAgents
content = re.sub(
    r'\s+_ = a\.ensureDefaultAgents\(ctx, p\.ID\)',
    '''\n\t\tif err := a.ensureDefaultAgents(ctx, p.ID); err != nil {
\t\t\tlog.Printf("[Loom] Warning: failed to ensure default agents for project %s: %v", p.ID, err)
\t\t}''',
    content
)

# Fix: _ = a.connectorManager.Close()
content = re.sub(
    r'\s+_ = a\.connectorManager\.Close\(\)',
    '''\n\t\tif err := a.connectorManager.Close(); err != nil {
\t\t\tlog.Printf("[Loom] Warning: failed to close connector manager: %v", err)
\t\t}''',
    content
)

# Fix: _ = mb.Close()
content = re.sub(
    r'\s+_ = mb\.Close\(\)',
    '''\n\t\t\tif err := mb.Close(); err != nil {
\t\t\t\tlog.Printf("[Loom] Warning: failed to close message bus: %v", err)
\t\t\t}''',
    content
)

# Fix: _ = a.database.Close()
content = re.sub(
    r'\s+_ = a\.database\.Close\(\)',
    '''\n\t\tif err := a.database.Close(); err != nil {
\t\t\tlog.Printf("[Loom] Warning: failed to close database: %v", err)
\t\t}''',
    content
)

# Fix: chart, _ = a.orgChartManager.CreateForProject
content = re.sub(
    r'chart, _ = a\.orgChartManager\.CreateForProject\(projectID, project\.Name\)',
    '''chart, err := a.orgChartManager.CreateForProject(projectID, project.Name)
\t\tif err != nil {
\t\t\tlog.Printf("[Loom] Warning: failed to create org chart for project %s: %v", projectID, err)
\t\t}''',
    content
)

# Fix: _ = a.orgChartManager.AssignAgentToRole
content = re.sub(
    r'\s+_ = a\.orgChartManager\.AssignAgentToRole\(projectID, pos\.RoleName, agent\.ID\)',
    '''\n\t\t\tif err := a.orgChartManager.AssignAgentToRole(projectID, pos.RoleName, agent.ID); err != nil {
\t\t\t\tlog.Printf("[Loom] Warning: failed to assign agent %s to role %s: %v", agent.ID, pos.RoleName, err)
\t\t\t}''',
    content
)

# Fix: _ = a.fileLockManager.ReleaseAgentLocks
content = re.sub(
    r'\s+_ = a\.fileLockManager\.ReleaseAgentLocks\(agent\.ID\)',
    '''\n\t\t\tif err := a.fileLockManager.ReleaseAgentLocks(agent.ID); err != nil {
\t\t\t\tlog.Printf("[Loom] Warning: failed to release file locks for agent %s: %v", agent.ID, err)
\t\t\t}''',
    content
)

# Fix: _ = a.eventBus.Publish
content = re.sub(
    r'\s+_ = a\.eventBus\.Publish\(&eventbus\.Event\{',
    '''\n\t\tif err := a.eventBus.Publish(&eventbus.Event{''',
    content
)

with open('internal/loom/loom_lifecycle.go', 'w') as f:
    f.write(content)

print('Fixed all remaining error handling issues')

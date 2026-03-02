package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

func main() {
	data, err := os.ReadFile("internal/loom/loom_lifecycle.go")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read file: %v\n", err)
		os.Exit(1)
	}

	content := string(data)

	// Fix 1: _ = a.database.UpsertProject(proj) -> if err := ... log.Printf
	content = regexp.MustCompile(`\s+_ = a\.database\.UpsertProject\(proj\)`).ReplaceAllString(
		content,
		`
			if err := a.database.UpsertProject(proj); err != nil {
				log.Printf("[Loom] Warning: failed to persist project %s to database: %v", proj.ID, err)
			}`)

	// Fix 2: _ = a.database.UpsertProject(&p) -> if err := ... log.Printf
	content = regexp.MustCompile(`\s+_ = a\.database\.UpsertProject\(&p\)`).ReplaceAllString(
		content,
		`
			if err := a.database.UpsertProject(&p); err != nil {
				log.Printf("[Loom] Warning: failed to persist project %s to database: %v", p.ID, err)
			}`)

	// Fix 3: _ = a.database.UpsertProject(p) -> if err := ... log.Printf
	content = regexp.MustCompile(`\s+_ = a\.database\.UpsertProject\(p\)\s*$`).ReplaceAllString(
		content,
		`
			if err := a.database.UpsertProject(p); err != nil {
				log.Printf("[Loom] Warning: failed to persist project %s git metadata to database: %v", p.ID, err)
			}`)

	// Fix 4: _ = a.beadsManager.LoadProjectPrefixFromConfig
	content = regexp.MustCompile(`\s+_ = a\.beadsManager\.LoadProjectPrefixFromConfig\(p\.ID, configPath\)`).ReplaceAllString(
		content,
		`
			if err := a.beadsManager.LoadProjectPrefixFromConfig(p.ID, configPath); err != nil {
				log.Printf("[Loom] Warning: failed to load project prefix for %s: %v", p.ID, err)
			}`)

	// Fix 5: _ = a.beadsManager.LoadBeadsFromFilesystem
	content = regexp.MustCompile(`\s+_ = a\.beadsManager\.LoadBeadsFromFilesystem\(p\.ID, mainBeadsPath\)`).ReplaceAllString(
		content,
		`
			if err := a.beadsManager.LoadBeadsFromFilesystem(p.ID, mainBeadsPath); err != nil {
				log.Printf("[Loom] Warning: failed to load beads from filesystem for %s: %v", p.ID, err)
			}`)

	// Fix 6: _ = a.beadsManager.LoadBeadsFromGit
	content = regexp.MustCompile(`\s+_ = a\.beadsManager\.LoadBeadsFromGit\(ctx, p\.ID, beadsPath\)`).ReplaceAllString(
		content,
		`
			if err := a.beadsManager.LoadBeadsFromGit(ctx, p.ID, beadsPath); err != nil {
				log.Printf("[Loom] Warning: failed to load beads from git for %s: %v", p.ID, err)
			}`)

	// Fix 7: providers, _ = a.database.ListProviders() -> handle error
	content = regexp.MustCompile(`providers, _ = a\.database\.ListProviders\(\)`).ReplaceAllString(
		content,
		`providers, err := a.database.ListProviders()
		if err != nil {
			log.Printf("[Loom] Warning: failed to list providers: %v", err)
			providers = []*internalmodels.Provider{}
		}`)

	// Fix 8: apiKey, _ = a.keyManager.GetKey
	content = regexp.MustCompile(`apiKey, _ = a\.keyManager\.GetKey\(p\.KeyID\)`).ReplaceAllString(
		content,
		`apiKey, err := a.keyManager.GetKey(p.KeyID)
		if err != nil {
			log.Printf("[Loom] Warning: failed to get key for provider %s: %v", p.ID, err)
		}`)

	// Fix 9: _ = a.providerRegistry.Upsert
	content = regexp.MustCompile(`\s+_ = a\.providerRegistry\.Upsert\(&provider\.ProviderConfig\{`).ReplaceAllString(
		content,
		`
			if err := a.providerRegistry.Upsert(&provider.ProviderConfig{`)

	// Fix 10: _, _ = a.agentManager.RestoreAgentWorker
	content = regexp.MustCompile(`_, _ = a\.agentManager\.RestoreAgentWorker\(ctx, ag\)`).ReplaceAllString(
		content,
		`_, err := a.agentManager.RestoreAgentWorker(ctx, ag)
		if err != nil {
			log.Printf("[Loom] Warning: failed to restore agent worker %s: %v", ag.ID, err)
		}`)

	// Fix 11: _ = a.projectManager.AddAgentToProject
	content = regexp.MustCompile(`\s+_ = a\.projectManager\.AddAgentToProject\(ag\.ProjectID, ag\.ID\)`).ReplaceAllString(
		content,
		`
			if err := a.projectManager.AddAgentToProject(ag.ProjectID, ag.ID); err != nil {
				log.Printf("[Loom] Warning: failed to add agent %s to project %s: %v", ag.ID, ag.ProjectID, err)
			}`)

	// Fix 12: _ = a.ensureDefaultAgents
	content = regexp.MustCompile(`\s+_ = a\.ensureDefaultAgents\(ctx, p\.ID\)`).ReplaceAllString(
		content,
		`
			if err := a.ensureDefaultAgents(ctx, p.ID); err != nil {
				log.Printf("[Loom] Warning: failed to ensure default agents for project %s: %v", p.ID, err)
			}`)

	// Fix 13: Shutdown cleanup errors
	content = regexp.MustCompile(`\s+_ = a\.connectorManager\.Close\(\)`).ReplaceAllString(
		content,
		`
			if err := a.connectorManager.Close(); err != nil {
				log.Printf("[Loom] Warning: failed to close connector manager: %v", err)
			}`)

	// Fix 14: _ = mb.Close()
	content = regexp.MustCompile(`\s+_ = mb\.Close\(\)`).ReplaceAllString(
		content,
		`
			if err := mb.Close(); err != nil {
				log.Printf("[Loom] Warning: failed to close message bus: %v", err)
			}`)

	// Fix 15: _ = a.database.Close()
	content = regexp.MustCompile(`\s+_ = a\.database\.Close\(\)`).ReplaceAllString(
		content,
		`
			if err := a.database.Close(); err != nil {
				log.Printf("[Loom] Warning: failed to close database: %v", err)
			}`)

	// Fix 16: chart, _ = a.orgChartManager.CreateForProject
	content = regexp.MustCompile(`chart, _ = a\.orgChartManager\.CreateForProject\(projectID, project\.Name\)`).ReplaceAllString(
		content,
		`chart, err := a.orgChartManager.CreateForProject(projectID, project.Name)
		if err != nil {
			log.Printf("[Loom] Warning: failed to create org chart for project %s: %v", projectID, err)
		}`)

	// Fix 17: _ = a.orgChartManager.AssignAgentToRole
	content = regexp.MustCompile(`\s+_ = a\.orgChartManager\.AssignAgentToRole\(projectID, pos\.RoleName, agent\.ID\)`).ReplaceAllString(
		content,
		`
			if err := a.orgChartManager.AssignAgentToRole(projectID, pos.RoleName, agent.ID); err != nil {
				log.Printf("[Loom] Warning: failed to assign agent %s to role %s: %v", agent.ID, pos.RoleName, err)
			}`)

	// Fix 18: _ = a.fileLockManager.ReleaseAgentLocks
	content = regexp.MustCompile(`\s+_ = a\.fileLockManager\.ReleaseAgentLocks\(agent\.ID\)`).ReplaceAllString(
		content,
		`
			if err := a.fileLockManager.ReleaseAgentLocks(agent.ID); err != nil {
				log.Printf("[Loom] Warning: failed to release file locks for agent %s: %v", agent.ID, err)
			}`)

	// Fix 19: _ = a.eventBus.Publish
	content = regexp.MustCompile(`\s+_ = a\.eventBus\.Publish\(&eventbus\.Event\{`).ReplaceAllString(
		content,
		`
			if err := a.eventBus.Publish(&eventbus.Event{`)

	// Write the fixed content
	if err := os.WriteFile("internal/loom/loom_lifecycle.go", []byte(content), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write file: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Successfully fixed error handling in loom_lifecycle.go")
}

package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	tfjson "github.com/hashicorp/terraform-json"
)

func latestUploadedState(workspaceID string) (StateVersion, bool) {
	var sv StateVersion
	if err := db.Where("workspace_id = ? AND upload_complete = ?", workspaceID, true).Order("created_at desc, serial desc").First(&sv).Error; err != nil {
		return StateVersion{}, false
	}
	return sv, true
}

func loadUploadedStateVersion(workspaceID string, stateVersionID string) (StateVersion, bool) {
	var sv StateVersion
	if err := db.Where("workspace_id = ? AND id = ? AND upload_complete = ?", workspaceID, stateVersionID, true).First(&sv).Error; err != nil {
		return StateVersion{}, false
	}
	return sv, true
}

func cliRunResponse(run CLIRun, includeLogs bool) map[string]interface{} {
	data := map[string]interface{}{
		"id":               run.ID,
		"workspace_id":     run.WorkspaceID,
		"command":          run.Command,
		"status":           run.Status,
		"message":          run.Message,
		"state_version_id": run.StateVersionID,
		"created_at":       run.CreatedAt,
		"updated_at":       run.UpdatedAt,
		"completed_at":     run.CompletedAt,
	}
	if includeLogs {
		data["log_body"] = run.LogBody
	}
	return data
}

func normalizeRunStatus(status string) string {
	value := strings.TrimSpace(strings.ToLower(status))
	switch value {
	case "planned", "applied", "error":
		return value
	default:
		return ""
	}
}

func providerVersionMapForState(stateVersionID string) map[string]string {
	result := map[string]string{}
	var selections []ProviderSelection
	db.Where("state_version_id = ?", stateVersionID).Find(&selections)
	for _, selection := range selections {
		if selection.Source == "" {
			continue
		}
		result[selection.Source] = selection.Version
	}
	return result
}

func applyProviderVersions(resources []resourceNode, providerVersions map[string]string) []resourceNode {
	if len(providerVersions) == 0 {
		return resources
	}
	for i := range resources {
		if version, ok := providerVersions[resources[i].ProviderSource]; ok && version != "" {
			resources[i].ProviderVersion = version
		}
	}
	return resources
}

func parseRawStateMetadata(rawState []byte) (string, int, string) {
	var parsed struct {
		TerraformVersion string `json:"terraform_version"`
		Serial           int    `json:"serial"`
		Lineage          string `json:"lineage"`
	}
	_ = json.Unmarshal(rawState, &parsed)
	return parsed.TerraformVersion, parsed.Serial, parsed.Lineage
}

func digestResources(sv StateVersion) map[string]resourceDigest {
	resources := extractResources(sv.RawState)
	result := make(map[string]resourceDigest, len(resources))
	for _, res := range resources {
		raw, _ := json.Marshal(map[string]interface{}{
			"attributes": res.Attributes,
			"depends_on": res.DependsOn,
			"provider":   res.Provider,
			"module":     res.ModulePath,
		})
		result[res.ID] = resourceDigest{ID: res.ID, Hash: string(raw)}
	}
	return result
}

func extractResources(rawState []byte) []resourceNode {
	var state tfjson.State
	err := json.Unmarshal(rawState, &state)
	if err == nil && state.Values != nil && state.Values.RootModule != nil {
		out := make([]resourceNode, 0)
		walkModule(state.Values.RootModule, &out, "root")
		if len(out) > 0 {
			return out
		}
	}

	out := make([]resourceNode, 0)
	var legacy struct {
		TerraformVersion string `json:"terraform_version"`
		Serial           int    `json:"serial"`
		Lineage          string `json:"lineage"`
		Resources        []struct {
			Module    string                 `json:"module"`
			Mode      string                 `json:"mode"`
			Type      string                 `json:"type"`
			Name      string                 `json:"name"`
			Provider  string                 `json:"provider"`
			Address   string                 `json:"address"`
			DependsOn []string               `json:"depends_on"`
			Values    map[string]interface{} `json:"values"`
			Instances []struct {
				IndexKey     interface{}            `json:"index_key"`
				Attributes   map[string]interface{} `json:"attributes"`
				Dependencies []string               `json:"dependencies"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(rawState, &legacy); err != nil {
		return out
	}

	seen := map[string]int{}
	for _, res := range legacy.Resources {
		modulePath := strings.TrimSpace(res.Module)
		if modulePath == "" {
			modulePath = "root"
		}

		addr := res.Address
		if addr == "" {
			prefix := ""
			if modulePath != "root" {
				prefix = modulePath + "."
			}
			addr = prefix + res.Type + "." + res.Name
			if strings.EqualFold(res.Mode, "data") {
				addr = prefix + "data." + res.Type + "." + res.Name
			}
		}

		instances := res.Instances
		if len(instances) == 0 {
			instances = append(instances, struct {
				IndexKey     interface{}            `json:"index_key"`
				Attributes   map[string]interface{} `json:"attributes"`
				Dependencies []string               `json:"dependencies"`
			}{})
		}

		for idx, inst := range instances {
			instAddr := addr
			if inst.IndexKey != nil {
				switch v := inst.IndexKey.(type) {
				case string:
					instAddr = instAddr + "[\"" + v + "\"]"
				case float64:
					instAddr = instAddr + "[" + strconv.FormatFloat(v, 'f', -1, 64) + "]"
				default:
					instAddr = instAddr + "[" + fmt.Sprintf("%v", v) + "]"
				}
			} else if len(instances) > 1 {
				instAddr = instAddr + "[" + strconv.Itoa(idx) + "]"
			}

			attrs := res.Values
			if len(inst.Attributes) > 0 {
				attrs = inst.Attributes
			}

			dependsOn := append([]string{}, res.DependsOn...)
			if len(inst.Dependencies) > 0 {
				dependsOn = append(dependsOn, inst.Dependencies...)
			}

			id := instAddr
			if count, exists := seen[id]; exists {
				count++
				seen[id] = count
				id = fmt.Sprintf("%s#%d", id, count)
			} else {
				seen[id] = 0
			}

			out = append(out, resourceNode{
				ID:              id,
				Address:         instAddr,
				Type:            res.Type,
				Name:            res.Name,
				Provider:        res.Provider,
				ProviderSource:  normalizeProviderSource(res.Provider),
				ProviderVersion: "unknown",
				ModulePath:      modulePathFromAddress(instAddr),
				Status:          "Synced",
				DependsOn:       dependsOn,
				Attributes:      attrs,
			})
		}
	}

	return out
}

func walkModule(module *tfjson.StateModule, out *[]resourceNode, modulePath string) {
	if module == nil {
		return
	}

	if modulePath == "" {
		modulePath = "root"
	}

	for _, res := range module.Resources {
		dependsOn := make([]string, 0)
		for _, dep := range res.DependsOn {
			dependsOn = append(dependsOn, qualifyAddress(modulePath, dep))
		}

		attrs := map[string]interface{}{}
		if res.AttributeValues != nil {
			attrs = res.AttributeValues
		}

		address := qualifyAddress(modulePath, res.Address)

		*out = append(*out, resourceNode{
			ID:              address,
			Address:         address,
			Type:            res.Type,
			Name:            res.Name,
			Provider:        res.ProviderName,
			ProviderSource:  normalizeProviderSource(res.ProviderName),
			ProviderVersion: "unknown",
			ModulePath:      modulePath,
			Status:          "Synced",
			DependsOn:       dependsOn,
			Attributes:      attrs,
		})
	}

	for _, child := range module.ChildModules {
		childPath := child.Address
		if childPath == "" {
			childPath = modulePath
		}
		walkModule(child, out, childPath)
	}
}

func modulePathFromAddress(address string) string {
	if address == "" {
		return "root"
	}
	parts := strings.Split(address, ".")
	if len(parts) < 2 {
		return "root"
	}

	segments := make([]string, 0)
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == "module" && i+1 < len(parts)-1 {
			segments = append(segments, "module."+parts[i+1])
			i++
		}
	}
	if len(segments) == 0 {
		return "root"
	}
	return strings.Join(segments, ".")
}

func qualifyAddress(modulePath string, address string) string {
	if address == "" {
		return ""
	}
	if modulePath == "" || modulePath == "root" {
		return address
	}
	if strings.HasPrefix(address, "module.") {
		return address
	}
	if strings.HasPrefix(address, "data.") {
		return modulePath + "." + address
	}
	if strings.Contains(address, ".") {
		return modulePath + "." + address
	}
	return address
}

func normalizeProviderSource(provider string) string {
	if provider == "" {
		return "unknown"
	}
	prefix := "provider[\""
	suffix := "\"]"
	if strings.HasPrefix(provider, prefix) && strings.HasSuffix(provider, suffix) {
		return strings.TrimSuffix(strings.TrimPrefix(provider, prefix), suffix)
	}
	return provider
}

func summarizeState(sv StateVersion) stateSummary {
	type rawState struct {
		TerraformVersion string                 `json:"terraform_version"`
		Serial           int                    `json:"serial"`
		Lineage          string                 `json:"lineage"`
		Outputs          map[string]interface{} `json:"outputs"`
	}

	var parsed rawState
	_ = json.Unmarshal(sv.RawState, &parsed)

	providerVersions := providerVersionMapForState(sv.ID)
	resources := applyProviderVersions(extractResources(sv.RawState), providerVersions)
	providerSet := map[string]struct{}{}
	moduleSet := map[string]struct{}{}

	for _, res := range resources {
		providerSet[res.ProviderSource] = struct{}{}
		moduleSet[res.ModulePath] = struct{}{}
	}

	modules := make([]string, 0, len(moduleSet))
	for module := range moduleSet {
		modules = append(modules, module)
	}
	sort.Strings(modules)

	providers := make([]map[string]string, 0, len(providerSet))
	for provider := range providerSet {
		version := providerVersions[provider]
		if version == "" {
			version = "unknown"
		}
		providers = append(providers, map[string]string{
			"source":  provider,
			"version": version,
		})
	}
	sort.Slice(providers, func(i, j int) bool {
		return providers[i]["source"] < providers[j]["source"]
	})

	outputs := make([]string, 0)
	for key := range parsed.Outputs {
		outputs = append(outputs, key)
	}
	sort.Strings(outputs)

	terraformVersion := parsed.TerraformVersion
	if terraformVersion == "" {
		terraformVersion = defaultWorkspaceTerraformVersion
	}

	return stateSummary{
		TerraformVersion: terraformVersion,
		Serial:           sv.Serial,
		Lineage:          sv.Lineage,
		ResourceCount:    len(resources),
		ModuleCount:      len(modules),
		OutputCount:      len(outputs),
		ProviderCount:    len(providers),
		Modules:          modules,
		Providers:        providers,
		Outputs:          outputs,
		Metadata: map[string]interface{}{
			"provider_version_note": "Provider versions come from tfvision sync-state metadata when available; plain Terraform state uploads fall back to unknown.",
		},
	}
}

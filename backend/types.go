package main

type stateVersionCreatePayload struct {
	Data struct {
		Attributes struct {
			Serial  int    `json:"serial"`
			Lineage string `json:"lineage"`
		} `json:"attributes"`
	} `json:"data"`
}

type providerVersionInput struct {
	Source  string `json:"source"`
	Version string `json:"version"`
}

type stateSyncPayload struct {
	RawStateBase64 string                 `json:"raw_state_base64"`
	Serial         int                    `json:"serial"`
	Lineage        string                 `json:"lineage"`
	Providers      []providerVersionInput `json:"providers"`
}

type cliRunPayload struct {
	Command        string `json:"command"`
	Status         string `json:"status"`
	Message        string `json:"message"`
	LogBody        string `json:"log_body"`
	StateVersionID string `json:"state_version_id"`
}

type resourceNode struct {
	ID              string                 `json:"id"`
	Address         string                 `json:"address"`
	Type            string                 `json:"type"`
	Name            string                 `json:"name"`
	Provider        string                 `json:"provider"`
	ProviderSource  string                 `json:"provider_source"`
	ProviderVersion string                 `json:"provider_version"`
	ModulePath      string                 `json:"module_path"`
	Status          string                 `json:"status"`
	DependsOn       []string               `json:"depends_on"`
	Attributes      map[string]interface{} `json:"attributes"`
}

type resourceDigest struct {
	ID   string
	Hash string
}

type stateSummary struct {
	TerraformVersion string                 `json:"terraform_version"`
	Serial           int                    `json:"serial"`
	Lineage          string                 `json:"lineage"`
	ResourceCount    int                    `json:"resource_count"`
	ModuleCount      int                    `json:"module_count"`
	OutputCount      int                    `json:"output_count"`
	ProviderCount    int                    `json:"provider_count"`
	Modules          []string               `json:"modules"`
	Providers        []map[string]string    `json:"providers"`
	Outputs          []string               `json:"outputs"`
	Metadata         map[string]interface{} `json:"metadata"`
}

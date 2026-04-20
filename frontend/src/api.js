const BASE_URL = '/api/v2';

const flatten = (item) => {
  if (!item) return null;
  const attrs = item.attributes || {};
  return {
    ID: item.id,
    Name: attrs.Name || attrs.name,
    Serial: attrs.Serial ?? attrs.serial,
    Lineage: attrs.Lineage ?? attrs.lineage,
    CreatedAt: attrs.CreatedAt || attrs['created-at'] || attrs.created_at,
    ExecutionMode: attrs.ExecutionMode || attrs['execution-mode'],
    TerraformVersion: attrs.TerraformVersion || attrs['terraform-version'],
    ...attrs,
  };
};

const readJson = async (resp) => {
  if (!resp.ok) throw new Error(`Request failed: ${resp.status}`);
  return resp.json();
};

export const fetchOrgs = async () => {
  const resp = await fetch(`${BASE_URL}/organizations`);
  const json = await readJson(resp);
  return (json.data || []).map(flatten);
};

export const createOrg = async (orgName) => {
  const resp = await fetch(`${BASE_URL}/organizations`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/vnd.api+json' },
    body: JSON.stringify({ data: { attributes: { name: orgName }, id: orgName } }),
  });
  const json = await readJson(resp);
  return flatten(json.data);
};

export const fetchWorkspaces = async (orgId) => {
  const resp = await fetch(`${BASE_URL}/organizations/${encodeURIComponent(orgId)}/workspaces`);
  const json = await readJson(resp);
  return (json.data || []).map(flatten);
};

export const createWorkspace = async (orgId, workspaceName) => {
  const resp = await fetch(`${BASE_URL}/organizations/${encodeURIComponent(orgId)}/workspaces/${encodeURIComponent(workspaceName)}`);
  const json = await readJson(resp);
  return flatten(json.data);
};

export const fetchStateVersions = async (workspaceId) => {
  const resp = await fetch(`${BASE_URL}/workspaces/${workspaceId}/state-versions`);
  if (!resp.ok) return [];
  const json = await resp.json();
  return (json.data || []).map(flatten);
};

export const fetchStateSummary = async (workspaceId) => {
  const resp = await fetch(`${BASE_URL}/workspaces/${workspaceId}/state-summary`);
  if (!resp.ok) return null;
  const json = await resp.json();
  return json.data || null;
};

export const fetchStateVersionSummary = async (workspaceId, stateVersionId) => {
  if (!workspaceId || !stateVersionId) return null;
  const resp = await fetch(`${BASE_URL}/workspaces/${workspaceId}/state-versions/${stateVersionId}/summary`);
  if (!resp.ok) return null;
  const json = await resp.json();
  return json.data || null;
};

export const fetchResources = async (workspaceId) => {
  const resp = await fetch(`${BASE_URL}/workspaces/${workspaceId}/resources`);
  if (!resp.ok) return [];
  const json = await resp.json();
  return json.data || [];
};

export const fetchStateDiff = async (workspaceId, fromId, toId) => {
  if (!workspaceId || !fromId || !toId) return null;
  const resp = await fetch(`${BASE_URL}/workspaces/${workspaceId}/state-versions/${fromId}/compare/${toId}`);
  if (!resp.ok) return null;
  const json = await resp.json();
  return json.data?.attributes || null;
};

export const fetchRuns = async (workspaceId) => {
  if (!workspaceId) return [];
  const resp = await fetch(`${BASE_URL}/workspaces/${workspaceId}/cli-runs`);
  if (!resp.ok) return [];
  const json = await resp.json();
  return json.data || [];
};

export const fetchRunDetail = async (runId) => {
  if (!runId) return null;
  const resp = await fetch(`${BASE_URL}/cli-runs/${runId}`);
  if (!resp.ok) return null;
  const json = await resp.json();
  return json.data || null;
};
